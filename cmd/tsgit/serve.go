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
	"github.com/kevindurb/tsgit/internal/web"
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

	resolver := identity.Fixed{Identity: identity.Development()}
	gitHandler := transport.NewHandler(
		resolver,
		store,
		&transport.GitBin{Git: store.GitPath(), Log: logger},
		logger,
	)
	ui := web.New(web.Config{
		Resolver: resolver,
		Store:    store,
		Log:      logger,
		Version:  version,
	})
	handler := front(gitHandler, ui)

	ln, err := devListen("tcp", cfg.DevListen)
	if err != nil {
		return fmt.Errorf("listen %s: %w", cfg.DevListen, err)
	}
	logger.Warn("development mode: no tailnet and no authentication", "listen", ln.Addr().String(), "root", store.Root())

	srv := &http.Server{Handler: handler, ReadHeaderTimeout: 10 * time.Second}
	return runHTTP(ctx, srv, ln, logger)
}

// front routes requests the smart-HTTP handler recognizes to git and every
// other path to the discovery front end. Dispatch is parse-based so a
// malformed git request is never served as a repository summary.
func front(git, ui http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if transport.Matches(r) {
			git.ServeHTTP(w, r)
			return
		}
		ui.ServeHTTP(w, r)
	})
}

func runHTTP(ctx context.Context, srv *http.Server, ln net.Listener, logger *slog.Logger) error {
	return runServers(ctx, logger, serverRun{srv: srv, ln: ln})
}

// serverRun is one HTTP server bound to a listener.
type serverRun struct {
	srv *http.Server
	ln  net.Listener
}

// runServers serves every listener, then shuts all of them down together. The
// first non-shutdown error is returned. In-flight requests drain for up to
// shutdownTimeout.
func runServers(ctx context.Context, logger *slog.Logger, servers ...serverRun) error {
	if len(servers) == 0 {
		return nil
	}
	errCh := make(chan error, len(servers))
	for _, s := range servers {
		go func(s serverRun) { errCh <- s.srv.Serve(s.ln) }(s)
	}

	var runErr error
	awaiting := len(servers)
	select {
	case err := <-errCh:
		awaiting--
		if !errors.Is(err, http.ErrServerClosed) {
			runErr = err
		}
	case <-ctx.Done():
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	for _, s := range servers {
		logger.Info("shutting down", "listen", s.ln.Addr().String())
		if err := s.srv.Shutdown(shutdownCtx); err != nil && runErr == nil {
			runErr = fmt.Errorf("shutdown: %w", err)
		}
	}
	for i := 0; i < awaiting; i++ {
		if err := <-errCh; !errors.Is(err, http.ErrServerClosed) && runErr == nil {
			runErr = err
		}
	}
	return runErr
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
