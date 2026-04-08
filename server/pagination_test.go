package main

import (
	"fmt"
	"os"
	"testing"
	"time"
)

// newTestStorage creates a temporary SQLite DB for testing and registers
// cleanup so the file is removed when the test finishes.
func newTestStorage(t *testing.T) *Storage {
	t.Helper()
	f, err := os.CreateTemp("", "mailtest-*.db")
	if err != nil {
		t.Fatalf("create temp db: %v", err)
	}
	f.Close()
	t.Cleanup(func() { os.Remove(f.Name()) })

	s, err := NewStorage(f.Name())
	if err != nil {
		t.Fatalf("NewStorage: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

// seedEmails inserts n emails for accountID spaced 1 second apart,
// starting at baseTime and going forward.
func seedEmails(t *testing.T, s *Storage, accountID string, n int, baseTime time.Time) {
	t.Helper()
	for i := 0; i < n; i++ {
		e := &Email{
			ID:         fmt.Sprintf("email-%d", i),
			AccountID:  accountID,
			FromAddr:   "sender@example.com",
			Subject:    fmt.Sprintf("Subject %d", i),
			ReceivedAt: baseTime.Add(time.Duration(i) * time.Second),
		}
		if err := s.StoreEmail(e); err != nil {
			t.Fatalf("StoreEmail %d: %v", i, err)
		}
	}
}

func TestPagination_FirstPage(t *testing.T) {
	s := newTestStorage(t)
	_, _ = s.db.Exec(`INSERT INTO accounts (id) VALUES ('acc1')`)

	base := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	seedEmails(t, s, "acc1", 10, base)

	page, err := s.ListEmails("acc1", 5, time.Time{})
	if err != nil {
		t.Fatalf("ListEmails: %v", err)
	}
	if len(page.Emails) != 5 {
		t.Errorf("got %d emails, want 5", len(page.Emails))
	}
	if !page.HasMore {
		t.Error("has_more should be true")
	}
	// Newest first — email-9 should be first.
	if page.Emails[0].ID != "email-9" {
		t.Errorf("first email ID: got %s, want email-9", page.Emails[0].ID)
	}
}

func TestPagination_LastPage(t *testing.T) {
	s := newTestStorage(t)
	_, _ = s.db.Exec(`INSERT INTO accounts (id) VALUES ('acc1')`)

	base := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	seedEmails(t, s, "acc1", 10, base)

	page, err := s.ListEmails("acc1", 5, time.Time{})
	if err != nil {
		t.Fatalf("first page: %v", err)
	}

	// Use the last email on the first page as the cursor.
	cursor := page.Emails[len(page.Emails)-1].ReceivedAt
	page2, err := s.ListEmails("acc1", 5, cursor)
	if err != nil {
		t.Fatalf("second page: %v", err)
	}
	if len(page2.Emails) != 5 {
		t.Errorf("got %d emails on second page, want 5", len(page2.Emails))
	}
	if page2.HasMore {
		t.Error("has_more should be false on last page")
	}
}

func TestPagination_NoDuplicates(t *testing.T) {
	s := newTestStorage(t)
	_, _ = s.db.Exec(`INSERT INTO accounts (id) VALUES ('acc1')`)

	base := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	seedEmails(t, s, "acc1", 12, base)

	seen := make(map[string]bool)
	var cursor time.Time
	for {
		page, err := s.ListEmails("acc1", 5, cursor)
		if err != nil {
			t.Fatalf("ListEmails: %v", err)
		}
		for _, e := range page.Emails {
			if seen[e.ID] {
				t.Errorf("duplicate email ID %s across pages", e.ID)
			}
			seen[e.ID] = true
		}
		if !page.HasMore {
			break
		}
		cursor = page.Emails[len(page.Emails)-1].ReceivedAt
	}
	if len(seen) != 12 {
		t.Errorf("total emails seen: got %d, want 12", len(seen))
	}
}

func TestPagination_EmptyAccount(t *testing.T) {
	s := newTestStorage(t)
	_, _ = s.db.Exec(`INSERT INTO accounts (id) VALUES ('acc1')`)

	page, err := s.ListEmails("acc1", 50, time.Time{})
	if err != nil {
		t.Fatalf("ListEmails: %v", err)
	}
	if len(page.Emails) != 0 {
		t.Errorf("expected 0 emails, got %d", len(page.Emails))
	}
	if page.HasMore {
		t.Error("has_more should be false for empty account")
	}
}

func TestPagination_LimitClamped(t *testing.T) {
	s := newTestStorage(t)
	_, _ = s.db.Exec(`INSERT INTO accounts (id) VALUES ('acc1')`)

	base := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	seedEmails(t, s, "acc1", 10, base)

	// Request 999 — should be clamped to maxPageSize (100), return all 10.
	page, err := s.ListEmails("acc1", 999, time.Time{})
	if err != nil {
		t.Fatalf("ListEmails: %v", err)
	}
	if len(page.Emails) != 10 {
		t.Errorf("got %d emails, want 10", len(page.Emails))
	}
	if page.HasMore {
		t.Error("has_more should be false")
	}
}

func TestPagination_DefaultLimit(t *testing.T) {
	s := newTestStorage(t)
	_, _ = s.db.Exec(`INSERT INTO accounts (id) VALUES ('acc1')`)

	base := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	// Insert more than the default page size.
	seedEmails(t, s, "acc1", defaultPageSize+5, base)

	page, err := s.ListEmails("acc1", 0, time.Time{}) // 0 → use default
	if err != nil {
		t.Fatalf("ListEmails: %v", err)
	}
	if len(page.Emails) != defaultPageSize {
		t.Errorf("got %d emails, want %d", len(page.Emails), defaultPageSize)
	}
	if !page.HasMore {
		t.Error("has_more should be true")
	}
}
