package main

import (
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	// Server
	Domain string
	DBPath string

	// SMTP
	SMTPAddr string

	// API
	APIAddr string

	// APNs
	APNsKeyPath    string
	APNsKeyID      string
	APNsTeamID     string
	APNsBundleID   string
	APNsProduction bool

	// TTL cleanup
	EmailTTL        time.Duration // delete emails older than this
	AccountTTL      time.Duration // delete accounts inactive longer than this
	CleanupInterval time.Duration // how often to run cleanup

	// Logging
	LogFormat string // "json" (default) or "text"
	LogLevel  string // "debug" | "info" | "warn" | "error"

	// Backup
	BackupDir      string        // directory for VACUUM INTO snapshots
	BackupInterval time.Duration // how often to snapshot (0 = disabled)
	BackupKeep     int           // number of snapshots to retain

	// Spam filtering
	SpamDNSBLs    []string // DNSBL zones to check (empty = disabled)
	SpamSPF       bool     // enable SPF checks
	SpamSPFReject bool     // true = reject on SPF fail; false = log only

	// FCM (Android push)
	FCMServerKey string // Firebase Cloud Messaging server key (v1 legacy key)
}

func LoadConfig() *Config {
	return &Config{
		Domain:          getEnv("MAIL_DOMAIN", "localhost"),
		DBPath:          getEnv("MAIL_DB_PATH", "mail.db"),
		SMTPAddr:        getEnv("MAIL_SMTP_ADDR", ":25"),
		APIAddr:         getEnv("MAIL_API_ADDR", ":8080"),
		APNsKeyPath:     getEnv("APNS_KEY_PATH", "apns_key.p8"),
		APNsKeyID:       getEnv("APNS_KEY_ID", ""),
		APNsTeamID:      getEnv("APNS_TEAM_ID", ""),
		APNsBundleID:    getEnv("APNS_BUNDLE_ID", "com.example.mailclient"),
		APNsProduction:  getEnv("APNS_PRODUCTION", "false") == "true",
		EmailTTL:        getDays("EMAIL_TTL_DAYS", 7),
		AccountTTL:      getDays("ACCOUNT_TTL_DAYS", 30),
		CleanupInterval: getDuration("CLEANUP_INTERVAL", 24*time.Hour),
		LogFormat:       getEnv("LOG_FORMAT", "json"),
		LogLevel:        getEnv("LOG_LEVEL", "info"),
		SpamDNSBLs:     getStringSlice("SPAM_DNSBLS", []string{"zen.spamhaus.org"}),
		SpamSPF:         getEnv("SPAM_SPF", "true") == "true",
		SpamSPFReject:   getEnv("SPAM_SPF_REJECT", "true") == "true",
		BackupDir:       getEnv("BACKUP_DIR", "backups"),
		BackupInterval:  getDuration("BACKUP_INTERVAL", 24*time.Hour),
		BackupKeep:      getInt("BACKUP_KEEP", 7),
		FCMServerKey:    getEnv("FCM_SERVER_KEY", ""),
	}
}

func getStringSlice(key string, fallback []string) []string {
	if v := os.Getenv(key); v != "" {
		var out []string
		for _, s := range strings.Split(v, ",") {
			s = strings.TrimSpace(s)
			if s != "" {
				out = append(out, s)
			}
		}
		return out
	}
	return fallback
}

func getInt(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
	}
	return fallback
}

func getDays(key string, defaultDays int) time.Duration {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return time.Duration(n) * 24 * time.Hour
		}
	}
	return time.Duration(defaultDays) * 24 * time.Hour
}

func getDuration(key string, fallback time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			return d
		}
	}
	return fallback
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
