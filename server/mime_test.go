package main

import (
	"strings"
	"testing"
)

// rawEmail builds a minimal RFC 5322 message from the given headers and body.
func rawEmail(headers map[string]string, body string) string {
	var sb strings.Builder
	for k, v := range headers {
		sb.WriteString(k)
		sb.WriteString(": ")
		sb.WriteString(v)
		sb.WriteString("\r\n")
	}
	sb.WriteString("\r\n")
	sb.WriteString(body)
	return sb.String()
}

func TestParsePlainText(t *testing.T) {
	raw := rawEmail(map[string]string{
		"From":         "Alice <alice@example.com>",
		"Subject":      "Hello",
		"Content-Type": "text/plain; charset=utf-8",
	}, "This is a plain text email.")

	pm, err := ParseMessage(strings.NewReader(raw))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pm.Subject != "Hello" {
		t.Errorf("subject: got %q, want %q", pm.Subject, "Hello")
	}
	if !strings.Contains(pm.From, "alice@example.com") {
		t.Errorf("from: got %q, want alice@example.com", pm.From)
	}
	if pm.BodyText != "This is a plain text email." {
		t.Errorf("body_text: got %q", pm.BodyText)
	}
	if pm.BodyHTML != "" {
		t.Errorf("body_html should be empty, got %q", pm.BodyHTML)
	}
}

func TestParseHTMLOnly(t *testing.T) {
	raw := rawEmail(map[string]string{
		"From":         "bob@example.com",
		"Subject":      "HTML mail",
		"Content-Type": "text/html; charset=utf-8",
	}, "<h1>Hello</h1><p>World</p>")

	pm, err := ParseMessage(strings.NewReader(raw))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pm.BodyHTML != "<h1>Hello</h1><p>World</p>" {
		t.Errorf("body_html: got %q", pm.BodyHTML)
	}
	if pm.BodyText != "" {
		t.Errorf("body_text should be empty, got %q", pm.BodyText)
	}
}

// TestParseMultipartAlternative covers the most common real-world email format:
// multipart/alternative with a text/plain and a text/html part.
func TestParseMultipartAlternative(t *testing.T) {
	const boundary = "boundary123"
	body := "--" + boundary + "\r\n" +
		"Content-Type: text/plain; charset=utf-8\r\n\r\n" +
		"Plain text version\r\n" +
		"--" + boundary + "\r\n" +
		"Content-Type: text/html; charset=utf-8\r\n\r\n" +
		"<p>HTML version</p>\r\n" +
		"--" + boundary + "--\r\n"

	raw := rawEmail(map[string]string{
		"From":         "carol@example.com",
		"Subject":      "Multipart",
		"Content-Type": `multipart/alternative; boundary="` + boundary + `"`,
	}, body)

	pm, err := ParseMessage(strings.NewReader(raw))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(pm.BodyText, "Plain text version") {
		t.Errorf("body_text missing plain part, got %q", pm.BodyText)
	}
	if !strings.Contains(pm.BodyHTML, "HTML version") {
		t.Errorf("body_html missing html part, got %q", pm.BodyHTML)
	}
}

// TestParseMultipartMixed covers multipart/mixed which is used when an email
// has both a body and file attachments.
func TestParseMultipartMixed(t *testing.T) {
	const boundary = "mixedboundary"
	body := "--" + boundary + "\r\n" +
		"Content-Type: text/plain; charset=utf-8\r\n\r\n" +
		"See the attached file.\r\n" +
		"--" + boundary + "\r\n" +
		"Content-Type: application/octet-stream\r\n" +
		"Content-Disposition: attachment; filename=\"file.bin\"\r\n\r\n" +
		"binarydata\r\n" +
		"--" + boundary + "--\r\n"

	raw := rawEmail(map[string]string{
		"From":         "dave@example.com",
		"Subject":      "With attachment",
		"Content-Type": `multipart/mixed; boundary="` + boundary + `"`,
	}, body)

	pm, err := ParseMessage(strings.NewReader(raw))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(pm.BodyText, "See the attached file.") {
		t.Errorf("body_text: got %q", pm.BodyText)
	}
	// Attachment should be silently skipped — not leaked into body.
	if strings.Contains(pm.BodyText, "binarydata") {
		t.Errorf("attachment data leaked into body_text")
	}
}

// TestParseQuotedPrintable verifies that quoted-printable encoding is decoded.
func TestParseQuotedPrintable(t *testing.T) {
	// "Héllo" in quoted-printable
	raw := rawEmail(map[string]string{
		"From":                     "eve@example.com",
		"Subject":                  "=?utf-8?q?Encoded_subject?=",
		"Content-Type":             "text/plain; charset=utf-8",
		"Content-Transfer-Encoding": "quoted-printable",
	}, "H=C3=A9llo World")

	pm, err := ParseMessage(strings.NewReader(raw))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(pm.BodyText, "Héllo") {
		t.Errorf("quoted-printable not decoded, got %q", pm.BodyText)
	}
}

// TestExtractLocalPart covers the SMTP address parsing helper.
func TestExtractLocalPart(t *testing.T) {
	tests := []struct {
		addr   string
		domain string
		want   string
	}{
		{"abc123@example.com", "example.com", "abc123"},
		{"<abc123@example.com>", "example.com", "abc123"},
		{"ABC123@EXAMPLE.COM", "example.com", "abc123"}, // case folding
		{"abc123@other.com", "example.com", ""},          // wrong domain
		{"notanemail", "example.com", ""},
		{"@example.com", "example.com", ""},
	}
	for _, tt := range tests {
		got := extractLocalPart(tt.addr, tt.domain)
		if got != tt.want {
			t.Errorf("extractLocalPart(%q, %q) = %q, want %q", tt.addr, tt.domain, got, tt.want)
		}
	}
}
