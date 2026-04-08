# Mail Server + iOS Client

A self-hosted email server written in Go paired with a native iOS client in Swift. The server receives email via SMTP, stores it in SQLite, and delivers real-time notifications to the iOS app via APNs push and WebSocket.

---

## Architecture

```
Internet
   │
   ▼ SMTP (port 25)
┌──────────────────────────────────────┐
│            Go Mail Server            │
│                                      │
│  ┌──────────┐  ┌────────────────┐   │
│  │  SMTP    │  │   REST API     │   │
│  │ receiver │  │ + WebSocket    │   │
│  └────┬─────┘  └───────┬────────┘   │
│       │                │            │
│  ┌────▼────────────────▼─────────┐  │
│  │   SQLite (WAL mode)           │  │
│  │   accounts / emails /         │  │
│  │   attachments / device_tokens │  │
│  └───────────────────────────────┘  │
│                                      │
│  ┌──────────┐ ┌──────────────────┐  │
│  │  APNs    │ │ Cleanup + Backup │  │
│  │  client  │ │  goroutines      │  │
│  └────┬─────┘ └──────────────────┘  │
└───────┼──────────────────────────────┘
        │ push
        ▼
  iOS Swift Client
  ┌──────────────────────────────────┐
  │  NavigationSplitView             │
  │  ┌──────────┐ ┌───────────────┐  │
  │  │ Sidebar  │ │  Inbox list   │  │
  │  │ accounts │ │  + search     │  │
  │  │ + labels │ │  + pagination │  │
  │  └──────────┘ └───────────────┘  │
  │  ┌────────────────────────────┐  │
  │  │  Email detail              │  │
  │  │  HTML / plain text         │  │
  │  │  Attachments (QuickLook)   │  │
  │  └────────────────────────────┘  │
  └──────────────────────────────────┘
```

---

## Repository Layout

```
mail/
├── Dockerfile              Multi-stage build (golang:alpine → alpine runtime)
├── docker-compose.yml      One-command deployment
├── .env.example            Environment variable template
│
├── server/
│   ├── main.go             Entry point + service lifecycle (graceful shutdown)
│   ├── config.go           All config via environment variables
│   ├── storage.go          SQLite — schema, CRUD, pagination, cleanup
│   ├── smtp.go             SMTP listener (go-smtp)
│   ├── api.go              REST API + WebSocket (Gin)
│   ├── hub.go              WebSocket hub with heartbeat (ping/pong)
│   ├── push.go             APNs push notifications (apns2)
│   ├── mime.go             MIME parsing — multipart, attachments, charsets
│   ├── spam.go             DNSBL + SPF inbound spam filtering
│   ├── cleanup.go          TTL-based email and account expiry
│   ├── backup.go           Scheduled VACUUM INTO snapshots
│   ├── logger.go           Structured logging (slog, JSON/text)
│   └── go.mod
│
└── ios/MailClient/
    ├── MailClientApp.swift  App entry, push permission, APNs token
    ├── AppDelegate.swift    APNs registration callbacks
    ├── Models.swift         Account, Email, AttachmentMeta, WebSocketEvent
    ├── APIClient.swift      HTTP + WebSocket + attachment download
    ├── MailStore.swift      Observable state, Keychain persistence, badge
    ├── ContentView.swift    NavigationSplitView root
    ├── SidebarView.swift    Account list, labels, copy address
    ├── InboxView.swift      Email list, search, pagination, swipe-to-delete
    ├── EmailDetailView.swift Email reader (HTML via WKWebView + plain text)
    └── AttachmentView.swift  Attachment list + QuickLook download viewer
```

---

## Server

### Requirements

- Go 1.21+ (or Docker — no Go needed on the host)
- A Linux or Windows host with port 25 open (see [Hosting](#hosting))
- An Apple Developer account with a Push Notifications `.p8` key (for APNs)

### Quick start with Docker

```bash
cp .env.example .env          # fill in MAIL_DOMAIN, APNS_* values
cp /path/to/your.p8 apns_key.p8
docker compose up -d
```

The container exposes port 25 (SMTP) and 8080 (API/WebSocket) and stores data in named Docker volumes (`mail-data`, `mail-backups`).

### Build from source

```bash
cd server
go mod tidy
go build -o mailserver .
./mailserver
```

### Install as a system service

```bash
sudo ./mailserver install
sudo ./mailserver start

# Other controls
sudo ./mailserver stop | restart | uninstall
```

On Linux this registers a systemd unit; on Windows a Windows Service.

---

### Configuration

All configuration is via environment variables. Copy `.env.example` to `.env` to get started.

#### Server

| Variable | Default | Description |
|---|---|---|
| `MAIL_DOMAIN` | `localhost` | Your mail domain (e.g. `mail.example.com`) |
| `MAIL_DB_PATH` | `mail.db` | Path to the SQLite database file |
| `MAIL_SMTP_ADDR` | `:25` | SMTP listen address |
| `MAIL_API_ADDR` | `:8080` | REST API + WebSocket listen address |
| `MAIL_TLS_CERT` | _(unset)_ | Path to TLS certificate (enables STARTTLS) |
| `MAIL_TLS_KEY` | _(unset)_ | Path to TLS private key |

#### APNs

| Variable | Default | Description |
|---|---|---|
| `APNS_KEY_PATH` | `apns_key.p8` | Path to Apple `.p8` auth key |
| `APNS_KEY_ID` | _(required)_ | 10-character Apple key ID |
| `APNS_TEAM_ID` | _(required)_ | 10-character Apple Team ID |
| `APNS_BUNDLE_ID` | `com.example.mailclient` | iOS app bundle identifier |
| `APNS_PRODUCTION` | `false` | Set `true` for App Store / TestFlight builds |

#### TTL Cleanup

| Variable | Default | Description |
|---|---|---|
| `EMAIL_TTL_DAYS` | `7` | Delete emails older than N days |
| `ACCOUNT_TTL_DAYS` | `30` | Delete inactive accounts after N days |
| `CLEANUP_INTERVAL` | `24h` | How often the cleanup goroutine runs |

#### Backup

| Variable | Default | Description |
|---|---|---|
| `BACKUP_DIR` | `backups` | Directory for `VACUUM INTO` snapshots |
| `BACKUP_INTERVAL` | `24h` | Snapshot frequency (`0` to disable) |
| `BACKUP_KEEP` | `7` | Number of snapshots to retain |

#### Spam Filtering

| Variable | Default | Description |
|---|---|---|
| `SPAM_DNSBLS` | `zen.spamhaus.org` | Comma-separated DNSBL zones (empty to disable) |
| `SPAM_SPF` | `true` | Enable SPF checks on inbound mail |
| `SPAM_SPF_REJECT` | `true` | Reject on SPF `fail`; `false` = log only |

#### Logging

| Variable | Default | Description |
|---|---|---|
| `LOG_FORMAT` | `json` | `json` (production) or `text` (human-readable) |
| `LOG_LEVEL` | `info` | `debug` \| `info` \| `warn` \| `error` |

---

### API Reference

All endpoints are on `MAIL_API_ADDR` (default `:8080`). Authenticated endpoints require `Authorization: Bearer <token>` (or `?token=<token>` for WebSocket).

#### Accounts

| Method | Path | Auth | Description |
|---|---|---|---|
| `POST` | `/accounts` | — | Create account. Returns `{id, address, token}`. Token shown **once** — client must persist it. Rate-limited: 5/min per IP. |
| `DELETE` | `/accounts/:id` | ✓ | Delete account and all its data. |

#### Emails

| Method | Path | Auth | Description |
|---|---|---|---|
| `GET` | `/accounts/:id/emails` | ✓ | List emails, newest first. Supports `?limit=N&before=<RFC3339>` for cursor pagination. Returns `{emails, has_more}`. |
| `GET` | `/accounts/:id/emails/:emailID` | ✓ | Fetch single email (marks read). Response includes `attachments` array. |
| `DELETE` | `/accounts/:id/emails/:emailID` | ✓ | Delete email. |

#### Attachments

| Method | Path | Auth | Description |
|---|---|---|---|
| `GET` | `/accounts/:id/emails/:emailID/attachments` | ✓ | List attachment metadata (no binary data). |
| `GET` | `/accounts/:id/emails/:emailID/attachments/:attachmentID` | ✓ | Download attachment bytes. Sets `Content-Disposition: attachment`. |

#### Device Tokens

| Method | Path | Auth | Body | Description |
|---|---|---|---|---|
| `POST` | `/accounts/:id/device-token` | ✓ | `{"token":"…"}` | Register APNs device token. |
| `DELETE` | `/accounts/:id/device-token` | ✓ | `{"token":"…"}` | Remove APNs device token. |

#### System

| Method | Path | Auth | Description |
|---|---|---|---|
| `GET` | `/health` | — | Returns `{"status":"ok","checks":{"db":"ok"}}` or HTTP 503. |

#### WebSocket

| Path | Auth | Description |
|---|---|---|
| `GET /ws/:id?token=<bearer>` | ✓ (query param) | Real-time events. Server sends `{"type":"new_email","email":{…}}`. Server pings every 30 s; client must pong within 60 s. |

---

### Security notes

- Account IDs and tokens are generated with `crypto/rand`. Tokens are stored as SHA-256 hashes; comparison uses `hmac.Equal` (constant-time).
- All email/attachment queries include `account_id` scope checks (no IDOR).
- Per-account email cap: 500 messages.
- WebSocket read limit: 4 KB per frame.
- Account creation rate-limited: 5 per minute per IP.
- `PRAGMA foreign_keys = ON` enforces cascading deletes at the DB layer.
- `PRAGMA journal_mode = WAL` with `synchronous = NORMAL` for concurrent reads and fast writes.
- Inbound spam filtered via DNSBL (Spamhaus ZEN by default) and SPF. Both checks fail-open — DNS errors never block legitimate mail.
- Attachment blobs capped at 10 MB each; oversized parts are silently discarded.
- Enable TLS in production via `MAIL_TLS_CERT` / `MAIL_TLS_KEY` and set `AllowInsecureAuth = false` in `smtp.go`.

---

## iOS Client

### Requirements

- Xcode 15+
- iOS 16.4+ deployment target
- Physical iPhone or iPad (APNs requires a real device)
- Apple Developer Program membership (Push Notifications entitlement)

### Setup with XcodeGen

```bash
cd ios
brew install xcodegen      # if not already installed
xcodegen generate
open MailClient.xcodeproj
```

### Manual Xcode setup

1. Create a new Xcode project: **iOS App**, SwiftUI interface, named `MailClient`.
2. Add all `.swift` files from `ios/MailClient/` to the project.
3. In **Signing & Capabilities**, add **Push Notifications**.
4. Set your server address in `APIClient.swift`:
   ```swift
   var baseURL   = URL(string: "https://mail.example.com:8080")!
   var wsBaseURL = "wss://mail.example.com:8080"
   ```
5. Set your bundle ID to match `APNS_BUNDLE_ID` on the server.
6. Build and run on a real device.

### Features

| Feature | Description |
|---|---|
| **Multiple accounts** | Tap `+` to create a random 6-character address (e.g. `a3f9k2@mail.example.com`). Hold as many accounts as you like. |
| **Account labels** | Long-press an account in the sidebar to rename it. The address is shown as a secondary line when a label is active. |
| **Copy address** | Long-press an account → "Copy Address", or tap the copy icon in the inbox toolbar. |
| **Real-time inbox** | New emails appear instantly via WebSocket. Server pings every 30 s; client reconnects with exponential backoff on disconnect. |
| **Push notifications** | APNs wakes the app when email arrives in the background. Tapping a notification opens the message directly. |
| **App icon badge** | Badge shows total unread count across all accounts; clears as you read messages. |
| **Attachments** | Tap any attachment to download and preview it via QuickLook. Paperclip icon in the list indicates emails with attachments. |
| **Search** | Filter the inbox by sender, subject, or body — instant, client-side. |
| **Pagination** | "Load More" footer appends older emails; cursor-based (no repeated entries on new arrivals). |
| **HTML email** | Rendered via WKWebView with JavaScript disabled (XSS prevention). |
| **Swipe to delete** | Swipe left on any email or full-swipe to delete immediately. |
| **Pull to refresh** | Pull down the inbox to force-reload from the server. |

### Security notes

- Account list and tokens stored in the iOS **Keychain** — encrypted at rest, excluded from unencrypted backups.
- JavaScript disabled in the HTML WebView to prevent script injection from malicious emails.
- APNs device token never logged in production builds.
- All responses validated for HTTP 2xx before decoding.
- Use `https://` and `wss://` in production — required for Apple App Transport Security (ATS).

---

## Hosting

Port 25 must be open. Most cloud providers block it by default.

| Provider | Port 25 | Notes |
|---|---|---|
| **DigitalOcean** | Open by default | $6/mo Droplet, easiest setup |
| **Hetzner Cloud** | Open by default | €4/mo CX11, cheapest option |
| **Linode / Akamai** | Open by default | Similar to DigitalOcean |
| **AWS EC2** | Blocked | Requires a manual removal request to AWS |
| **Google Cloud** | Blocked | Cannot be unblocked on standard tiers |
| **Azure** | Blocked | Requires support ticket |

### DNS setup

```
# Direct inbound email to your server
MX   mail.example.com.   10   <your-server-ip>

# Resolve the mail hostname
A    mail.example.com.        <your-server-ip>

# Reverse DNS — improves deliverability, set via your hosting provider
PTR  <your-server-ip>         mail.example.com.

# SPF record — authorises your server to send (if you send outbound mail)
TXT  mail.example.com.        "v=spf1 a mx ~all"
```

### Production checklist

- [ ] TLS certificate (Let's Encrypt via `certbot`), set `MAIL_TLS_CERT` / `MAIL_TLS_KEY`
- [ ] Set `AllowInsecureAuth = false` in `smtp.go` once TLS is configured
- [ ] Firewall: allow 25 (SMTP), 8080 (API), block everything else
- [ ] Set `APNS_PRODUCTION=true` for App Store / TestFlight builds
- [ ] Configure PTR / reverse DNS for your server IP
- [ ] Add SPF TXT record for your domain
- [ ] Set `LOG_FORMAT=json` and pipe logs to a log aggregator (Loki, Datadog, etc.)
- [ ] Verify `GET /health` returns 200 from your monitoring tool

---

## Dependencies

### Server

| Package | Version | Purpose |
|---|---|---|
| `github.com/emersion/go-smtp` | v0.20 | SMTP server |
| `github.com/emersion/go-message` | v0.18 | MIME email parsing |
| `github.com/gin-gonic/gin` | v1.9 | HTTP router + middleware |
| `github.com/gorilla/websocket` | v1.5 | WebSocket |
| `github.com/sideshow/apns2` | v0.25 | APNs push notifications |
| `github.com/kardianos/service` | v1.2 | Cross-platform service management |
| `blitiri.com.ar/go/spf` | v1.5 | SPF sender verification |
| `modernc.org/sqlite` | v1.29 | Pure-Go SQLite (no CGO) |

### iOS

| Framework | Purpose |
|---|---|
| SwiftUI | UI |
| WebKit | HTML email rendering |
| QuickLook | Attachment preview |
| UserNotifications | APNs push + badge count |
| Security | Keychain storage |
| Foundation | Networking (URLSession, WebSocket) |
| UniformTypeIdentifiers | Attachment MIME type icons |
