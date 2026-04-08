package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

var testUpgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

// dialWS connects a test WebSocket client to the given httptest server URL.
func dialWS(t *testing.T, srv *httptest.Server) *websocket.Conn {
	t.Helper()
	url := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws"
	conn, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		t.Fatalf("dial ws: %v", err)
	}
	return conn
}

// startEchoServer starts an httptest server that upgrades the connection,
// creates a wsClient with custom timing, and runs it via the hub.
func startEchoServer(t *testing.T, hub *Hub, ping, pong, write time.Duration) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := testUpgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		c := &wsClient{
			conn:         conn,
			accountID:    "test",
			send:         make(chan []byte, 8),
			pingInterval: ping,
			pongWait:     pong,
			writeWait:    write,
		}
		hub.Register(c)
		hub.RunClient(c) // blocks until disconnected
	}))
	t.Cleanup(srv.Close)
	return srv
}

// TestHeartbeat_PingReceived verifies that the server sends a WebSocket Ping
// within one pingInterval after the client connects.
func TestHeartbeat_PingReceived(t *testing.T) {
	hub := NewHub()
	srv := startEchoServer(t, hub,
		100*time.Millisecond, // pingInterval
		500*time.Millisecond, // pongWait
		100*time.Millisecond, // writeWait
	)

	conn := dialWS(t, srv)
	defer conn.Close()

	pingReceived := make(chan struct{}, 1)
	conn.SetPingHandler(func(data string) error {
		pingReceived <- struct{}{}
		// Send the pong back so the server doesn't drop us.
		return conn.WriteMessage(websocket.PongMessage, []byte(data))
	})

	// Drive the read loop so the ping handler fires.
	go func() {
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}()

	select {
	case <-pingReceived:
		// Pass — server sent a ping within the expected window.
	case <-time.After(300 * time.Millisecond):
		t.Fatal("server did not send a ping within 300ms")
	}
}

// TestHeartbeat_ConnectionDroppedWithoutPong verifies that the server closes
// the connection when the client stops responding to pings (pongWait expires).
func TestHeartbeat_ConnectionDroppedWithoutPong(t *testing.T) {
	hub := NewHub()
	srv := startEchoServer(t, hub,
		50*time.Millisecond,  // pingInterval — send pings quickly
		120*time.Millisecond, // pongWait    — short deadline
		50*time.Millisecond,  // writeWait
	)

	conn := dialWS(t, srv)
	defer conn.Close()

	// Override the ping handler to do nothing — no pong sent back.
	conn.SetPingHandler(func(string) error { return nil })

	closed := make(chan struct{})
	go func() {
		defer close(closed)
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}()

	select {
	case <-closed:
		// Pass — server detected the missing pong and dropped the connection.
	case <-time.After(500 * time.Millisecond):
		t.Fatal("server did not close the connection after pongWait expired")
	}
}

// TestHeartbeat_ConnectionKeptAliveWithPong verifies that a well-behaved client
// that replies to pings stays connected past the pongWait window.
func TestHeartbeat_ConnectionKeptAliveWithPong(t *testing.T) {
	hub := NewHub()
	srv := startEchoServer(t, hub,
		50*time.Millisecond,  // pingInterval
		120*time.Millisecond, // pongWait
		50*time.Millisecond,  // writeWait
	)

	conn := dialWS(t, srv)
	defer conn.Close()

	// Respond to every ping.
	conn.SetPingHandler(func(data string) error {
		return conn.WriteMessage(websocket.PongMessage, []byte(data))
	})

	closed := make(chan struct{})
	go func() {
		defer close(closed)
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}()

	// Wait well past one pongWait — connection should still be alive.
	select {
	case <-closed:
		t.Fatal("server closed a healthy connection that was responding to pings")
	case <-time.After(300 * time.Millisecond):
		// Pass — connection survived.
	}
}

// TestBroadcast verifies that a message sent via hub.Broadcast reaches the client.
func TestBroadcast_MessageDelivered(t *testing.T) {
	hub := NewHub()
	srv := startEchoServer(t, hub,
		10*time.Second,
		20*time.Second,
		5*time.Second,
	)

	conn := dialWS(t, srv)
	defer conn.Close()

	// Give the server goroutine time to Register the client.
	time.Sleep(20 * time.Millisecond)

	email := &Email{ID: "e1", AccountID: "test", Subject: "Hi", FromAddr: "a@b.com"}
	hub.Broadcast("test", email)

	_ = conn.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
	_, msg, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("did not receive broadcast: %v", err)
	}
	if !strings.Contains(string(msg), `"new_email"`) {
		t.Errorf("unexpected message: %s", msg)
	}
}
