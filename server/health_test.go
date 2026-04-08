package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHealth_OK(t *testing.T) {
	store := newTestStorage(t)
	hub := NewHub()
	push := &PushService{enabled: false}
	cfg := &Config{Domain: "test.local"}
	ts := httptest.NewServer(buildRouter(cfg, store, hub, push))
	t.Cleanup(ts.Close)

	resp, err := http.Get(ts.URL + "/health")
	if err != nil {
		t.Fatalf("GET /health: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status: got %d, want 200", resp.StatusCode)
	}

	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body["status"] != "ok" {
		t.Errorf("status field: got %q, want \"ok\"", body["status"])
	}
	checks, _ := body["checks"].(map[string]any)
	if checks["db"] != "ok" {
		t.Errorf("db check: got %q, want \"ok\"", checks["db"])
	}
}

func TestHealth_DBError(t *testing.T) {
	store := newTestStorage(t)
	hub := NewHub()
	push := &PushService{enabled: false}
	cfg := &Config{Domain: "test.local"}
	ts := httptest.NewServer(buildRouter(cfg, store, hub, push))
	t.Cleanup(ts.Close)

	// Close the DB to simulate a failure.
	store.Close()

	resp, err := http.Get(ts.URL + "/health")
	if err != nil {
		t.Fatalf("GET /health: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("status: got %d, want 503", resp.StatusCode)
	}

	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body["status"] != "error" {
		t.Errorf("status field: got %q, want \"error\"", body["status"])
	}
}

func TestHealth_NoAuthRequired(t *testing.T) {
	store := newTestStorage(t)
	hub := NewHub()
	push := &PushService{enabled: false}
	cfg := &Config{Domain: "test.local"}
	ts := httptest.NewServer(buildRouter(cfg, store, hub, push))
	t.Cleanup(ts.Close)

	// No Authorization header — should still return 200.
	resp, err := http.Get(ts.URL + "/health")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("health should not require auth, got %d", resp.StatusCode)
	}
}
