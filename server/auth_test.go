package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// startTestAPI spins up the full Gin API against a temp SQLite DB and
// returns the httptest.Server plus the Storage so tests can seed data.
func startTestAPI(t *testing.T) (*httptest.Server, *Storage) {
	t.Helper()
	store := newTestStorage(t)
	hub := NewHub()
	push := &PushService{enabled: false}
	cfg := &Config{Domain: "test.local", APIAddr: ":0"}

	// We call StartAPI in a goroutine normally, but for tests we build the
	// router inline so httptest can capture it.
	router := buildRouter(cfg, store, hub, push)
	srv := httptest.NewServer(router)
	t.Cleanup(srv.Close)
	return srv, store
}

// createTestAccount calls POST /accounts and returns the id and token.
func createTestAccount(t *testing.T, srv *httptest.Server) (id, token string) {
	t.Helper()
	resp, err := http.Post(srv.URL+"/accounts", "application/json", nil)
	if err != nil {
		t.Fatalf("POST /accounts: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201, got %d", resp.StatusCode)
	}
	var body struct {
		ID    string `json:"id"`
		Token string `json:"token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return body.ID, body.Token
}

func TestAuth_CreateAccountReturnsToken(t *testing.T) {
	srv, _ := startTestAPI(t)
	id, token := createTestAccount(t, srv)
	if len(id) != 6 {
		t.Errorf("id length: got %d, want 6", len(id))
	}
	if len(token) != 64 {
		t.Errorf("token length: got %d, want 64 hex chars", len(token))
	}
}

func TestAuth_MissingToken(t *testing.T) {
	srv, _ := startTestAPI(t)
	id, _ := createTestAccount(t, srv)

	resp, err := http.Get(srv.URL + "/accounts/" + id + "/emails")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", resp.StatusCode)
	}
}

func TestAuth_WrongToken(t *testing.T) {
	srv, _ := startTestAPI(t)
	id, _ := createTestAccount(t, srv)

	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/accounts/"+id+"/emails", nil)
	req.Header.Set("Authorization", "Bearer "+"a"+strings.Repeat("0", 63))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", resp.StatusCode)
	}
}

func TestAuth_CorrectToken(t *testing.T) {
	srv, _ := startTestAPI(t)
	id, token := createTestAccount(t, srv)

	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/accounts/"+id+"/emails", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
}

func TestAuth_CrossAccount(t *testing.T) {
	srv, _ := startTestAPI(t)
	id1, _ := createTestAccount(t, srv)
	_, token2 := createTestAccount(t, srv)

	// Try to access account 1's emails using account 2's token.
	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/accounts/"+id1+"/emails", nil)
	req.Header.Set("Authorization", "Bearer "+token2)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("cross-account: expected 401, got %d", resp.StatusCode)
	}
}

func TestAuth_DeleteAccount(t *testing.T) {
	srv, _ := startTestAPI(t)
	id, token := createTestAccount(t, srv)

	// Should fail without token.
	req, _ := http.NewRequest(http.MethodDelete, srv.URL+"/accounts/"+id, nil)
	resp, _ := http.DefaultClient.Do(req)
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected 401 without token, got %d", resp.StatusCode)
	}

	// Should succeed with correct token.
	req, _ = http.NewRequest(http.MethodDelete, srv.URL+"/accounts/"+id, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, _ = http.DefaultClient.Do(req)
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Errorf("expected 204, got %d", resp.StatusCode)
	}
}
