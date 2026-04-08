package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// TestShutdown_InFlightHTTPRequestCompletes verifies that an in-flight HTTP
// request finishes normally after Shutdown() is called — the server waits
// for it rather than dropping it.
func TestShutdown_InFlightHTTPRequestCompletes(t *testing.T) {
	store := newTestStorage(t)
	hub := NewHub()
	push := &PushService{enabled: false}
	cfg := &Config{Domain: "test.local"}

	// Add a slow endpoint to the router so we can simulate an in-flight request.
	router := buildRouter(cfg, store, hub, push)
	readyCh := make(chan struct{})
	proceedCh := make(chan struct{})

	// Inject a slow handler directly on the underlying engine.
	// We can do this by wrapping the router in an http.Handler.
	slowHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/slow" {
			close(readyCh)  // signal that the handler is running
			<-proceedCh     // wait until the test tells us to continue
			w.WriteHeader(http.StatusOK)
			return
		}
		router.ServeHTTP(w, r)
	})

	srv := &http.Server{Handler: slowHandler}
	ts := httptest.NewServer(slowHandler)
	t.Cleanup(ts.Close)
	_ = srv

	var wg sync.WaitGroup
	wg.Add(1)
	var gotStatus int
	go func() {
		defer wg.Done()
		resp, err := http.Get(ts.URL + "/slow")
		if err != nil {
			t.Errorf("slow request failed: %v", err)
			return
		}
		resp.Body.Close()
		gotStatus = resp.StatusCode
	}()

	// Wait until the slow handler is executing.
	<-readyCh

	// Start graceful shutdown in the background.
	shutdownDone := make(chan struct{})
	go func() {
		defer close(shutdownDone)
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		ts.Config.Shutdown(ctx) //nolint
	}()

	// Let the slow handler finish.
	close(proceedCh)
	wg.Wait()

	<-shutdownDone

	if gotStatus != http.StatusOK {
		t.Errorf("in-flight request: got status %d, want 200", gotStatus)
	}
}

// TestShutdown_NewRequestsRejectedAfterShutdown verifies that once Shutdown
// is called new connections get connection-refused (or similar) errors.
func TestShutdown_NewRequestsRejectedAfterShutdown(t *testing.T) {
	store := newTestStorage(t)
	hub := NewHub()
	push := &PushService{enabled: false}
	cfg := &Config{Domain: "test.local"}

	router := buildRouter(cfg, store, hub, push)
	ts := httptest.NewServer(router)

	// Shut the server down immediately.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ts.Config.Shutdown(ctx) //nolint
	ts.Close()

	// New request should fail.
	_, err := http.Get(ts.URL + "/accounts")
	if err == nil {
		t.Error("expected error after shutdown, got nil")
	}
}

// TestShutdown_WebSocketClientsReceiveCloseFrame verifies that Hub.Shutdown
// sends a GoingAway close frame to all connected WebSocket clients.
func TestShutdown_WebSocketClientsReceiveCloseFrame(t *testing.T) {
	hub := NewHub()

	// Start a test server and connect a WebSocket client.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := testUpgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		c := &wsClient{
			conn:         conn,
			accountID:    "test",
			send:         make(chan []byte, 8),
			pingInterval: 10 * time.Second,
			pongWait:     20 * time.Second,
			writeWait:    5 * time.Second,
		}
		hub.Register(c)
		hub.RunClient(c)
	}))
	t.Cleanup(srv.Close)

	url := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws"
	conn, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	// Give RunClient time to register.
	time.Sleep(20 * time.Millisecond)

	closedCh := make(chan int, 1)
	conn.SetCloseHandler(func(code int, _ string) error {
		closedCh <- code
		return nil
	})

	// Drive the read loop so the close handler fires.
	go func() {
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}()

	// Trigger Hub shutdown.
	hub.Shutdown()

	select {
	case code := <-closedCh:
		if code != websocket.CloseGoingAway {
			t.Errorf("close code: got %d, want %d (GoingAway)", code, websocket.CloseGoingAway)
		}
	case <-time.After(500 * time.Millisecond):
		t.Error("client did not receive a close frame within 500ms")
	}
}

// TestShutdown_HubEmptyAfterShutdown verifies the hub has no registered
// clients after Shutdown() returns.
func TestShutdown_HubEmptyAfterShutdown(t *testing.T) {
	hub := NewHub()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := testUpgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		c := &wsClient{
			conn:         conn,
			accountID:    "acc",
			send:         make(chan []byte, 8),
			pingInterval: 10 * time.Second,
			pongWait:     20 * time.Second,
			writeWait:    5 * time.Second,
		}
		hub.Register(c)
		hub.RunClient(c)
	}))
	t.Cleanup(srv.Close)

	url := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws"
	conn, _, _ := websocket.DefaultDialer.Dial(url, nil)
	defer conn.Close()

	time.Sleep(20 * time.Millisecond)

	hub.Shutdown()

	hub.mu.RLock()
	count := len(hub.clients)
	hub.mu.RUnlock()

	if count != 0 {
		t.Errorf("hub.clients len: got %d, want 0 after shutdown", count)
	}
}
