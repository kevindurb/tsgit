package main

import (
	"context"
	"crypto/tls"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"tailscale.com/ipn"

	"github.com/kevindurb/tsgit/internal/authz"
	"github.com/kevindurb/tsgit/internal/transport"
	"github.com/kevindurb/tsgit/internal/web"
)

// Public listener limits. They are deliberately stricter than the tailnet
// server's: an anonymous pack exchange is a resource lever.
const (
	publicReadHeaderTimeout       = 10 * time.Second
	publicReadTimeout             = 60 * time.Second
	publicIdleTimeout             = 60 * time.Second
	publicMaxHeaderBytes          = 1 << 20
	publicMaxBodyBytes      int64 = 64 << 20
	publicRetryAfter              = "5"
)

type publicClientAddrKey struct{}

// newPublicServer builds the Funnel HTTP server. WriteTimeout is intentionally
// unset: a large clone is legitimate and bounded by the pack limiter instead.
func newPublicServer(handler http.Handler) *http.Server {
	return &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: publicReadHeaderTimeout,
		ReadTimeout:       publicReadTimeout,
		IdleTimeout:       publicIdleTimeout,
		MaxHeaderBytes:    publicMaxHeaderBytes,
		ConnContext:       publicConnContext,
	}
}

// publicConnContext records the real public client address for access logs.
// It is never consulted for authorization.
func publicConnContext(ctx context.Context, c net.Conn) context.Context {
	if addr := publicClientAddr(c); addr != "" {
		return context.WithValue(ctx, publicClientAddrKey{}, addr)
	}
	return ctx
}

// publicClientAddr unwraps TLS and returns FunnelConn.Src. The embedded
// connection's RemoteAddr is the Funnel relay, not the client.
func publicClientAddr(c net.Conn) string {
	if tlsConn, ok := c.(*tls.Conn); ok {
		c = tlsConn.NetConn()
	}
	if funnelConn, ok := c.(*ipn.FunnelConn); ok {
		return funnelConn.Src.String()
	}
	return ""
}

func publicClientAddrFromContext(ctx context.Context) string {
	addr, _ := ctx.Value(publicClientAddrKey{}).(string)
	return addr
}

// publicSurface bounds request bodies, caps concurrent public git operations,
// and logs the public principal with the client address from the connection
// context. Pack exchanges and git-backed browsing reads share one budget.
func publicSurface(next http.Handler, maxConcurrent int, logger *slog.Logger) http.Handler {
	slots := make(chan struct{}, maxConcurrent)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		logger.Info("public request",
			"principal", authz.FunnelNode,
			"client", publicClientAddrFromContext(r.Context()),
			"method", r.Method,
			"path", r.URL.Path,
		)
		if r.ContentLength > publicMaxBodyBytes {
			http.Error(w, "request body too large", http.StatusRequestEntityTooLarge)
			return
		}
		if r.Body != nil {
			r.Body = http.MaxBytesReader(w, r.Body, publicMaxBodyBytes)
		}
		if isPublicExpensive(r) {
			select {
			case slots <- struct{}{}:
				defer func() { <-slots }()
			default:
				w.Header().Set("Retry-After", publicRetryAfter)
				http.Error(w, "public operation limit reached", http.StatusServiceUnavailable)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

// isPublicExpensive reports whether r is a pack exchange or a git-backed
// browsing read, both of which cost a process and disk I/O.
func isPublicExpensive(r *http.Request) bool {
	return isPublicPackPOST(r) || web.IsBrowsingPath(r)
}

func isPublicPackPOST(r *http.Request) bool {
	if r.Method != http.MethodPost || !strings.HasSuffix(r.URL.Path, "/"+transport.ServiceUploadPack) {
		return false
	}
	return transport.Matches(r)
}

// funnelPort returns the Funnel port. Only 443, 8443, and 10000 are valid;
// the configured --port is used because the listeners share it.
func funnelPort(port int) string {
	return ":" + strconv.Itoa(port)
}
