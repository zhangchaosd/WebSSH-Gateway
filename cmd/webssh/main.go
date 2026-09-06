package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"webssh/internal/auth"
	"webssh/internal/config"
	"webssh/internal/httpapi"
	"webssh/internal/security"
	"webssh/internal/sshgw"
	"webssh/internal/store"
)

var version = "1.0.0"

func main() {
	if e := run(); e != nil {
		slog.Error("webssh stopped", "error", e)
		os.Exit(1)
	}
}
func run() error {
	cfg, e := config.Load()
	if e != nil {
		return e
	}
	if len(os.Args) > 1 && os.Args[1] == "healthcheck" {
		return healthcheck(cfg.Listen)
	}
	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(log)
	cipher, created, e := security.LoadOrCreateCipher(cfg.MasterKeyFile)
	if e != nil {
		return fmt.Errorf("master key: %w", e)
	}
	if created {
		log.Info("created master key", "path", cfg.MasterKeyFile)
	}
	st, e := store.Open(cfg.DatabasePath)
	if e != nil {
		return fmt.Errorf("database: %w", e)
	}
	defer st.Close()
	if len(os.Args) > 1 && os.Args[1] == "backup" {
		return backup(st, cfg.DatabasePath, os.Args[2:])
	}
	users, e := st.UserCount()
	if e != nil {
		return e
	}
	if users == 0 {
		if cfg.AdminPassword == "" {
			return errors.New("WEBSSH_ADMIN_PASSWORD is required for first startup")
		}
		hash, e := auth.HashPassword(cfg.AdminPassword)
		if e != nil {
			return e
		}
		made, e := st.EnsureAdmin(cfg.AdminUser, hash)
		if e != nil {
			return e
		}
		if made {
			log.Warn("initial administrator created", "username", cfg.AdminUser, "notice", "change the bootstrap password after first login")
		}
	}
	am := auth.NewManager(cfg.SessionIdleTimeout, cfg.CookieSecure)
	gw := sshgw.New(cipher, sshgw.Settings{ConnectTimeout: cfg.ConnectTimeout, HandshakeTimeout: cfg.HandshakeTimeout, Keepalive: cfg.KeepaliveInterval, DefaultTerm: cfg.DefaultTerm, Listen: cfg.Listen})
	api := httpapi.New(cfg, st, cipher, am, gw, log)
	srv := &http.Server{Addr: cfg.Listen, Handler: api.Handler(), ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 2 * time.Minute, MaxHeaderBytes: 1 << 20}
	done := make(chan os.Signal, 1)
	signal.Notify(done, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-done
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
	}()
	log.Info("WebSSH Gateway ready", "version", version, "listen", cfg.Listen, "database", cfg.DatabasePath)
	e = srv.ListenAndServe()
	if errors.Is(e, http.ErrServerClosed) {
		return nil
	}
	return e
}
func healthcheck(listen string) error {
	host, port, e := net.SplitHostPort(listen)
	if e != nil {
		return e
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	client := http.Client{Timeout: 2 * time.Second}
	res, e := client.Get("http://" + net.JoinHostPort(host, port) + "/api/v1/healthz")
	if e != nil {
		return e
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("healthcheck returned %s", res.Status)
	}
	return nil
}
func backup(st *store.Store, dbPath string, args []string) error {
	if len(args) != 2 || args[0] != "--output" {
		return errors.New("usage: webssh backup --output <path>")
	}
	dst, e := filepath.Abs(args[1])
	if e != nil {
		return e
	}
	src, e := filepath.Abs(dbPath)
	if e != nil {
		return e
	}
	if src == dst {
		return errors.New("backup output must differ from database path")
	}
	if _, e = st.DB.Exec(`PRAGMA wal_checkpoint(FULL)`); e != nil {
		return e
	}
	b, e := os.ReadFile(src)
	if e != nil {
		return e
	}
	if e = os.WriteFile(dst, b, 0600); e != nil {
		return e
	}
	fmt.Printf("backup written to %s\n", dst)
	return nil
}
