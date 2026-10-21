// Command server is the probe server binary. It serves the admin API, the
// one-way agent REST/WebSocket endpoints, and (in later milestones) the
// embedded web UI.
package main

import (
	"context"
	"crypto/tls"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"probe/server/internal/api"
	"probe/server/internal/config"
	"probe/server/internal/repository"
)

func main() {
	configPath := flag.String("config", "", "path to config.yaml (empty uses safe loopback defaults)")
	flag.Parse()
	if err := run(*configPath); err != nil {
		slog.Error("probe-server", "error", err)
		os.Exit(1)
	}
}

func run(configPath string) error {
	cfg, err := config.Load(configPath)
	if err != nil {
		return err
	}
	logger := newLogger(cfg.Log.Level)
	slog.SetDefault(logger)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	db, err := repository.OpenWithBusyTimeout(ctx, filepath.Join(cfg.DataDir, "probe.db"), cfg.Storage.BusyTimeoutMS)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer db.Close()
	if err := repository.Migrate(ctx, db); err != nil {
		return fmt.Errorf("apply migrations: %w", err)
	}
	if err := repository.NewAgentRepo(db).ResetOnline(ctx); err != nil {
		return fmt.Errorf("reset online state: %w", err)
	}

	app, err := api.New(cfg, db, logger)
	if err != nil {
		return err
	}
	go app.RunMaintenance(ctx)
	go app.RunAlertEngine(ctx)
	setupToken, err := app.EnsureSetupToken(ctx)
	if err != nil {
		return fmt.Errorf("prepare setup token: %w", err)
	}
	if setupToken != "" {
		logger.Warn("first-run setup required",
			"url", strings.TrimRight(cfg.PublicURL, "/")+"/setup",
			"token", setupToken,
			"token_file", filepath.Join(cfg.DataDir, "setup_token"))
	}

	httpServer := &http.Server{
		Addr:              cfg.Listen,
		Handler:           app.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}

	listener, err := net.Listen("tcp", cfg.Listen)
	if err != nil {
		return fmt.Errorf("listen %s: %w", cfg.Listen, err)
	}
	scheme := "http"
	if cfg.TLS.Mode == "files" {
		cert, err := tls.LoadX509KeyPair(cfg.TLS.Cert, cfg.TLS.Key)
		if err != nil {
			_ = listener.Close()
			return fmt.Errorf("load TLS certificate: %w", err)
		}
		httpServer.TLSConfig = &tls.Config{
			Certificates: []tls.Certificate{cert},
			MinVersion:   tls.VersionTLS12,
		}
		listener = tls.NewListener(listener, httpServer.TLSConfig)
		scheme = "https"
	}
	logger.Info("probe-server listening",
		"address", cfg.Listen,
		"scheme", scheme,
		"public_url", cfg.PublicURL,
		"proxy_mode", cfg.Proxy.Mode)

	errCh := make(chan error, 1)
	go func() {
		errCh <- httpServer.Serve(listener)
	}()

	select {
	case <-ctx.Done():
		logger.Info("shutting down")
	case err := <-errCh:
		if err != nil && err != http.ErrServerClosed {
			return fmt.Errorf("serve: %w", err)
		}
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		logger.Warn("shutdown", "error", err)
	}
	return nil
}

func newLogger(level string) *slog.Logger {
	var l slog.Level
	switch level {
	case "debug":
		l = slog.LevelDebug
	case "warn":
		l = slog.LevelWarn
	case "error":
		l = slog.LevelError
	default:
		l = slog.LevelInfo
	}
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: l}))
}
