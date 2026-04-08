package main

import (
	"fmt"
	"testing"
	"time"
)

// insertAccountAt inserts an account with a specific created_at timestamp,
// bypassing the normal CreateAccount flow so we can backdate it.
func insertAccountAt(t *testing.T, s *Storage, id string, createdAt time.Time) {
	t.Helper()
	_, err := s.db.Exec(
		`INSERT INTO accounts (id, token_hash, created_at) VALUES (?, '', ?)`,
		id, createdAt,
	)
	if err != nil {
		t.Fatalf("insertAccountAt: %v", err)
	}
}

// insertEmailAt inserts an email with a specific received_at timestamp and
// updates the account's last_email_at to match.
func insertEmailAt(t *testing.T, s *Storage, accountID string, receivedAt time.Time) string {
	t.Helper()
	id := fmt.Sprintf("email-%d", receivedAt.UnixNano())
	_, err := s.db.Exec(
		`INSERT INTO emails (id, account_id, from_addr, received_at) VALUES (?, ?, 'a@b.com', ?)`,
		id, accountID, receivedAt,
	)
	if err != nil {
		t.Fatalf("insertEmailAt: %v", err)
	}
	_, err = s.db.Exec(
		`UPDATE accounts SET last_email_at = ? WHERE id = ?`, receivedAt, accountID,
	)
	if err != nil {
		t.Fatalf("update last_email_at: %v", err)
	}
	return id
}

func countRows(t *testing.T, s *Storage, table, whereClause string, args ...any) int {
	t.Helper()
	var n int
	query := fmt.Sprintf("SELECT COUNT(*) FROM %s", table)
	if whereClause != "" {
		query += " WHERE " + whereClause
	}
	if err := s.db.QueryRow(query, args...).Scan(&n); err != nil {
		t.Fatalf("countRows(%s): %v", table, err)
	}
	return n
}

// TestCleanup_OldEmailsDeleted verifies that emails older than emailTTL are removed.
func TestCleanup_OldEmailsDeleted(t *testing.T) {
	s := newTestStorage(t)
	insertAccountAt(t, s, "acc1", time.Now().Add(-10*24*time.Hour))

	old := time.Now().Add(-8 * 24 * time.Hour)  // 8 days ago — should be deleted
	recent := time.Now().Add(-3 * 24 * time.Hour) // 3 days ago — should survive

	insertEmailAt(t, s, "acc1", old)
	insertEmailAt(t, s, "acc1", recent)

	res, err := s.Cleanup(7*24*time.Hour, 30*24*time.Hour)
	if err != nil {
		t.Fatalf("Cleanup: %v", err)
	}
	if res.EmailsDeleted != 1 {
		t.Errorf("emails deleted: got %d, want 1", res.EmailsDeleted)
	}
	if n := countRows(t, s, "emails", ""); n != 1 {
		t.Errorf("emails remaining: got %d, want 1", n)
	}
}

// TestCleanup_RecentEmailsKept verifies that emails within the TTL are untouched.
func TestCleanup_RecentEmailsKept(t *testing.T) {
	s := newTestStorage(t)
	insertAccountAt(t, s, "acc1", time.Now().Add(-5*24*time.Hour))
	insertEmailAt(t, s, "acc1", time.Now().Add(-2*24*time.Hour))

	res, err := s.Cleanup(7*24*time.Hour, 30*24*time.Hour)
	if err != nil {
		t.Fatalf("Cleanup: %v", err)
	}
	if res.EmailsDeleted != 0 {
		t.Errorf("emails deleted: got %d, want 0", res.EmailsDeleted)
	}
}

// TestCleanup_InactiveAccountDeleted verifies that accounts with no recent
// emails are removed along with their data (ON DELETE CASCADE).
func TestCleanup_InactiveAccountDeleted(t *testing.T) {
	s := newTestStorage(t)

	// Account created 40 days ago, last email 35 days ago — should be deleted.
	insertAccountAt(t, s, "old", time.Now().Add(-40*24*time.Hour))
	insertEmailAt(t, s, "old", time.Now().Add(-35*24*time.Hour))

	// Account created 5 days ago — should survive regardless.
	insertAccountAt(t, s, "new", time.Now().Add(-5*24*time.Hour))

	res, err := s.Cleanup(7*24*time.Hour, 30*24*time.Hour)
	if err != nil {
		t.Fatalf("Cleanup: %v", err)
	}
	if res.AccountsDeleted != 1 {
		t.Errorf("accounts deleted: got %d, want 1", res.AccountsDeleted)
	}
	if n := countRows(t, s, "accounts", "id = 'new'"); n != 1 {
		t.Error("new account should still exist")
	}
	if n := countRows(t, s, "accounts", "id = 'old'"); n != 0 {
		t.Error("old account should have been deleted")
	}
	// Cascade should have removed the old email too.
	if n := countRows(t, s, "emails", "account_id = 'old'"); n != 0 {
		t.Errorf("old emails should be cascade-deleted, got %d", n)
	}
}

// TestCleanup_ActiveAccountKept verifies that accounts with recent activity
// are not deleted even if they were created a long time ago.
func TestCleanup_ActiveAccountKept(t *testing.T) {
	s := newTestStorage(t)

	// Account created 60 days ago but received an email yesterday.
	insertAccountAt(t, s, "active", time.Now().Add(-60*24*time.Hour))
	insertEmailAt(t, s, "active", time.Now().Add(-1*24*time.Hour))

	res, err := s.Cleanup(7*24*time.Hour, 30*24*time.Hour)
	if err != nil {
		t.Fatalf("Cleanup: %v", err)
	}
	if res.AccountsDeleted != 0 {
		t.Errorf("accounts deleted: got %d, want 0 (account is active)", res.AccountsDeleted)
	}
}

// TestCleanup_NeverUsedAccountDeleted verifies that old accounts that never
// received any email are cleaned up.
func TestCleanup_NeverUsedAccountDeleted(t *testing.T) {
	s := newTestStorage(t)

	// Account created 31 days ago, no emails ever.
	insertAccountAt(t, s, "ghost", time.Now().Add(-31*24*time.Hour))

	res, err := s.Cleanup(7*24*time.Hour, 30*24*time.Hour)
	if err != nil {
		t.Fatalf("Cleanup: %v", err)
	}
	if res.AccountsDeleted != 1 {
		t.Errorf("accounts deleted: got %d, want 1", res.AccountsDeleted)
	}
}

// TestCleanup_DeviceTokensCascadeDeleted verifies device tokens are removed
// when their account is deleted.
func TestCleanup_DeviceTokensCascadeDeleted(t *testing.T) {
	s := newTestStorage(t)

	insertAccountAt(t, s, "acc1", time.Now().Add(-40*24*time.Hour))
	if err := s.SaveDeviceToken("acc1", "tok_abc"); err != nil {
		t.Fatalf("SaveDeviceToken: %v", err)
	}

	_, err := s.Cleanup(7*24*time.Hour, 30*24*time.Hour)
	if err != nil {
		t.Fatalf("Cleanup: %v", err)
	}
	if n := countRows(t, s, "device_tokens", "account_id = 'acc1'"); n != 0 {
		t.Errorf("device tokens should be cascade-deleted, got %d", n)
	}
}

// TestCleanup_LastEmailAtUpdatedOnStore verifies that StoreEmail keeps
// last_email_at current so the TTL logic has accurate data.
func TestCleanup_LastEmailAtUpdatedOnStore(t *testing.T) {
	s := newTestStorage(t)
	insertAccountAt(t, s, "acc1", time.Now().Add(-5*24*time.Hour))

	before := time.Now().Add(-time.Second)
	email := &Email{
		ID:         "e1",
		AccountID:  "acc1",
		FromAddr:   "x@y.com",
		Subject:    "hi",
		ReceivedAt: time.Now(),
	}
	if err := s.StoreEmail(email); err != nil {
		t.Fatalf("StoreEmail: %v", err)
	}

	var lastEmailAt time.Time
	err := s.db.QueryRow(`SELECT last_email_at FROM accounts WHERE id = 'acc1'`).Scan(&lastEmailAt)
	if err != nil {
		t.Fatalf("query last_email_at: %v", err)
	}
	if lastEmailAt.Before(before) {
		t.Errorf("last_email_at not updated: got %v, want >= %v", lastEmailAt, before)
	}
}
