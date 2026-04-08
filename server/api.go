package main

import (
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

// Simple per-IP token bucket for account creation (max 5 per minute).
type rateLimiter struct {
	mu      sync.Mutex
	buckets map[string][]time.Time
}

func newRateLimiter() *rateLimiter {
	return &rateLimiter{buckets: make(map[string][]time.Time)}
}

func (r *rateLimiter) allow(ip string) bool {
	const maxRequests = 5
	const window = time.Minute
	now := time.Now()

	r.mu.Lock()
	defer r.mu.Unlock()

	times := r.buckets[ip]
	// Evict entries outside the window.
	cutoff := now.Add(-window)
	filtered := times[:0]
	for _, t := range times {
		if t.After(cutoff) {
			filtered = append(filtered, t)
		}
	}
	if len(filtered) >= maxRequests {
		r.buckets[ip] = filtered
		return false
	}
	r.buckets[ip] = append(filtered, now)
	return true
}

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

// requireAuth is a Gin middleware that validates the Bearer token for the
// account identified by the :id URL parameter.
// WebSocket connections pass the token as ?token=<value> because the iOS
// URLSessionWebSocketTask does not support custom headers during the upgrade.
func requireAuth(store *Storage) gin.HandlerFunc {
	return func(c *gin.Context) {
		accountID := c.Param("id")

		// Accept token from Authorization header or ?token= query param (WebSocket).
		token := ""
		if auth := c.GetHeader("Authorization"); strings.HasPrefix(auth, "Bearer ") {
			token = strings.TrimPrefix(auth, "Bearer ")
		} else if q := c.Query("token"); q != "" {
			token = q
		}

		if token == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "missing bearer token"})
			return
		}

		ok, err := store.VerifyAccountToken(accountID, token)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "auth check failed"})
			return
		}
		if !ok {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid token"})
			return
		}

		c.Next()
	}
}

// buildRouter constructs and returns the Gin engine. Extracted so tests can
// use it without starting a real listener.
func buildRouter(cfg *Config, store *Storage, hub *Hub, push *PushService) http.Handler {
	rl := newRateLimiter()
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(RequestID(), AccessLog())

	// CORS — also expose Authorization so browsers can send it.
	r.Use(func(c *gin.Context) {
		c.Header("Access-Control-Allow-Origin", "*")
		c.Header("Access-Control-Allow-Methods", "GET,POST,DELETE,OPTIONS")
		c.Header("Access-Control-Allow-Headers", "Content-Type,Authorization")
		if c.Request.Method == http.MethodOptions {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}
		c.Next()
	})

	// --- Health check (no auth) ---

	// GET /health — used by load balancers, uptime monitors, Docker HEALTHCHECK.
	// Returns 200 {"status":"ok",...} or 503 {"status":"error",...}.
	r.GET("/health", func(c *gin.Context) {
		checks := gin.H{}
		allOK := true

		if err := store.Ping(); err != nil {
			checks["db"] = "error: " + err.Error()
			allOK = false
		} else {
			checks["db"] = "ok"
		}

		status := "ok"
		if !allOK {
			status = "error"
		}

		code := http.StatusOK
		if !allOK {
			code = http.StatusServiceUnavailable
		}

		c.JSON(code, gin.H{"status": status, "checks": checks})
	})

	// --- Public routes ---

	// POST /accounts → create account; returns token once (never stored in plain form)
	r.POST("/accounts", func(c *gin.Context) {
		ip, _, _ := net.SplitHostPort(c.Request.RemoteAddr)
		if !rl.allow(ip) {
			c.JSON(http.StatusTooManyRequests, gin.H{"error": "too many accounts created, try again later"})
			return
		}
		id, token, err := store.CreateAccount()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusCreated, gin.H{
			"id":      id,
			"address": id + "@" + cfg.Domain,
			"token":   token, // shown once — client must persist this
		})
	})

	// All routes below require a valid Bearer token for the account in :id.
	auth := r.Group("/accounts/:id", requireAuth(store))

	// DELETE /accounts/:id
	auth.DELETE("", func(c *gin.Context) {
		id := c.Param("id")
		if _, err := store.db.Exec(`DELETE FROM accounts WHERE id = ?`, id); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.Status(http.StatusNoContent)
	})

	// --- Email routes ---

	// GET /accounts/:id/emails?limit=50&before=<RFC3339>
	auth.GET("/emails", func(c *gin.Context) {
		accountID := c.Param("id")

		limit := 0
		if s := c.Query("limit"); s != "" {
			if _, err := fmt.Sscanf(s, "%d", &limit); err != nil || limit < 0 {
				c.JSON(http.StatusBadRequest, gin.H{"error": "invalid limit"})
				return
			}
		}

		var (
			before time.Time
			err    error
		)
		if s := c.Query("before"); s != "" {
			before, err = time.Parse(time.RFC3339, s)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": "before must be RFC3339"})
				return
			}
		}

		page, err := store.ListEmails(accountID, limit, before)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, page)
	})

	// GET /accounts/:id/emails/:emailID
	auth.GET("/emails/:emailID", func(c *gin.Context) {
		accountID := c.Param("id")
		emailID := c.Param("emailID")
		email, err := store.GetEmail(accountID, emailID)
		if err != nil || email == nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "email not found"})
			return
		}
		_ = store.MarkRead(accountID, emailID)
		c.JSON(http.StatusOK, email)
	})

	// DELETE /accounts/:id/emails/:emailID
	auth.DELETE("/emails/:emailID", func(c *gin.Context) {
		accountID := c.Param("id")
		emailID := c.Param("emailID")
		if err := store.DeleteEmail(accountID, emailID); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.Status(http.StatusNoContent)
	})

	// --- Attachment routes ---

	// GET /accounts/:id/emails/:emailID/attachments
	auth.GET("/emails/:emailID/attachments", func(c *gin.Context) {
		accountID := c.Param("id")
		emailID := c.Param("emailID")
		metas, err := store.ListAttachments(accountID, emailID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, metas)
	})

	// GET /accounts/:id/emails/:emailID/attachments/:attachmentID
	auth.GET("/emails/:emailID/attachments/:attachmentID", func(c *gin.Context) {
		accountID := c.Param("id")
		emailID := c.Param("emailID")
		attachmentID := c.Param("attachmentID")

		data, meta, err := store.GetAttachmentData(accountID, emailID, attachmentID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		if data == nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "attachment not found"})
			return
		}

		c.Header("Content-Disposition", fmt.Sprintf(`attachment; filename=%q`, meta.Filename))
		c.Data(http.StatusOK, meta.ContentType, data)
	})

	// --- Device token routes ---

	// POST /accounts/:id/device-token
	auth.POST("/device-token", func(c *gin.Context) {
		accountID := c.Param("id")
		var body struct {
			Token string `json:"token" binding:"required"`
		}
		if err := c.ShouldBindJSON(&body); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		if err := store.SaveDeviceToken(accountID, body.Token); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.Status(http.StatusNoContent)
	})

	// DELETE /accounts/:id/device-token
	auth.DELETE("/device-token", func(c *gin.Context) {
		accountID := c.Param("id")
		var body struct {
			Token string `json:"token" binding:"required"`
		}
		if err := c.ShouldBindJSON(&body); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		if err := store.RemoveDeviceToken(accountID, body.Token); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.Status(http.StatusNoContent)
	})

	// --- WebSocket ---

	// GET /ws/:id?token=<bearer>  — real-time updates for an account.
	// Token is passed as a query param because iOS URLSessionWebSocketTask
	// does not support custom headers during the WebSocket upgrade handshake.
	r.GET("/ws/:id", requireAuth(store), func(c *gin.Context) {
		accountID := c.Param("id")
		conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
		if err != nil {
			reqLog(c).Error("ws upgrade failed", "err", err)
			return
		}
		client := newClient(conn, accountID)
		hub.Register(client)
		go hub.RunClient(client)
	})

	return r
}

// buildHTTPServer constructs but does not start the HTTP server.
func buildHTTPServer(cfg *Config, store *Storage, hub *Hub, push *PushService) *http.Server {
	return &http.Server{
		Addr:    cfg.APIAddr,
		Handler: buildRouter(cfg, store, hub, push),
	}
}

// StartAPI builds and starts the HTTP server in a background goroutine.
// Returns the server so the caller can call Shutdown() during graceful stop.
func StartAPI(cfg *Config, store *Storage, hub *Hub, push *PushService) *http.Server {
	srv := buildHTTPServer(cfg, store, hub, push)
	slog.Info("api: listening", "addr", cfg.APIAddr)
	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("api: stopped", "err", err)
		}
	}()
	return srv
}
