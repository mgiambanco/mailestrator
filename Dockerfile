# ── Stage 1: build ────────────────────────────────────────────────────────────
FROM golang:1.26-alpine AS builder

WORKDIR /src

# Cache dependency downloads separately from source changes.
COPY server/go.mod server/go.sum ./
RUN go mod download

COPY server/ ./

# CGO_ENABLED=0: modernc/sqlite is pure Go — no C toolchain needed.
# -trimpath: strip local paths from the binary for reproducibility.
# -ldflags: omit debug info and symbol table to shrink the binary.
RUN CGO_ENABLED=0 GOOS=linux go build \
    -trimpath \
    -ldflags="-s -w" \
    -o /mailserver \
    .

# ── Stage 2: runtime ──────────────────────────────────────────────────────────
# alpine (~5 MB) over scratch so wget is available for the HEALTHCHECK.
FROM alpine:3.21

# Non-root user for the server process.
RUN adduser -D -u 1000 mail

COPY --from=builder /mailserver /mailserver

# Directories owned by the service user.
RUN mkdir -p /data /backups && chown mail:mail /data /backups

USER mail

VOLUME ["/data", "/backups"]

EXPOSE 25 8080

HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
    CMD wget -qO- http://localhost:8080/health | grep -q '"status":"ok"'

ENTRYPOINT ["/mailserver"]
