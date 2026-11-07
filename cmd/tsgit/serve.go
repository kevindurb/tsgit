package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/kevindurb/tsgit/internal/config"
	"github.com/kevindurb/tsgit/internal/identity"
	"github.com/kevindurb/tsgit/internal/repos"
	"github.com/kevindurb/tsgit/internal/transport"
)

const shutdownTimeout = 10 * time.Second

// devListen is a seam so the development server can be exercised against a
// listener a test already owns.
var devListen = net.Listen

func serve(ctx context.Context, cfg *config.Config, stderr io.Writer) error {
	logger := newLogger(cfg, stderr)
	if cfg.DevListen != "" {
		return serveDev(ctx, cfg, logger)
	}
	return serveTailnet(ctx, cfg, logger)
}

func serveDev(ctx context.Context, cfg *config.Config, logger *slog.Logger) error {
	dev, err := config.PrepareDev()
	if err != nil {
		return err
	}
	defer dev.Cleanup()

	cfg.RepoRoot = dev.RepoRoot
	store, err := repos.Open(cfg.RepoRoot, cfg.DefaultBranch)
	if err != nil {
		return err
	}

	handler := transport.NewHandler(
		identity.Fixed{Identity: identity.Development()},
		store,
		&transport.GitBin{Git: store.GitPath(), Log: logger},
		logger,
	)

	ln, err := devListen("tcp", cfg.DevListen)
	if err != nil {
		return fmt.Errorf("listen %s: %w", cfg.DevListen, err)
	}
	logger.Warn("development mode: no tailnet and no authentication", "listen", ln.Addr().String(), "root", store.Root())

	srv := &http.Server{Handler: handler, ReadHeaderTimeout: 10 * time.Second}
	return runHTTP(ctx, srv, ln, logger)
}

func runHTTP(ctx context.Context, srv *http.Server, ln net.Listener, logger *slog.Logger) error {
	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.Serve(ln)
	}()

	select {
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		logger.Info("shutting down", "listen", ln.Addr().String())
		if err := srv.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("shutdown: %w", err)
		}
		err := <-errCh
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}

func newLogger(cfg *config.Config, w io.Writer) *slog.Logger {
	level := slog.LevelInfo
	switch strings.ToLower(cfg.Log) {
	case "debug":
		level = slog.LevelDebug
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	}
	return slog.New(slog.NewTextHandler(w, &slog.HandlerOptions{Level: level}))
}
