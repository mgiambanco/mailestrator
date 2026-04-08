package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"strings"
	"testing"
	"time"
)

// buildMultipartEmail constructs a minimal multipart/mixed MIME message with a
// text body and one attachment so we can exercise the parser end-to-end.
func buildMultipartEmail(t *testing.T, filename, contentType string, attachData []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)

	header := make(textproto.MIMEHeader)
	header.Set("Content-Type", fmt.Sprintf("multipart/mixed; boundary=%q", mw.Boundary()))
	header.Set("From", "sender@example.com")
	header.Set("Subject", "Test with attachment")

	// Write the raw headers before the multipart body.
	var msg bytes.Buffer
	msg.WriteString("From: sender@example.com\r\n")
	msg.WriteString("Subject: Test with attachment\r\n")
	msg.WriteString(fmt.Sprintf("Content-Type: multipart/mixed; boundary=%q\r\n", mw.Boundary()))
	msg.WriteString("\r\n")

	// Text part.
	h := make(textproto.MIMEHeader)
	h.Set("Content-Type", "text/plain")
	h.Set("Content-Disposition", "inline")
	pw, _ := mw.CreatePart(h)
	pw.Write([]byte("Hello world"))

	// Attachment part.
	h = make(textproto.MIMEHeader)
	h.Set("Content-Type", contentType)
	h.Set("Content-Disposition", fmt.Sprintf(`attachment; filename=%q`, filename))
	pw, _ = mw.CreatePart(h)
	pw.Write(attachData)

	mw.Close()
	msg.Write(buf.Bytes())
	return msg.Bytes()
}

// ── MIME parser tests ────────────────────────────────────────────────────────

func TestParseMessage_AttachmentExtracted(t *testing.T) {
	raw := buildMultipartEmail(t, "hello.txt", "text/plain", []byte("attachment content"))
	pm, err := ParseMessage(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("ParseMessage: %v", err)
	}
	if len(pm.Attachments) != 1 {
		t.Fatalf("attachments: got %d, want 1", len(pm.Attachments))
	}
	a := pm.Attachments[0]
	if a.Filename != "hello.txt" {
		t.Errorf("filename: got %q, want %q", a.Filename, "hello.txt")
	}
	if string(a.Data) != "attachment content" {
		t.Errorf("data: got %q, want %q", string(a.Data), "attachment content")
	}
}

func TestParseMessage_OversizedAttachmentSkipped(t *testing.T) {
	big := make([]byte, maxAttachmentBytes+1)
	raw := buildMultipartEmail(t, "big.bin", "application/octet-stream", big)
	pm, err := ParseMessage(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("ParseMessage: %v", err)
	}
	if len(pm.Attachments) != 0 {
		t.Errorf("oversized attachment should be skipped, got %d attachments", len(pm.Attachments))
	}
}

// ── Storage tests ────────────────────────────────────────────────────────────

func storeEmailWithAttachment(t *testing.T, s *Storage, accountID string) (*Email, *AttachmentMeta) {
	t.Helper()
	att := &AttachmentMeta{
		ID:          randomID(16),
		Filename:    "report.pdf",
		ContentType: "application/pdf",
		Size:        4,
		Data:        []byte("data"),
	}
	e := &Email{
		ID:          randomID(16),
		AccountID:   accountID,
		FromAddr:    "x@y.com",
		Subject:     "has attachment",
		ReceivedAt:  time.Now(),
		Attachments: []*AttachmentMeta{att},
	}
	if err := s.StoreEmail(e); err != nil {
		t.Fatalf("StoreEmail: %v", err)
	}
	return e, att
}

func TestStorage_AttachmentStoredAndRetrieved(t *testing.T) {
	s := newTestStorage(t)
	insertAccountAt(t, s, "acc1", time.Now())

	email, att := storeEmailWithAttachment(t, s, "acc1")

	data, meta, err := s.GetAttachmentData("acc1", email.ID, att.ID)
	if err != nil {
		t.Fatalf("GetAttachmentData: %v", err)
	}
	if meta == nil {
		t.Fatal("expected attachment metadata, got nil")
	}
	if meta.Filename != "report.pdf" {
		t.Errorf("filename: got %q, want %q", meta.Filename, "report.pdf")
	}
	if string(data) != "data" {
		t.Errorf("data: got %q, want %q", string(data), "data")
	}
}

func TestStorage_ListAttachments(t *testing.T) {
	s := newTestStorage(t)
	insertAccountAt(t, s, "acc1", time.Now())

	email, _ := storeEmailWithAttachment(t, s, "acc1")

	metas, err := s.ListAttachments("acc1", email.ID)
	if err != nil {
		t.Fatalf("ListAttachments: %v", err)
	}
	if len(metas) != 1 {
		t.Fatalf("got %d attachments, want 1", len(metas))
	}
	// Data must NOT be loaded by ListAttachments.
	if metas[0].Data != nil {
		t.Error("ListAttachments should not populate Data field")
	}
}

func TestStorage_GetEmail_PopulatesAttachments(t *testing.T) {
	s := newTestStorage(t)
	insertAccountAt(t, s, "acc1", time.Now())

	email, _ := storeEmailWithAttachment(t, s, "acc1")

	got, err := s.GetEmail("acc1", email.ID)
	if err != nil || got == nil {
		t.Fatalf("GetEmail: %v, %v", got, err)
	}
	if len(got.Attachments) != 1 {
		t.Errorf("GetEmail: got %d attachments, want 1", len(got.Attachments))
	}
	if got.AttachmentCount != 1 {
		t.Errorf("AttachmentCount: got %d, want 1", got.AttachmentCount)
	}
}

func TestStorage_AttachmentCount_InListEmails(t *testing.T) {
	s := newTestStorage(t)
	insertAccountAt(t, s, "acc1", time.Now())

	storeEmailWithAttachment(t, s, "acc1")

	page, err := s.ListEmails("acc1", 10, time.Time{})
	if err != nil {
		t.Fatalf("ListEmails: %v", err)
	}
	if len(page.Emails) != 1 {
		t.Fatalf("got %d emails, want 1", len(page.Emails))
	}
	if page.Emails[0].AttachmentCount != 1 {
		t.Errorf("AttachmentCount in list: got %d, want 1", page.Emails[0].AttachmentCount)
	}
}

func TestStorage_AttachmentIDOR(t *testing.T) {
	s := newTestStorage(t)
	insertAccountAt(t, s, "acc1", time.Now())
	insertAccountAt(t, s, "acc2", time.Now())

	email, att := storeEmailWithAttachment(t, s, "acc1")

	// acc2 must not be able to access acc1's attachment.
	data, meta, err := s.GetAttachmentData("acc2", email.ID, att.ID)
	if err != nil {
		t.Fatalf("GetAttachmentData: %v", err)
	}
	if data != nil || meta != nil {
		t.Error("IDOR: acc2 should not be able to access acc1 attachment")
	}
}

// ── API tests ────────────────────────────────────────────────────────────────

func TestAPI_DownloadAttachment(t *testing.T) {
	s := newTestStorage(t)
	_, token, _ := s.CreateAccount()
	accounts, _ := s.ListEmails("", 1, time.Time{})
	_ = accounts

	// Create account properly and get its ID.
	id, tok, err := s.CreateAccount()
	if err != nil {
		t.Fatal(err)
	}
	_ = tok

	att := &AttachmentMeta{
		ID:          randomID(16),
		Filename:    "hello.txt",
		ContentType: "text/plain",
		Size:        5,
		Data:        []byte("hello"),
	}
	email := &Email{
		ID:          randomID(16),
		AccountID:   id,
		FromAddr:    "x@y.com",
		ReceivedAt:  time.Now(),
		Attachments: []*AttachmentMeta{att},
	}
	if err := s.StoreEmail(email); err != nil {
		t.Fatal(err)
	}

	cfg := &Config{Domain: "test.local"}
	r := buildRouter(cfg, s, NewHub(), NewPushService(cfg))

	// List attachments.
	req := httptest.NewRequest(http.MethodGet,
		fmt.Sprintf("/accounts/%s/emails/%s/attachments", id, email.ID), nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	// token is from a different account — expect 401
	if w.Code != http.StatusUnauthorized {
		t.Errorf("wrong account token: got %d, want 401", w.Code)
	}

	// Use the correct token.
	req = httptest.NewRequest(http.MethodGet,
		fmt.Sprintf("/accounts/%s/emails/%s/attachments", id, email.ID), nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("list attachments: got %d, want 200", w.Code)
	}
	var metas []*AttachmentMeta
	if err := json.NewDecoder(w.Body).Decode(&metas); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(metas) != 1 || metas[0].Filename != "hello.txt" {
		t.Errorf("unexpected metadata: %+v", metas)
	}

	// Download the attachment.
	req = httptest.NewRequest(http.MethodGet,
		fmt.Sprintf("/accounts/%s/emails/%s/attachments/%s", id, email.ID, att.ID), nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("download: got %d, want 200", w.Code)
	}
	if body := w.Body.String(); body != "hello" {
		t.Errorf("body: got %q, want %q", body, "hello")
	}
	if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/plain") {
		t.Errorf("Content-Type: got %q, want text/plain", ct)
	}
}
