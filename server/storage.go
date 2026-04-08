package main

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"time"

	_ "modernc.org/sqlite"
)

const schema = `
CREATE TABLE IF NOT EXISTS accounts (
	id             TEXT PRIMARY KEY,
	created_at     DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
	token_hash     TEXT NOT NULL DEFAULT '',
	last_email_at  DATETIME
);

CREATE TABLE IF NOT EXISTS emails (
	id          TEXT PRIMARY KEY,
	account_id  TEXT NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
	from_addr   TEXT NOT NULL,
	subject     TEXT NOT NULL DEFAULT '',
	body_text   TEXT NOT NULL DEFAULT '',
	body_html   TEXT NOT NULL DEFAULT '',
	received_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
	read        INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS emails_account_idx ON emails(account_id, received_at DESC);

CREATE TABLE IF NOT EXISTS attachments (
	id           TEXT    PRIMARY KEY,
	email_id     TEXT    NOT NULL REFERENCES emails(id) ON DELETE CASCADE,
	account_id   TEXT    NOT NULL,
	filename     TEXT    NOT NULL DEFAULT '',
	content_type TEXT    NOT NULL DEFAULT 'application/octet-stream',
	size         INTEGER NOT NULL DEFAULT 0,
	data         BLOB    NOT NULL
);
CREATE INDEX IF NOT EXISTS attachments_email_idx ON attachments(email_id);

CREATE TABLE IF NOT EXISTS device_tokens (
	account_id TEXT NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
	token      TEXT NOT NULL,
	token_type TEXT NOT NULL DEFAULT 'apns',
	PRIMARY KEY (account_id, token)
);
`

const accountChars = "abcdefghijklmnopqrstuvwxyz0123456789"

// AttachmentMeta describes an attachment.
// Data is populated only during email storage and is never serialized to JSON.
type AttachmentMeta struct {
	ID          string `json:"id"`
	EmailID     string `json:"email_id"`
	Filename    string `json:"filename"`
	ContentType string `json:"content_type"`
	Size        int    `json:"size"`
	Data        []byte `json:"-"` // storage only — never sent to clients
}

type Email struct {
	ID              string            `json:"id"`
	AccountID       string            `json:"account_id"`
	FromAddr        string            `json:"from"`
	Subject         string            `json:"subject"`
	BodyText        string            `json:"body_text"`
	BodyHTML        string            `json:"body_html"`
	ReceivedAt      time.Time         `json:"received_at"`
	Read            bool              `json:"read"`
	AttachmentCount int               `json:"attachment_count"`
	Attachments     []*AttachmentMeta `json:"attachments,omitempty"`
}

type Storage struct {
	db *sql.DB
}

const maxEmailsPerAccount = 500

func NewStorage(path string) (*Storage, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}
	// WAL mode: multiple readers can run concurrently with one writer.
	// synchronous=NORMAL is safe under WAL and roughly 2× faster than FULL.
	// We allow up to 5 open connections so readers don't queue behind writes.
	db.SetMaxOpenConns(5)

	pragmas := []string{
		`PRAGMA journal_mode = WAL`,
		`PRAGMA synchronous  = NORMAL`,
		`PRAGMA foreign_keys = ON`,
	}
	for _, p := range pragmas {
		if _, err := db.Exec(p); err != nil {
			return nil, fmt.Errorf("pragma %q: %w", p, err)
		}
	}
	if _, err := db.Exec(schema); err != nil {
		return nil, fmt.Errorf("create schema: %w", err)
	}
	if err := migrateAccounts(db); err != nil {
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return &Storage{db: db}, nil
}

// migrateAccounts applies incremental schema changes to the accounts table.
func migrateAccounts(db *sql.DB) error {
	cols, err := tableColumns(db, "accounts")
	if err != nil {
		return err
	}
	if !cols["token_hash"] {
		if _, err := db.Exec(`ALTER TABLE accounts ADD COLUMN token_hash TEXT NOT NULL DEFAULT ''`); err != nil {
			return err
		}
	}
	if !cols["last_email_at"] {
		if _, err := db.Exec(`ALTER TABLE accounts ADD COLUMN last_email_at DATETIME`); err != nil {
			return err
		}
	}
	return nil
}

// tableColumns returns a set of column names present in the given table.
func tableColumns(db *sql.DB, table string) (map[string]bool, error) {
	rows, err := db.Query(`SELECT name FROM pragma_table_info(?)`, table)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	cols := make(map[string]bool)
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		cols[name] = true
	}
	return cols, rows.Err()
}

func (s *Storage) Close() error {
	return s.db.Close()
}

// Ping verifies the database is reachable with a lightweight query.
func (s *Storage) Ping() error {
	return s.db.QueryRow(`SELECT 1`).Scan(new(int))
}

// CleanupResult reports what the last cleanup run deleted.
type CleanupResult struct {
	EmailsDeleted   int64
	AccountsDeleted int64
}

// Cleanup deletes:
//   - Emails older than emailTTL
//   - Accounts that have been inactive (no email received) for accountTTL,
//     provided the account itself is also older than accountTTL.
//     ON DELETE CASCADE removes their emails and device tokens automatically.
func (s *Storage) Cleanup(emailTTL, accountTTL time.Duration) (CleanupResult, error) {
	var res CleanupResult

	emailCutoff := time.Now().Add(-emailTTL)
	r, err := s.db.Exec(`DELETE FROM emails WHERE received_at < ?`, emailCutoff)
	if err != nil {
		return res, fmt.Errorf("cleanup emails: %w", err)
	}
	res.EmailsDeleted, _ = r.RowsAffected()

	// An account is considered inactive when:
	//   - It has never received an email AND was created before the cutoff, OR
	//   - Its last received email is before the cutoff.
	accountCutoff := time.Now().Add(-accountTTL)
	r, err = s.db.Exec(`
		DELETE FROM accounts
		WHERE created_at < ?
		  AND (last_email_at IS NULL OR last_email_at < ?)
	`, accountCutoff, accountCutoff)
	if err != nil {
		return res, fmt.Errorf("cleanup accounts: %w", err)
	}
	res.AccountsDeleted, _ = r.RowsAffected()

	return res, nil
}

// CreateAccount generates a random 6-char account ID and a 32-byte bearer
// token. The plain token is returned once and never stored — only its
// SHA-256 hash is persisted.
func (s *Storage) CreateAccount() (id, token string, err error) {
	token = randomID(64) // 64-char hex, 256 bits of entropy
	hash := tokenHash(token)

	for i := 0; i < 10; i++ {
		id = randomID(6)
		_, err = s.db.Exec(
			`INSERT INTO accounts (id, token_hash) VALUES (?, ?)`, id, hash,
		)
		if err == nil {
			return id, token, nil
		}
		// likely ID collision — retry with a new ID (token stays the same)
	}
	return "", "", fmt.Errorf("could not generate unique account ID")
}

// VerifyAccountToken reports whether token is the valid bearer credential
// for accountID. Uses constant-time comparison to prevent timing attacks.
func (s *Storage) VerifyAccountToken(accountID, token string) (bool, error) {
	var storedHash string
	err := s.db.QueryRow(
		`SELECT token_hash FROM accounts WHERE id = ?`, accountID,
	).Scan(&storedHash)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	provided := tokenHash(token)
	return hmac.Equal([]byte(provided), []byte(storedHash)), nil
}

// tokenHash returns the hex-encoded SHA-256 hash of a token string.
func tokenHash(token string) string {
	h := sha256.Sum256([]byte(token))
	return hex.EncodeToString(h[:])
}

func (s *Storage) AccountExists(id string) (bool, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM accounts WHERE id = ?`, id).Scan(&n)
	return n > 0, err
}

func (s *Storage) StoreEmail(e *Email) error {
	// Enforce per-account cap to prevent disk exhaustion.
	var count int
	if err := s.db.QueryRow(
		`SELECT COUNT(*) FROM emails WHERE account_id = ?`, e.AccountID,
	).Scan(&count); err != nil {
		return err
	}
	if count >= maxEmailsPerAccount {
		return fmt.Errorf("account %s has reached the %d email limit", e.AccountID, maxEmailsPerAccount)
	}

	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck

	if _, err := tx.Exec(
		`INSERT INTO emails (id, account_id, from_addr, subject, body_text, body_html, received_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		e.ID, e.AccountID, e.FromAddr, e.Subject, e.BodyText, e.BodyHTML, e.ReceivedAt,
	); err != nil {
		return err
	}

	for _, a := range e.Attachments {
		if _, err := tx.Exec(
			`INSERT INTO attachments (id, email_id, account_id, filename, content_type, size, data)
			 VALUES (?, ?, ?, ?, ?, ?, ?)`,
			a.ID, e.ID, e.AccountID, a.Filename, a.ContentType, a.Size, a.Data,
		); err != nil {
			return err
		}
	}

	// Keep last_email_at current so the TTL cleanup knows this account is active.
	if _, err := tx.Exec(
		`UPDATE accounts SET last_email_at = ? WHERE id = ?`, e.ReceivedAt, e.AccountID,
	); err != nil {
		return err
	}

	return tx.Commit()
}

const (
	defaultPageSize = 50
	maxPageSize     = 100
)

// EmailPage is the paginated result returned by ListEmails.
type EmailPage struct {
	Emails  []Email `json:"emails"`
	HasMore bool    `json:"has_more"`
}

// ListEmails returns up to limit emails for the account, ordered newest-first.
// If before is non-zero only emails received before that time are returned,
// enabling cursor-based pagination.
func (s *Storage) ListEmails(accountID string, limit int, before time.Time) (*EmailPage, error) {
	if limit <= 0 {
		limit = defaultPageSize
	}
	if limit > maxPageSize {
		limit = maxPageSize
	}

	// Fetch one extra row to determine whether another page exists.
	fetch := limit + 1

	const listCols = `
		SELECT e.id, e.account_id, e.from_addr, e.subject, e.body_text, e.body_html,
		       e.received_at, e.read,
		       COUNT(a.id) AS attachment_count
		FROM emails e
		LEFT JOIN attachments a ON a.email_id = e.id`

	var (
		rows *sql.Rows
		err  error
	)
	if before.IsZero() {
		rows, err = s.db.Query(
			listCols+`
			WHERE e.account_id = ?
			GROUP BY e.id ORDER BY e.received_at DESC LIMIT ?`,
			accountID, fetch,
		)
	} else {
		rows, err = s.db.Query(
			listCols+`
			WHERE e.account_id = ? AND e.received_at < ?
			GROUP BY e.id ORDER BY e.received_at DESC LIMIT ?`,
			accountID, before, fetch,
		)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var emails []Email
	for rows.Next() {
		var e Email
		var readInt int
		if err := rows.Scan(&e.ID, &e.AccountID, &e.FromAddr, &e.Subject,
			&e.BodyText, &e.BodyHTML, &e.ReceivedAt, &readInt, &e.AttachmentCount); err != nil {
			return nil, err
		}
		e.Read = readInt == 1
		emails = append(emails, e)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	hasMore := len(emails) > limit
	if hasMore {
		emails = emails[:limit] // trim the sentinel row
	}
	if emails == nil {
		emails = []Email{}
	}
	return &EmailPage{Emails: emails, HasMore: hasMore}, nil
}

// GetEmail returns the email only if it belongs to accountID (prevents IDOR).
// Attachments metadata (no data) is populated.
func (s *Storage) GetEmail(accountID, emailID string) (*Email, error) {
	var e Email
	var readInt int
	err := s.db.QueryRow(
		`SELECT id, account_id, from_addr, subject, body_text, body_html, received_at, read
		 FROM emails WHERE id = ? AND account_id = ?`, emailID, accountID,
	).Scan(&e.ID, &e.AccountID, &e.FromAddr, &e.Subject, &e.BodyText, &e.BodyHTML, &e.ReceivedAt, &readInt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	e.Read = readInt == 1

	attachments, err := s.ListAttachments(accountID, emailID)
	if err != nil {
		return nil, err
	}
	e.Attachments = attachments
	e.AttachmentCount = len(attachments)
	return &e, nil
}

func (s *Storage) MarkRead(accountID, emailID string) error {
	_, err := s.db.Exec(`UPDATE emails SET read = 1 WHERE id = ? AND account_id = ?`, emailID, accountID)
	return err
}

func (s *Storage) DeleteEmail(accountID, emailID string) error {
	_, err := s.db.Exec(`DELETE FROM emails WHERE id = ? AND account_id = ?`, emailID, accountID)
	return err
}

// ListAttachments returns attachment metadata for an email (no blob data).
// The account_id check prevents IDOR.
func (s *Storage) ListAttachments(accountID, emailID string) ([]*AttachmentMeta, error) {
	rows, err := s.db.Query(
		`SELECT id, email_id, filename, content_type, size
		 FROM attachments WHERE email_id = ? AND account_id = ?
		 ORDER BY rowid`,
		emailID, accountID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*AttachmentMeta
	for rows.Next() {
		a := &AttachmentMeta{}
		if err := rows.Scan(&a.ID, &a.EmailID, &a.Filename, &a.ContentType, &a.Size); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	if out == nil {
		out = []*AttachmentMeta{}
	}
	return out, rows.Err()
}

// GetAttachmentData returns the raw bytes and metadata for one attachment.
// Returns nil, nil, nil when not found.
func (s *Storage) GetAttachmentData(accountID, emailID, attachmentID string) ([]byte, *AttachmentMeta, error) {
	a := &AttachmentMeta{}
	err := s.db.QueryRow(
		`SELECT id, email_id, filename, content_type, size, data
		 FROM attachments WHERE id = ? AND email_id = ? AND account_id = ?`,
		attachmentID, emailID, accountID,
	).Scan(&a.ID, &a.EmailID, &a.Filename, &a.ContentType, &a.Size, &a.Data)
	if err == sql.ErrNoRows {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	return a.Data, a, nil
}

func (s *Storage) SaveDeviceToken(accountID, token, tokenType string) error {
	if tokenType == "" {
		tokenType = "apns"
	}
	_, err := s.db.Exec(
		`INSERT OR REPLACE INTO device_tokens (account_id, token, token_type) VALUES (?, ?, ?)`,
		accountID, token, tokenType,
	)
	return err
}

// DeviceToken holds a push token together with its platform type.
type DeviceToken struct {
	Token     string
	TokenType string // "apns" or "fcm"
}

func (s *Storage) RemoveDeviceToken(accountID, token string) error {
	_, err := s.db.Exec(
		`DELETE FROM device_tokens WHERE account_id = ? AND token = ?`,
		accountID, token,
	)
	return err
}

func (s *Storage) GetDeviceTokens(accountID string) ([]DeviceToken, error) {
	rows, err := s.db.Query(
		`SELECT token, token_type FROM device_tokens WHERE account_id = ?`, accountID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tokens []DeviceToken
	for rows.Next() {
		var dt DeviceToken
		if err := rows.Scan(&dt.Token, &dt.TokenType); err != nil {
			return nil, err
		}
		tokens = append(tokens, dt)
	}
	return tokens, rows.Err()
}

// randomID returns a cryptographically random lowercase hex string of length n.
// Account IDs act as bearer credentials so they must use crypto/rand.
func randomID(n int) string {
	// Each byte becomes 2 hex chars; generate enough bytes then truncate.
	b := make([]byte, (n+1)/2)
	if _, err := rand.Read(b); err != nil {
		panic("crypto/rand unavailable: " + err.Error())
	}
	return hex.EncodeToString(b)[:n]
}
