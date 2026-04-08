package main

import (
	"strings"
	"testing"
)

// TestWALMode verifies that NewStorage enables WAL journal mode.
func TestWALMode(t *testing.T) {
	s := newTestStorage(t)

	var mode string
	if err := s.db.QueryRow(`PRAGMA journal_mode`).Scan(&mode); err != nil {
		t.Fatalf("query journal_mode: %v", err)
	}
	if strings.ToLower(mode) != "wal" {
		t.Errorf("journal_mode: got %q, want %q", mode, "wal")
	}
}

// TestSynchronousNormal verifies that synchronous is set to NORMAL (1).
func TestSynchronousNormal(t *testing.T) {
	s := newTestStorage(t)

	var sync int
	if err := s.db.QueryRow(`PRAGMA synchronous`).Scan(&sync); err != nil {
		t.Fatalf("query synchronous: %v", err)
	}
	// NORMAL = 1, FULL = 2, OFF = 0
	if sync != 1 {
		t.Errorf("synchronous: got %d, want 1 (NORMAL)", sync)
	}
}

// TestForeignKeysEnabled verifies that foreign_keys pragma is still ON.
func TestForeignKeysEnabled(t *testing.T) {
	s := newTestStorage(t)

	var fk int
	if err := s.db.QueryRow(`PRAGMA foreign_keys`).Scan(&fk); err != nil {
		t.Fatalf("query foreign_keys: %v", err)
	}
	if fk != 1 {
		t.Errorf("foreign_keys: got %d, want 1 (ON)", fk)
	}
}
