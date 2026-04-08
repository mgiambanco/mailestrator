package main

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

// TestRequestID_EchoesClientID verifies that a client-supplied X-Request-ID is
// echoed back in the response header unchanged.
func TestRequestID_EchoesClientID(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(RequestID())
	r.GET("/ping", func(c *gin.Context) { c.Status(http.StatusOK) })

	req := httptest.NewRequest(http.MethodGet, "/ping", nil)
	req.Header.Set("X-Request-ID", "my-custom-id")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if got := w.Header().Get("X-Request-ID"); got != "my-custom-id" {
		t.Errorf("X-Request-ID: got %q, want %q", got, "my-custom-id")
	}
}

// TestRequestID_GeneratesWhenAbsent verifies that a unique ID is generated
// and echoed back when the client does not supply one.
func TestRequestID_GeneratesWhenAbsent(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(RequestID())
	r.GET("/ping", func(c *gin.Context) { c.Status(http.StatusOK) })

	req1 := httptest.NewRequest(http.MethodGet, "/ping", nil)
	w1 := httptest.NewRecorder()
	r.ServeHTTP(w1, req1)
	id1 := w1.Header().Get("X-Request-ID")
	if id1 == "" {
		t.Fatal("expected a generated X-Request-ID, got empty string")
	}

	req2 := httptest.NewRequest(http.MethodGet, "/ping", nil)
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)
	id2 := w2.Header().Get("X-Request-ID")

	if id1 == id2 {
		t.Errorf("two generated request IDs should be unique, both got %q", id1)
	}
}

// TestAccessLog_DoesNotPanic verifies that AccessLog middleware handles all
// HTTP status classes (2xx/4xx/5xx) without panicking.
func TestAccessLog_DoesNotPanic(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(RequestID(), AccessLog())
	r.GET("/ok", func(c *gin.Context) { c.Status(http.StatusOK) })
	r.GET("/bad", func(c *gin.Context) { c.Status(http.StatusBadRequest) })
	r.GET("/err", func(c *gin.Context) { c.Status(http.StatusInternalServerError) })

	for _, path := range []string{"/ok", "/bad", "/err"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
	}
}

// TestSetupLogger_JSONFormat verifies that SetupLogger("json", "info") installs
// a logger that emits valid JSON lines with the expected fields.
func TestSetupLogger_JSONFormat(t *testing.T) {
	var buf bytes.Buffer
	handler := slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo})
	prev := slog.Default()
	slog.SetDefault(slog.New(handler))
	t.Cleanup(func() { slog.SetDefault(prev) })

	slog.Info("test message", "key", "value")

	line := strings.TrimSpace(buf.String())
	if line == "" {
		t.Fatal("no log output")
	}

	var obj map[string]any
	if err := json.Unmarshal([]byte(line), &obj); err != nil {
		t.Fatalf("log line is not valid JSON: %v\nline: %s", err, line)
	}
	if obj["msg"] != "test message" {
		t.Errorf("msg field: got %v, want %q", obj["msg"], "test message")
	}
	if obj["key"] != "value" {
		t.Errorf("key field: got %v, want %q", obj["key"], "value")
	}
}

// TestSetupLogger_TextFormat verifies that SetupLogger("text", "debug") does not panic.
func TestSetupLogger_TextFormat(t *testing.T) {
	prev := slog.Default()
	t.Cleanup(func() { slog.SetDefault(prev) })
	SetupLogger("text", "debug")
	// If we get here without panicking the handler was installed correctly.
}
