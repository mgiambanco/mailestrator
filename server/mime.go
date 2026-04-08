package main

import (
	"io"
	"strings"

	gomail "github.com/emersion/go-message/mail"
)

const maxAttachmentBytes = 10 * 1024 * 1024 // 10 MB per attachment

// ParsedMessage holds the fields extracted from a raw email.
type ParsedMessage struct {
	From        string
	Subject     string
	BodyText    string
	BodyHTML    string
	Attachments []ParsedAttachment
}

// ParsedAttachment holds one decoded attachment from a MIME message.
type ParsedAttachment struct {
	Filename    string
	ContentType string
	Data        []byte
}

// ParseMessage parses a raw RFC 5322 message (including MIME multipart)
// and returns the human-readable fields.
//
// It correctly handles:
//   - text/plain
//   - text/html
//   - multipart/alternative  (text + html variants of the same content)
//   - multipart/mixed        (inline parts + attachments — attachments are skipped)
//   - quoted-printable and base64 transfer encodings (handled by go-message)
//   - Non-UTF-8 charsets     (go-message converts to UTF-8 automatically)
func ParseMessage(r io.Reader) (*ParsedMessage, error) {
	mr, err := gomail.CreateReader(r)
	if err != nil {
		// Not a valid MIME message — read as plain text fallback.
		body, _ := io.ReadAll(r)
		return &ParsedMessage{BodyText: string(body)}, nil
	}

	pm := &ParsedMessage{}

	// Extract headers.
	pm.Subject, _ = mr.Header.Subject()

	if addrs, err := mr.Header.AddressList("From"); err == nil && len(addrs) > 0 {
		pm.From = addrs[0].String()
	} else {
		// Fallback to raw header value.
		pm.From = mr.Header.Get("From")
	}

	// Walk the MIME tree.
	walkParts(mr, pm)

	return pm, nil
}

// walkParts iterates all parts of a mail.Reader, recursing into nested
// multipart containers. Attachments are skipped (handled separately in a
// future improvement).
func walkParts(mr *gomail.Reader, pm *ParsedMessage) {
	for {
		p, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			break
		}

		switch h := p.Header.(type) {
		case *gomail.InlineHeader:
			ct, _, _ := h.ContentType()
			body, err := io.ReadAll(p.Body)
			if err != nil {
				continue
			}
			switch strings.ToLower(ct) {
			case "text/plain":
				// Prefer the first plain-text part; don't overwrite if already set.
				if pm.BodyText == "" {
					pm.BodyText = string(body)
				}
			case "text/html":
				if pm.BodyHTML == "" {
					pm.BodyHTML = string(body)
				}
			}

		case *gomail.AttachmentHeader:
			ct, _, _ := h.ContentType()
			filename, _ := h.Filename()
			if filename == "" {
				filename = "attachment"
			}

			// Read up to the limit + 1 byte so we can detect oversized parts
			// without loading the whole blob into memory.
			lr := io.LimitReader(p.Body, maxAttachmentBytes+1)
			data, err := io.ReadAll(lr)
			if err != nil || len(data) > maxAttachmentBytes {
				_, _ = io.Copy(io.Discard, p.Body) // drain remainder
				continue
			}

			pm.Attachments = append(pm.Attachments, ParsedAttachment{
				Filename:    filename,
				ContentType: strings.ToLower(ct),
				Data:        data,
			})
		}
	}
}
