package main

import (
	"log/slog"
	"time"
)

// StartCleanup runs the TTL cleanup on a ticker and blocks until ctx is done.
// Call it in a goroutine.
func StartCleanup(cfg *Config, store *Storage, stop <-chan struct{}) {
	// Run once immediately at startup so stale data from a previous session
	// is cleared before the server begins serving requests.
	runCleanup(cfg, store)

	ticker := time.NewTicker(cfg.CleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			runCleanup(cfg, store)
		case <-stop:
			return
		}
	}
}

func runCleanup(cfg *Config, store *Storage) {
	res, err := store.Cleanup(cfg.EmailTTL, cfg.AccountTTL)
	if err != nil {
		slog.Error("cleanup: error", "err", err)
		return
	}
	if res.EmailsDeleted > 0 || res.AccountsDeleted > 0 {
		slog.Info("cleanup: deleted stale data",
			"emails", res.EmailsDeleted,
			"accounts", res.AccountsDeleted)
	}
}
