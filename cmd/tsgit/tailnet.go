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
	"tailscale.com/tailcfg/nodecap"
	"tailscale.com/tsnet"

	"github.com/kevindurb/tsgit/internal/config"
	"github.com/kevindurb/tsgit/internal/identity"
	"github.com/kevindurb/tsgit/internal/repos"
	"github.com/kevindurb/tsgit/internal/transport"
	"github.com/kevindurb/tsgit/internal/web"
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

// ListenFunnel opens a public listener that accepts Funnel flows only. The
// tailnet listener keeps its own port registration; tsnet dispatches by flow
// origin, so a tailnet peer can never arrive here.
func (t *tailnet) ListenFunnel(network, addr string) (net.Listener, error) {
	return t.server.ListenFunnel(network, addr, tsnet.FunnelOnly())
}

func (t *tailnet) CertDomains() []string {
	return t.server.CertDomains()
}

func (t *tailnet) WhoIs(ctx context.Context, remoteAddr string) (*apitype.WhoIsResponse, error) {
	return t.client.WhoIs(ctx, remoteAddr)
}

// StatusWithoutPeers reports the node's own capabilities for the public
// resolver. It is an in-process LocalAPI call and omits the peer list.
func (t *tailnet) StatusWithoutPeers(ctx context.Context) (*ipnstate.Status, error) {
	return t.client.StatusWithoutPeers(ctx)
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
	secret, err := csrfSecret()
	if err != nil {
		return err
	}

	domains := node.CertDomains()
	hasHTTPS := node.status.Self != nil && node.status.Self.HasCap(tailcfg.CapabilityHTTPS)
	if httpsEnabled(cfg.HTTPS, hasHTTPS, domains) {
		return serveTLS(ctx, cfg, store, node, domains, secret, logger)
	}
	if cfg.HTTPS {
		logger.Warn("HTTPS unavailable; serving plain HTTP", "cert_domains", len(domains), "capability", hasHTTPS)
	}
	if cfg.Funnel {
		logger.Error("Funnel requested but HTTPS is unavailable; not opening the public listener",
			"cert_domains", len(domains), "capability", hasHTTPS)
	}

	ln, err := node.Listen("tcp", ":"+strconv.Itoa(cfg.HTTPPort))
	if err != nil {
		return fmt.Errorf("listen on tailnet:%d: %w", cfg.HTTPPort, err)
	}
	handler, err := newTailnetHandler(store, node, secret, logger, false)
	if err != nil {
		return err
	}
	logger.Info("serving git smart HTTP over HTTP", "hostname", cfg.Hostname, "port", cfg.HTTPPort)
	srv := &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
	}
	return runHTTP(ctx, srv, ln, logger)
}

// newTailnetHandler builds the private front end: tailnet identity, write
// support, and optionally a badge marking repositories that are also public.
func newTailnetHandler(store *repos.Store, node *tailnet, secret []byte, logger *slog.Logger, markPublic bool) (http.Handler, error) {
	var publicResolver identity.Resolver
	if markPublic {
		publicResolver = identity.NewFunnel(node, logger)
	}
	resolver := identity.NewLocal(node, logger)
	del, err := web.NewDeleteHandler(web.DeleteConfig{
		Resolver: resolver,
		Store:    store,
		Log:      logger,
		Version:  version,
		Secret:   secret,
	})
	if err != nil {
		return nil, err
	}
	return composeTailnet(store, resolver, publicResolver,
		&transport.GitBin{Git: store.GitPath(), Log: logger}, del, logger), nil
}

// composeTailnet wires the tailnet resolver into both front ends. The public
// resolver, when present, is used only for the exposure badge. The delete
// handler is nil when the caller does not want the lifecycle surface.
func composeTailnet(store *repos.Store, resolver, publicResolver identity.Resolver, backend transport.Backend, del *web.DeleteHandler, logger *slog.Logger) http.Handler {
	gitHandler := transport.NewHandler(resolver, store, backend, logger)
	webConfig := web.Config{
		Resolver: resolver,
		Store:    store,
		Log:      logger,
		Version:  version,
	}
	webConfig.PublicResolver = publicResolver
	return front(gitHandler, web.New(webConfig), del)
}

// newPublicHandler builds the Funnel front end.
func newPublicHandler(cfg *config.Config, store *repos.Store, node *tailnet, logger *slog.Logger) http.Handler {
	backend := &transport.GitBin{Git: store.GitPath(), Log: logger}
	return composePublic(store, identity.NewFunnel(node, logger), backend, cfg.FunnelMaxConcurrent, logger)
}

// composePublic wires the public principal into both front ends. It shares the
// store and templates but refuses every write route before a backend starts.
func composePublic(store *repos.Store, resolver identity.Resolver, backend transport.Backend, maxConcurrent int, logger *slog.Logger) http.Handler {
	gitHandler := transport.NewHandler(resolver, store, backend, logger)
	gitHandler.ReadOnly = true
	ui := web.New(web.Config{
		Resolver: resolver,
		Store:    store,
		Log:      logger,
		Version:  version,
		Public:   true,
	})
	return publicSurface(front(gitHandler, ui, nil), maxConcurrent, logger)
}

func serveTLS(ctx context.Context, cfg *config.Config, store *repos.Store, node *tailnet, domains []string, secret []byte, logger *slog.Logger) error {
	host := domains[0]
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

	publicLn, publicOn := openFunnelListener(cfg, node.status.Self, domains, node.ListenFunnel, logger)

	tailnetHandler, err := newTailnetHandler(store, node, secret, logger, publicOn)
	if err != nil {
		return err
	}
	servers := []serverRun{{
		srv: &http.Server{
			Handler:           hstsHandler(tailnetHandler),
			ReadHeaderTimeout: 10 * time.Second,
		},
		ln: tlsLn,
	}}
	if publicOn {
		servers = append(servers, serverRun{
			srv: newPublicServer(newPublicHandler(cfg, store, node, logger)),
			ln:  publicLn,
		})
		logger.Info("serving public read-only git over Funnel", "host", host, "port", cfg.Port)
	}

	logger.Info("serving git smart HTTP over HTTPS", "hostname", host, "port", cfg.Port)
	serveErr := runServers(ctx, logger, servers...)

	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	_ = redirectSrv.Shutdown(shutdownCtx)
	return serveErr
}

// funnelBind is the listener seam, so the failure path is testable without a
// tailnet.
type funnelBind func(network, addr string) (net.Listener, error)

// openFunnelListener opens the public listener only when --funnel is set and
// the node has every prerequisite. A failure is logged and returns false; the
// tailnet keeps serving.
func openFunnelListener(cfg *config.Config, self *ipnstate.PeerStatus, domains []string, bind funnelBind, logger *slog.Logger) (net.Listener, bool) {
	if !cfg.Funnel {
		return nil, false
	}
	if reason := funnelUnavailableReason(self, domains); reason != "" {
		logger.Error("Funnel requested but unavailable; not opening the public listener", "reason", reason)
		return nil, false
	}
	ln, err := bind("tcp", funnelPort(cfg.Port))
	if err != nil {
		logger.Error("Funnel listener failed; serving the tailnet only", "port", cfg.Port, "error", err)
		return nil, false
	}
	return ln, true
}

func funnelUnavailableReason(self *ipnstate.PeerStatus, domains []string) string {
	if self == nil {
		return "node status is unavailable"
	}
	if !self.HasCap(nodecap.HTTPS) {
		return "node lacks the https capability"
	}
	if !self.HasCap(nodecap.Funnel) {
		return "node lacks the funnel attribute"
	}
	if len(domains) == 0 {
		return "no HTTPS certificate domain is available"
	}
	return ""
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
