package main

import (
	"context"
	"log"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/emersion/go-smtp"
	"github.com/kardianos/service"
)

type program struct {
	cfg         *Config
	store       *Storage
	hub         *Hub
	push        *PushService
	smtpServer  *smtp.Server
	httpServer  *http.Server
	cleanupStop chan struct{}
	backupStop  chan struct{}
}

func (p *program) Start(s service.Service) error {
	cfg := LoadConfig()
	p.cfg = cfg

	SetupLogger(cfg.LogFormat, cfg.LogLevel)

	store, err := NewStorage(cfg.DBPath)
	if err != nil {
		return err
	}
	p.store = store
	p.hub = NewHub()
	p.push = NewPushService(cfg)

	p.smtpServer = StartSMTP(cfg, store, p.hub, p.push)
	p.httpServer = StartAPI(cfg, store, p.hub, p.push)

	p.cleanupStop = make(chan struct{})
	go StartCleanup(cfg, store, p.cleanupStop)

	p.backupStop = make(chan struct{})
	go StartBackup(cfg, store, p.backupStop)

	return nil
}

func (p *program) Stop(s service.Service) error {
	slog.Info("shutdown: starting graceful shutdown", "timeout", "30s")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// 0. Stop background goroutines so they don't touch the DB while we drain.
	if p.cleanupStop != nil {
		close(p.cleanupStop)
	}
	if p.backupStop != nil {
		close(p.backupStop)
	}

	// 1. Stop accepting new SMTP connections first so no new emails arrive
	//    while we're draining.
	if p.smtpServer != nil {
		if err := p.smtpServer.Close(); err != nil {
			slog.Error("shutdown: smtp", "err", err)
		} else {
			slog.Info("shutdown: smtp stopped")
		}
	}

	// 2. Send WebSocket close frames before the HTTP server goes away.
	//    Long-lived WS connections won't drain on their own via Shutdown().
	if p.hub != nil {
		p.hub.Shutdown()
		slog.Info("shutdown: websocket clients closed")
	}

	// 3. Drain in-flight HTTP requests; Shutdown() waits until they complete
	//    or the context deadline fires.
	if p.httpServer != nil {
		if err := p.httpServer.Shutdown(ctx); err != nil {
			slog.Error("shutdown: http drain", "err", err)
		} else {
			slog.Info("shutdown: http drained")
		}
	}

	// 4. Close the database only after all requests have finished so no
	//    in-flight handler touches the DB after it's closed.
	if p.store != nil {
		if err := p.store.Close(); err != nil {
			slog.Error("shutdown: db", "err", err)
		} else {
			slog.Info("shutdown: db closed")
		}
	}

	slog.Info("shutdown: complete")
	return nil
}

func main() {
	svcConfig := &service.Config{
		Name:        "MailServer",
		DisplayName: "Mail Server",
		Description: "Custom email server with iOS push support",
	}

	prg := &program{}
	svc, err := service.New(prg, svcConfig)
	if err != nil {
		log.Fatal(err)
	}

	// Sub-commands: install | uninstall | start | stop | restart
	if len(os.Args) > 1 {
		cmd := os.Args[1]
		switch cmd {
		case "install", "uninstall", "start", "stop", "restart":
			if err := service.Control(svc, cmd); err != nil {
				log.Fatalf("service control %q: %v", cmd, err)
			}
			log.Printf("service %q OK", cmd)
			return
		}
	}

	// kardianos/service handles SIGINT/SIGTERM in interactive mode and calls
	// program.Stop() automatically — no manual signal handling needed.
	if err := svc.Run(); err != nil {
		log.Fatal(err)
	}
}
