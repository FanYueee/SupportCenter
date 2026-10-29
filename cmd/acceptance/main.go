// Separate executable for functional acceptance before Auth0 tenant integration.
package main

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5/pgxpool"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"supportcenter/internal/acceptance"
	"supportcenter/internal/app"
	"supportcenter/internal/database"
	"supportcenter/internal/model"
	"syscall"
	"time"
)

func main() {
	if err := run(); err != nil {
		slog.Error("acceptance stopped", "error", err)
		os.Exit(1)
	}
}
func run() error {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	origin := strings.TrimRight(os.Getenv("APP_URL"), "/")
	u, err := url.Parse(origin)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.Path != "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return errors.New("APP_URL must be an HTTPS origin")
	}
	password := os.Getenv("ACCEPTANCE_PASSWORD")
	if len(password) < 32 {
		return errors.New("ACCEPTANCE_PASSWORD must contain at least 32 characters")
	}
	cfg, err := pgxpool.ParseConfig(os.Getenv("DATABASE_URL"))
	if err != nil || cfg.ConnConfig.Database != "support_acceptance" {
		return errors.New("acceptance requires a separate database named support_acceptance")
	}
	startup, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	store, err := database.Open(startup, os.Getenv("DATABASE_URL"))
	if err != nil {
		return errors.New("acceptance database unavailable")
	}
	defer store.Pool.Close()
	if err = store.Migrate(startup); err != nil {
		return err
	}
	dir := os.Getenv("ATTACHMENT_DIR")
	if dir == "" {
		return errors.New("ATTACHMENT_DIR is required")
	}
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
	accounts := map[string]model.User{}
	for _, account := range []struct{ Key, Name, Role string }{{"customer", "驗收客戶 A", "user"}, {"customer2", "驗收客戶 B", "user"}, {"support", "驗收客服", "support"}} {
		user, e := store.User(startup, "acceptance|"+account.Key, account.Name, account.Key+"@example.invalid")
		if e != nil {
			return e
		}
		if e = store.SetRole(startup, "acceptance|"+account.Key, account.Role); e != nil {
			return e
		}
		user.Role = account.Role
		accounts[account.Key] = user
	}
	core := &app.App{Store: store, Files: files, Origin: origin, Log: slog.Default()}
	go core.Cleanup(ctx)
	address := os.Getenv("LISTEN_ADDR")
	if address == "" {
		address = "127.0.0.1:8082"
	}
	server := &http.Server{Addr: address, Handler: acceptance.Handler(core.Handler(), store, origin, password, accounts), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 60 * time.Second, WriteTimeout: 60 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16 << 10}
	done := make(chan error, 1)
	go func() { slog.Info("acceptance listening", "address", address); done <- server.ListenAndServe() }()
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
