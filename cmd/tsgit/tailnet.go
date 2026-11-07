package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"tailscale.com/client/local"
	"tailscale.com/client/tailscale/apitype"
	"tailscale.com/envknob"
	"tailscale.com/hostinfo"
	"tailscale.com/ipn/ipnstate"
	"tailscale.com/tailcfg"
	"tailscale.com/tsnet"

	"github.com/kevindurb/tsgit/internal/config"
	"github.com/kevindurb/tsgit/internal/identity"
	"github.com/kevindurb/tsgit/internal/repos"
	"github.com/kevindurb/tsgit/internal/transport"
)

// startTailnet is a seam so development mode can be tested without joining a
// tailnet.
var startTailnet = newTailnet

type tailnet struct {
	server *tsnet.Server
	client *local.Client
	status *ipnstate.Status
}

func newTailnet(ctx context.Context, cfg *config.Config, logger *slog.Logger) (*tailnet, error) {
	hostinfo.SetApp("tsgit")

	server := &tsnet.Server{
		Hostname:      cfg.Hostname,
		Dir:           cfg.StateDir,
		ControlURL:    cfg.ControlURL,
		AdvertiseTags: cfg.AdvertiseTagList(),
		RunWebClient:  false,
		UserLogf: func(format string, args ...any) {
			logger.Info(fmt.Sprintf(format, args...))
		},
		Logf: func(format string, args ...any) {
			if cfg.DebugTsnet {
				logger.Debug(fmt.Sprintf(format, args...))
			}
		},
	}

	if cfg.AuthKeyFile == "" && envknob.String("TS_AUTHKEY") != "" {
		logger.Info("using TS_AUTHKEY from the environment")
	}

	if cfg.AuthKeyFile != "" {
		key, err := os.ReadFile(cfg.AuthKeyFile)
		switch {
		case err == nil:
			server.AuthKey = strings.TrimSpace(string(key))
		case isRegistered(cfg.StateDir):
			logger.Warn("auth key file unreadable; using existing node state", "path", cfg.AuthKeyFile, "error", err)
		default:
			return nil, fmt.Errorf("read auth key file %q: %w", cfg.AuthKeyFile, err)
		}
	}

	status, err := server.Up(ctx)
	if err != nil {
		return nil, fmt.Errorf("join tailnet as %q: %w", cfg.Hostname, err)
	}
	client, err := server.LocalClient()
	if err != nil {
		_ = server.Close()
		return nil, fmt.Errorf("open LocalAPI client: %w", err)
	}
	return &tailnet{server: server, client: client, status: status}, nil
}

func isRegistered(stateDir string) bool {
	_, err := os.Stat(filepath.Join(stateDir, "tailscaled.state"))
	return err == nil
}

func (t *tailnet) Listen(network, addr string) (net.Listener, error) {
	return t.server.Listen(network, addr)
}

func (t *tailnet) ListenTLS(network, addr string) (net.Listener, error) {
	return t.server.ListenTLS(network, addr)
}

func (t *tailnet) CertDomains() []string {
	return t.server.CertDomains()
}

func (t *tailnet) WhoIs(ctx context.Context, remoteAddr string) (*apitype.WhoIsResponse, error) {
	return t.client.WhoIs(ctx, remoteAddr)
}

func (t *tailnet) Close() error {
	return t.server.Close()
}

func serveTailnet(ctx context.Context, cfg *config.Config, logger *slog.Logger) error {
	node, err := startTailnet(ctx, cfg, logger)
	if err != nil {
		return err
	}
	defer node.Close()

	store, err := repos.Open(cfg.RepoRoot, cfg.DefaultBranch)
	if err != nil {
		return err
	}

	handler := transport.NewHandler(
		identity.NewLocal(node, logger),
		store,
		&transport.GitBin{Git: store.GitPath(), Log: logger},
		logger,
	)

	domains := node.CertDomains()
	hasHTTPS := node.status.Self != nil && node.status.Self.HasCap(tailcfg.CapabilityHTTPS)
	if httpsEnabled(cfg.HTTPS, hasHTTPS, domains) {
		return serveTLS(ctx, cfg, handler, node, domains[0], logger)
	}
	if cfg.HTTPS {
		logger.Warn("HTTPS unavailable; serving plain HTTP", "cert_domains", len(domains), "capability", hasHTTPS)
	}

	ln, err := node.Listen("tcp", ":"+strconv.Itoa(cfg.HTTPPort))
	if err != nil {
		return fmt.Errorf("listen on tailnet:%d: %w", cfg.HTTPPort, err)
	}
	logger.Info("serving git smart HTTP over HTTP", "hostname", cfg.Hostname, "port", cfg.HTTPPort)
	srv := &http.Server{Handler: handler, ReadHeaderTimeout: 10 * time.Second}
	return runHTTP(ctx, srv, ln, logger)
}

func serveTLS(ctx context.Context, cfg *config.Config, handler http.Handler, node *tailnet, host string, logger *slog.Logger) error {
	tlsLn, err := node.ListenTLS("tcp", ":"+strconv.Itoa(cfg.Port))
	if err != nil {
		return fmt.Errorf("listen TLS on tailnet:%d: %w", cfg.Port, err)
	}
	redirectLn, err := node.Listen("tcp", ":"+strconv.Itoa(cfg.HTTPPort))
	if err != nil {
		return fmt.Errorf("listen HTTP redirect on tailnet:%d: %w", cfg.HTTPPort, err)
	}

	redirectSrv := &http.Server{
		Handler:           redirectHandler(host, cfg.Port),
		ReadHeaderTimeout: 10 * time.Second,
	}
	go func() {
		if err := redirectSrv.Serve(redirectLn); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("HTTP redirect listener failed", "error", err)
		}
	}()

	logger.Info("serving git smart HTTP over HTTPS", "hostname", host, "port", cfg.Port)
	srv := &http.Server{
		Handler:           hstsHandler(handler),
		ReadHeaderTimeout: 10 * time.Second,
	}
	serveErr := runHTTP(ctx, srv, tlsLn, logger)

	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	_ = redirectSrv.Shutdown(shutdownCtx)
	return serveErr
}

func httpsEnabled(explicit, hasCap bool, certDomains []string) bool {
	return explicit && hasCap && len(certDomains) > 0
}

func redirectHandler(host string, port int) http.Handler {
	authority := host
	if port != 443 {
		authority = net.JoinHostPort(host, strconv.Itoa(port))
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "https://"+authority+r.URL.RequestURI(), http.StatusPermanentRedirect)
	})
}

func hstsHandler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Strict-Transport-Security", "max-age=31536000")
		next.ServeHTTP(w, r)
	})
}
