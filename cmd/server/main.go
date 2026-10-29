package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"supportcenter/internal/ai"
	"supportcenter/internal/app"
	"supportcenter/internal/database"
	"supportcenter/internal/notify"
)

func main() {
	if err := run(); err != nil {
		slog.Error("server stopped", "error", err)
		os.Exit(1)
	}
}
func run() error {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	for _, key := range []string{"DATABASE_URL", "APP_URL", "AUTH0_DOMAIN", "AUTH0_CLIENT_ID", "AUTH0_CLIENT_SECRET", "ATTACHMENT_DIR"} {
		if os.Getenv(key) == "" {
			return errors.New("missing required environment variable: " + key)
		}
	}
	origin := strings.TrimRight(os.Getenv("APP_URL"), "/")
	u, err := url.Parse(origin)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.Path != "" || u.RawQuery != "" || u.Fragment != "" || u.User != nil {
		return errors.New("APP_URL must be an HTTPS origin without a path")
	}
	initCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	store, err := database.Open(initCtx, os.Getenv("DATABASE_URL"))
	if err != nil {
		return errors.New("database connection failed")
	}
	defer store.Pool.Close()
	if err = store.Migrate(initCtx); err != nil {
		return errors.New("database migration failed")
	}
	dir := os.Getenv("ATTACHMENT_DIR")
	if err = os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	if err = os.Chmod(dir, 0700); err != nil {
		return err
	}
	files, err := os.OpenRoot(dir)
	if err != nil {
		return err
	}
	defer files.Close()
	auth, err := app.NewAuth(initCtx, os.Getenv("AUTH0_DOMAIN"), os.Getenv("AUTH0_CLIENT_ID"), os.Getenv("AUTH0_CLIENT_SECRET"), origin)
	if err != nil {
		return err
	}
	aiEnabled := os.Getenv("AI_AUTO_REPLY_ENABLED") == "true"
	aiKey := os.Getenv("OPENCODE_GO_API_KEY")
	if aiEnabled && aiKey == "" {
		return errors.New("AI_AUTO_REPLY_ENABLED requires OPENCODE_GO_API_KEY")
	}
	mailConfig := notify.Config{
		Host: os.Getenv("SMTP_HOST"), Port: os.Getenv("SMTP_PORT"), From: os.Getenv("SMTP_FROM"),
		Username: os.Getenv("SMTP_USERNAME"), Password: os.Getenv("SMTP_PASSWORD"), TLSMode: os.Getenv("SMTP_TLS_MODE"),
	}
	if mailConfig.Enabled() && mailConfig.TLSMode == "" {
		mailConfig.TLSMode = "starttls"
		if mailConfig.Port == "465" {
			mailConfig.TLSMode = "implicit"
		}
	}
	if err = mailConfig.Validate(); err != nil {
		return err
	}
	portalAPI, err := app.PortalAPIFromEnvironment()
	if err != nil {
		return err
	}
	a := &app.App{Store: store, Auth: auth, Files: files, Origin: origin, Log: slog.Default(), AIEnabled: aiEnabled, MailEnabled: mailConfig.Enabled(), PortalAPI: portalAPI}
	if aiEnabled {
		go (&ai.Worker{Store: store, Key: aiKey, Log: slog.Default()}).Run(ctx)
	}
	if mailConfig.Enabled() {
		go (&notify.Worker{Store: store, Sender: notify.SMTP{Config: mailConfig}, Origin: origin, Log: slog.Default()}).Run(ctx)
	}
	go a.Cleanup(ctx)
	go a.AutoClose(ctx)
	address := os.Getenv("LISTEN_ADDR")
	if address == "" {
		address = "127.0.0.1:8080"
	}
	server := &http.Server{Addr: address, Handler: a.Handler(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 60 * time.Second, WriteTimeout: 60 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16 << 10}
	done := make(chan error, 1)
	go func() { slog.Info("support center listening", "address", address); done <- server.ListenAndServe() }()
	select {
	case err = <-done:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		shutdown, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		return server.Shutdown(shutdown)
	}
}
