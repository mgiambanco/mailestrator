package main

import (
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

const reqIDKey = "request_id"

// SetupLogger configures the global slog logger.
// format: "json" (default in production) or "text" (human-readable)
// level:  "debug" | "info" | "warn" | "error"
func SetupLogger(format, level string) {
	var lvl slog.Level
	switch strings.ToLower(level) {
	case "debug":
		lvl = slog.LevelDebug
	case "warn":
		lvl = slog.LevelWarn
	case "error":
		lvl = slog.LevelError
	default:
		lvl = slog.LevelInfo
	}

	opts := &slog.HandlerOptions{Level: lvl}

	var handler slog.Handler
	if strings.ToLower(format) == "json" {
		handler = slog.NewJSONHandler(os.Stdout, opts)
	} else {
		handler = slog.NewTextHandler(os.Stdout, opts)
	}

	slog.SetDefault(slog.New(handler))
}

// RequestID is Gin middleware that stamps every request with a unique ID.
// It echoes back the client's X-Request-ID if provided, otherwise generates one.
func RequestID() gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.GetHeader("X-Request-ID")
		if id == "" {
			id = newRequestID()
		}
		c.Set(reqIDKey, id)
		c.Header("X-Request-ID", id)
		c.Next()
	}
}

// AccessLog is Gin middleware that writes a structured access log entry after
// each request using the global slog logger.
func AccessLog() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()

		status := c.Writer.Status()
		lvl := slog.LevelInfo
		if status >= http.StatusInternalServerError {
			lvl = slog.LevelError
		} else if status >= http.StatusBadRequest {
			lvl = slog.LevelWarn
		}

		slog.Log(c.Request.Context(), lvl, "http",
			"method", c.Request.Method,
			"path", c.Request.URL.Path,
			"status", status,
			"duration_ms", time.Since(start).Milliseconds(),
			"ip", c.ClientIP(),
			"request_id", c.GetString(reqIDKey),
		)
	}
}

// reqLog returns a logger pre-loaded with the request ID from the Gin context.
func reqLog(c *gin.Context) *slog.Logger {
	return slog.With("request_id", c.GetString(reqIDKey))
}

func newRequestID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
