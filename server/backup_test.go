package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestRunBackup_CreatesSnapshot verifies that runBackup writes a valid SQLite
// file into the backup directory.
func TestRunBackup_CreatesSnapshot(t *testing.T) {
	s := newTestStorage(t)
	dir := t.TempDir()

	cfg := &Config{BackupDir: dir, BackupKeep: 7}
	if err := runBackup(cfg, s); err != nil {
		t.Fatalf("runBackup: %v", err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 snapshot file, got %d", len(entries))
	}
	name := entries[0].Name()
	if filepath.Ext(name) != ".db" {
		t.Errorf("snapshot file has unexpected extension: %q", name)
	}

	// The snapshot must itself be a readable SQLite DB.
	snap, err := NewStorage(filepath.Join(dir, name))
	if err != nil {
		t.Fatalf("open snapshot as Storage: %v", err)
	}
	defer snap.Close()
	if err := snap.Ping(); err != nil {
		t.Errorf("snapshot Ping: %v", err)
	}
}

// TestPruneBackups_KeepsLatest verifies that old snapshots are deleted while
// the newest `keep` files are retained.
func TestPruneBackups_KeepsLatest(t *testing.T) {
	dir := t.TempDir()

	// Create 5 fake snapshot files with ascending names (lexicographic = oldest first).
	names := []string{
		"mail_2026-01-01T00-00-00.db",
		"mail_2026-01-02T00-00-00.db",
		"mail_2026-01-03T00-00-00.db",
		"mail_2026-01-04T00-00-00.db",
		"mail_2026-01-05T00-00-00.db",
	}
	for _, n := range names {
		if err := os.WriteFile(filepath.Join(dir, n), []byte("x"), 0o644); err != nil {
			t.Fatalf("create fake snapshot: %v", err)
		}
	}

	if err := pruneBackups(dir, 3); err != nil {
		t.Fatalf("pruneBackups: %v", err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entries) != 3 {
		t.Errorf("expected 3 snapshots after pruning, got %d", len(entries))
	}
	// The 3 newest should survive.
	for _, n := range names[2:] {
		if _, err := os.Stat(filepath.Join(dir, n)); err != nil {
			t.Errorf("expected %q to survive, but got: %v", n, err)
		}
	}
}

// TestPruneBackups_NopWhenUnderLimit verifies that pruning does nothing when
// the number of snapshots is already within the keep limit.
func TestPruneBackups_NopWhenUnderLimit(t *testing.T) {
	dir := t.TempDir()
	for _, n := range []string{"mail_2026-01-01T00-00-00.db", "mail_2026-01-02T00-00-00.db"} {
		if err := os.WriteFile(filepath.Join(dir, n), []byte("x"), 0o644); err != nil {
			t.Fatalf("create fake snapshot: %v", err)
		}
	}

	if err := pruneBackups(dir, 7); err != nil {
		t.Fatalf("pruneBackups: %v", err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entries) != 2 {
		t.Errorf("expected 2 snapshots, got %d", len(entries))
	}
}

// TestStartBackup_DisabledWhenIntervalZero verifies that StartBackup returns
// immediately when BackupInterval is zero (feature disabled).
func TestStartBackup_DisabledWhenIntervalZero(t *testing.T) {
	s := newTestStorage(t)
	stop := make(chan struct{})
	cfg := &Config{BackupInterval: 0}

	done := make(chan struct{})
	go func() {
		StartBackup(cfg, s, stop)
		close(done)
	}()

	select {
	case <-done:
		// Good — returned immediately.
	case <-time.After(500 * time.Millisecond):
		t.Error("StartBackup did not return when interval is zero")
	}
}
