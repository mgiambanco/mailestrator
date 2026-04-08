package main

import (
	"fmt"
	"log/slog"
	"os"

	"github.com/sideshow/apns2"
	"github.com/sideshow/apns2/payload"
	"github.com/sideshow/apns2/token"
)

// PushService wraps the APNs client.
type PushService struct {
	client   *apns2.Client
	bundleID string
	enabled  bool
}

func NewPushService(cfg *Config) *PushService {
	if cfg.APNsKeyPath == "" || cfg.APNsKeyID == "" || cfg.APNsTeamID == "" {
		slog.Info("push: APNs not configured — push notifications disabled")
		return &PushService{enabled: false}
	}

	keyBytes, err := os.ReadFile(cfg.APNsKeyPath)
	if err != nil {
		slog.Error("push: read APNs key, push disabled", "path", cfg.APNsKeyPath, "err", err)
		return &PushService{enabled: false}
	}

	authKey, err := token.AuthKeyFromBytes(keyBytes)
	if err != nil {
		slog.Error("push: parse APNs key, push disabled", "err", err)
		return &PushService{enabled: false}
	}

	t := &token.Token{
		AuthKey: authKey,
		KeyID:   cfg.APNsKeyID,
		TeamID:  cfg.APNsTeamID,
	}

	var client *apns2.Client
	if cfg.APNsProduction {
		client = apns2.NewTokenClient(t).Production()
	} else {
		client = apns2.NewTokenClient(t).Development()
	}

	slog.Info("push: APNs configured", "bundle", cfg.APNsBundleID, "production", cfg.APNsProduction)
	return &PushService{client: client, bundleID: cfg.APNsBundleID, enabled: true}
}

// Send delivers a push notification for a new email.
func (p *PushService) Send(deviceToken string, email *Email) error {
	if !p.enabled {
		return nil
	}

	from := email.FromAddr
	if len(from) > 60 {
		from = from[:60] + "…"
	}
	subject := email.Subject
	if subject == "" {
		subject = "(no subject)"
	}

	n := &apns2.Notification{
		DeviceToken: deviceToken,
		Topic:       p.bundleID,
		Payload: payload.NewPayload().
			AlertTitle(subject).
			AlertBody(fmt.Sprintf("From: %s", from)).
			Sound("default").
			Badge(1).
			Custom("account_id", email.AccountID).
			Custom("email_id", email.ID),
	}

	res, err := p.client.Push(n)
	if err != nil {
		return fmt.Errorf("apns push: %w", err)
	}
	if !res.Sent() {
		return fmt.Errorf("apns rejected: %s", res.Reason)
	}
	return nil
}
