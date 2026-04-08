package main

import (
	"encoding/json"
	"log/slog"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

const (
	wsReadLimit   = 4 * 1024        // 4 KB max frame from client
	wsPingInterval = 30 * time.Second // how often the server pings
	wsPongWait    = 60 * time.Second // client must pong within this window
	wsWriteWait   = 10 * time.Second // timeout for a single write
)

// Hub manages WebSocket connections keyed by account ID.
type Hub struct {
	mu      sync.RWMutex
	clients map[string]map[*wsClient]struct{}
}

type wsClient struct {
	conn      *websocket.Conn
	accountID string
	send      chan []byte

	// Overridable in tests to speed up timing.
	pingInterval time.Duration
	pongWait     time.Duration
	writeWait    time.Duration
}

func newClient(conn *websocket.Conn, accountID string) *wsClient {
	return &wsClient{
		conn:         conn,
		accountID:    accountID,
		send:         make(chan []byte, 32),
		pingInterval: wsPingInterval,
		pongWait:     wsPongWait,
		writeWait:    wsWriteWait,
	}
}

func NewHub() *Hub {
	return &Hub{
		clients: make(map[string]map[*wsClient]struct{}),
	}
}

// Register adds a client for an account.
func (h *Hub) Register(c *wsClient) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.clients[c.accountID] == nil {
		h.clients[c.accountID] = make(map[*wsClient]struct{})
	}
	h.clients[c.accountID][c] = struct{}{}
}

// Unregister removes a client.
func (h *Hub) Unregister(c *wsClient) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if set, ok := h.clients[c.accountID]; ok {
		delete(set, c)
		if len(set) == 0 {
			delete(h.clients, c.accountID)
		}
	}
	close(c.send)
}

// Shutdown closes all connected WebSocket clients with a GoingAway close frame.
// Called during server shutdown before the HTTP server drains.
func (h *Hub) Shutdown() {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, clients := range h.clients {
		for c := range clients {
			_ = c.conn.WriteMessage(
				websocket.CloseMessage,
				websocket.FormatCloseMessage(websocket.CloseGoingAway, "server shutting down"),
			)
			c.conn.Close()
		}
	}
	h.clients = make(map[string]map[*wsClient]struct{})
}

// Broadcast sends an email event to all clients watching an account.
func (h *Hub) Broadcast(accountID string, email *Email) {
	payload, err := json.Marshal(map[string]any{
		"type":  "new_email",
		"email": email,
	})
	if err != nil {
		return
	}

	h.mu.RLock()
	defer h.mu.RUnlock()
	for c := range h.clients[accountID] {
		select {
		case c.send <- payload:
		default:
			// Client too slow — drop.
		}
	}
}

// RunClient pumps messages to/from a WebSocket connection.
//
// Heartbeat contract:
//   - Server sends a Ping every pingInterval with a write deadline.
//   - The read deadline is set to pongWait and is reset by each Pong.
//   - If no Pong arrives within pongWait the read deadline fires, ReadMessage
//     returns an error, and the deferred cleanup runs.
func (h *Hub) RunClient(c *wsClient) {
	defer func() {
		h.Unregister(c)
		c.conn.Close()
		slog.Info("ws: client disconnected", "account", c.accountID)
	}()

	c.conn.SetReadLimit(wsReadLimit)

	// Arm the initial read deadline.
	_ = c.conn.SetReadDeadline(time.Now().Add(c.pongWait))

	// Reset the read deadline every time we receive a Pong.
	c.conn.SetPongHandler(func(string) error {
		return c.conn.SetReadDeadline(time.Now().Add(c.pongWait))
	})

	// Write pump — sends application messages and periodic pings.
	go func() {
		ticker := time.NewTicker(c.pingInterval)
		defer ticker.Stop()

		for {
			select {
			case msg, ok := <-c.send:
				_ = c.conn.SetWriteDeadline(time.Now().Add(c.writeWait))
				if !ok {
					// Hub closed the channel — send a close frame and exit.
					_ = c.conn.WriteMessage(websocket.CloseMessage, []byte{})
					return
				}
				if err := c.conn.WriteMessage(websocket.TextMessage, msg); err != nil {
					return
				}

			case <-ticker.C:
				_ = c.conn.SetWriteDeadline(time.Now().Add(c.writeWait))
				if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
					// Client unreachable — stop the write pump; read pump will
					// detect the closed connection and trigger cleanup.
					return
				}
			}
		}
	}()

	// Read pump — drains incoming frames and drives the pong handler above.
	for {
		if _, _, err := c.conn.ReadMessage(); err != nil {
			break
		}
	}
}
