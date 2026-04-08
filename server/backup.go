package main

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// StartBackup runs periodic VACUUM INTO snapshots and blocks until stop is
// closed. Pass a zero BackupInterval to disable backups entirely.
// Call in a goroutine.
func StartBackup(cfg *Config, store *Storage, stop <-chan struct{}) {
	if cfg.BackupInterval == 0 {
		return
	}

	if err := os.MkdirAll(cfg.BackupDir, 0o750); err != nil {
		slog.Error("backup: cannot create backup dir", "dir", cfg.BackupDir, "err", err)
		return
	}

	ticker := time.NewTicker(cfg.BackupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if err := runBackup(cfg, store); err != nil {
				slog.Error("backup: snapshot failed", "err", err)
			}
		case <-stop:
			return
		}
	}
}

// runBackup executes VACUUM INTO to create a timestamped snapshot, then
// prunes old snapshots so at most BackupKeep files are retained.
func runBackup(cfg *Config, store *Storage) error {
	name := fmt.Sprintf("mail_%s.db", time.Now().UTC().Format("2006-01-02T15-04-05"))
	dest := filepath.Join(cfg.BackupDir, name)

	if _, err := store.db.Exec(`VACUUM INTO ?`, dest); err != nil {
		return fmt.Errorf("VACUUM INTO %q: %w", dest, err)
	}

	slog.Info("backup: snapshot created", "file", dest)

	if err := pruneBackups(cfg.BackupDir, cfg.BackupKeep); err != nil {
		// Non-fatal — log and continue.
		slog.Warn("backup: prune failed", "err", err)
	}
	return nil
}

// pruneBackups removes the oldest mail_*.db snapshots in dir, keeping at
// most keep files.
func pruneBackups(dir string, keep int) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("read dir %q: %w", dir, err)
	}

	var snapshots []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasPrefix(e.Name(), "mail_") && strings.HasSuffix(e.Name(), ".db") {
			snapshots = append(snapshots, filepath.Join(dir, e.Name()))
		}
	}

	// Sort ascending by name (timestamp embedded, so lexicographic = chronological).
	sort.Strings(snapshots)

	if len(snapshots) <= keep {
		return nil
	}

	for _, old := range snapshots[:len(snapshots)-keep] {
		if err := os.Remove(old); err != nil {
			return fmt.Errorf("remove %q: %w", old, err)
		}
		slog.Info("backup: pruned old snapshot", "file", old)
	}
	return nil
}
