package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"io"
	"log/slog"
	"net"
	"strings"
	"time"

	"github.com/emersion/go-smtp"
)

// smtpBackend implements smtp.Backend.
type smtpBackend struct {
	cfg   *Config
	store *Storage
	hub   *Hub
	push  *PushService
	spam  *SpamFilter
}

func (b *smtpBackend) NewSession(c *smtp.Conn) (smtp.Session, error) {
	// Extract the remote IP for spam checks.
	var remoteIP net.IP
	if tcpAddr, ok := c.Conn().RemoteAddr().(*net.TCPAddr); ok {
		remoteIP = tcpAddr.IP
	}

	// DNSBL check at connection time — reject immediately if listed.
	if remoteIP != nil {
		if err := b.spam.CheckDNSBL(context.Background(), remoteIP); err != nil {
			slog.Warn("smtp: DNSBL rejected connection", "ip", remoteIP.String(), "err", err)
			return nil, err
		}
	}

	return &smtpSession{backend: b, remoteIP: remoteIP, helo: c.Hostname()}, nil
}

// smtpSession handles one SMTP conversation.
type smtpSession struct {
	backend  *smtpBackend
	remoteIP net.IP
	helo     string
	from     string
	to       []string
}

// Ensure smtpSession satisfies smtp.Session.
var _ smtp.Session = (*smtpSession)(nil)

func (s *smtpSession) AuthPlain(username, password string) error {
	// Receive-only server — no outbound auth needed.
	return nil
}

func (s *smtpSession) Mail(from string, opts *smtp.MailOptions) error {
	// SPF check — uses the MAIL FROM domain and the connecting IP.
	if s.remoteIP != nil {
		if err := s.backend.spam.CheckSPF(context.Background(), s.remoteIP, s.helo, from); err != nil {
			slog.Warn("smtp: SPF rejected mail", "from", from, "ip", s.remoteIP.String())
			return err
		}
	}
	s.from = from
	return nil
}

func (s *smtpSession) Rcpt(to string, opts *smtp.RcptOptions) error {
	s.to = append(s.to, to)
	return nil
}

func (s *smtpSession) Data(r io.Reader) error {
	raw, err := io.ReadAll(r)
	if err != nil {
		return err
	}

	pm, err := ParseMessage(bytes.NewReader(raw))
	if err != nil {
		slog.Error("smtp: parse message", "err", err)
		return err
	}

	for _, addr := range s.to {
		accountID := extractLocalPart(addr, s.backend.cfg.Domain)
		if accountID == "" {
			continue
		}
		exists, err := s.backend.store.AccountExists(accountID)
		if err != nil || !exists {
			slog.Warn("smtp: unknown account, dropping mail", "account", accountID, "from", s.from)
			continue
		}

		attachments := make([]*AttachmentMeta, 0, len(pm.Attachments))
		for _, pa := range pm.Attachments {
			attachments = append(attachments, &AttachmentMeta{
				ID:          randomID(16),
				Filename:    pa.Filename,
				ContentType: pa.ContentType,
				Size:        len(pa.Data),
				Data:        pa.Data,
			})
		}

		email := &Email{
			ID:          randomID(16),
			AccountID:   accountID,
			FromAddr:    pm.From,
			Subject:     pm.Subject,
			BodyText:    pm.BodyText,
			BodyHTML:    pm.BodyHTML,
			ReceivedAt:  time.Now(),
			Attachments: attachments,
		}

		if err := s.backend.store.StoreEmail(email); err != nil {
			slog.Error("smtp: store email", "account", accountID, "err", err)
			continue
		}

		// Notify WebSocket clients.
		s.backend.hub.Broadcast(accountID, email)

		// Send push notifications (APNs or FCM).
		go func(accountID string, e *Email) {
			tokens, err := s.backend.store.GetDeviceTokens(accountID)
			if err != nil || len(tokens) == 0 {
				return
			}
			for _, dt := range tokens {
				switch dt.TokenType {
				case "fcm":
					if err := s.backend.push.SendFCM(dt.Token, e); err != nil {
						slog.Error("fcm: push failed", "token", dt.Token, "err", err)
					}
				default: // "apns"
					if err := s.backend.push.Send(dt.Token, e); err != nil {
						slog.Error("apns: push failed", "token", dt.Token, "err", err)
					}
				}
			}
		}(accountID, email)

		slog.Info("smtp: delivered", "account", accountID, "domain", s.backend.cfg.Domain, "from", pm.From, "subject", pm.Subject)
	}
	return nil
}

func (s *smtpSession) Reset() {
	s.from = ""
	s.to = nil
}

func (s *smtpSession) Logout() error {
	return nil
}

// buildSMTPServer constructs but does not start the SMTP server.
func buildSMTPServer(cfg *Config, store *Storage, hub *Hub, push *PushService) *smtp.Server {
	be := &smtpBackend{cfg: cfg, store: store, hub: hub, push: push, spam: NewSpamFilter(cfg)}

	srv := smtp.NewServer(be)
	srv.Addr = cfg.SMTPAddr
	srv.Domain = cfg.Domain
	srv.ReadTimeout = 30 * time.Second
	srv.WriteTimeout = 30 * time.Second
	srv.MaxMessageBytes = 25 * 1024 * 1024
	srv.MaxRecipients = 10
	srv.AllowInsecureAuth = true // set false once TLS is configured

	if cert := getEnv("MAIL_TLS_CERT", ""); cert != "" {
		tlsCert, err := tls.LoadX509KeyPair(cert, getEnv("MAIL_TLS_KEY", ""))
		if err != nil {
			slog.Error("smtp: tls cert load error, continuing without TLS", "err", err)
		} else {
			srv.TLSConfig = &tls.Config{Certificates: []tls.Certificate{tlsCert}}
		}
	}
	return srv
}

// StartSMTP builds and starts the SMTP server in the foreground.
// Returns the server so the caller can call Close() during shutdown.
func StartSMTP(cfg *Config, store *Storage, hub *Hub, push *PushService) *smtp.Server {
	srv := buildSMTPServer(cfg, store, hub, push)
	slog.Info("smtp: listening", "addr", cfg.SMTPAddr, "domain", cfg.Domain)
	go func() {
		if err := srv.ListenAndServe(); err != nil {
			slog.Error("smtp: stopped", "err", err)
		}
	}()
	return srv
}

// extractLocalPart returns the local part of an address if it matches the domain.
func extractLocalPart(addr, domain string) string {
	addr = strings.TrimSpace(addr)
	addr = strings.Trim(addr, "<>")
	parts := strings.SplitN(addr, "@", 2)
	if len(parts) != 2 {
		return ""
	}
	if !strings.EqualFold(parts[1], domain) {
		return ""
	}
	return strings.ToLower(parts[0])
}
