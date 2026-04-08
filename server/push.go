package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"

	"github.com/sideshow/apns2"
	"github.com/sideshow/apns2/payload"
	"github.com/sideshow/apns2/token"
)

// PushService wraps the APNs client and optionally FCM.
type PushService struct {
	client       *apns2.Client
	bundleID     string
	enabled      bool
	fcmServerKey string
	httpClient   *http.Client
}

func NewPushService(cfg *Config) *PushService {
	svc := &PushService{
		fcmServerKey: cfg.FCMServerKey,
		httpClient:   &http.Client{},
	}
	if cfg.FCMServerKey != "" {
		slog.Info("push: FCM configured")
	}
	if cfg.APNsKeyPath == "" || cfg.APNsKeyID == "" || cfg.APNsTeamID == "" {
		slog.Info("push: APNs not configured — APNs push disabled")
		return svc
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
	svc.client = client
	svc.bundleID = cfg.APNsBundleID
	svc.enabled = true
	return svc
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

// SendFCM delivers a push notification via Firebase Cloud Messaging (legacy HTTP API).
func (p *PushService) SendFCM(deviceToken string, email *Email) error {
	if p.fcmServerKey == "" {
		return nil
	}

	subject := email.Subject
	if subject == "" {
		subject = "(no subject)"
	}
	from := email.FromAddr
	if len(from) > 60 {
		from = from[:60] + "…"
	}

	body := map[string]any{
		"to": deviceToken,
		"notification": map[string]string{
			"title": subject,
			"body":  from,
		},
		"data": map[string]string{
			"account_id": email.AccountID,
			"email_id":   email.ID,
			"subject":    subject,
			"from":       from,
		},
		"priority": "high",
	}

	payload, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("fcm marshal: %w", err)
	}

	req, err := http.NewRequest(http.MethodPost,
		"https://fcm.googleapis.com/fcm/send", bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("fcm request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "key="+p.fcmServerKey)

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("fcm send: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("fcm status: %s", resp.Status)
	}
	return nil
}
