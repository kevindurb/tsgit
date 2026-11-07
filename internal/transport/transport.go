// Package transport serves the git smart-HTTP endpoints. It owns routing,
// response framing, and the boundary to a replaceable pack backend; it does not
// implement the pack protocol itself.
package transport

import (
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/kevindurb/tsgit/internal/authz"
	"github.com/kevindurb/tsgit/internal/identity"
	"github.com/kevindurb/tsgit/internal/repos"
)

// Smart-HTTP service names.
const (
	ServiceUploadPack  = "git-upload-pack"
	ServiceReceivePack = "git-receive-pack"
)

// Request is one pack exchange handed to a Backend.
type Request struct {
	RepoPath    string
	Service     string
	GitProtocol string
	Advertise   bool
	Stateless   bool
	Body        io.Reader
	Out         io.Writer
}

// Backend performs a pack exchange. The first implementation shells out to git;
// a later one can be pure Go without changing the handler.
type Backend interface {
	Serve(ctx context.Context, req Request) error
}

// Handler is the smart-HTTP entry point.
type Handler struct {
	Resolver identity.Resolver
	Store    *repos.Store
	Backend  Backend
	Log      *slog.Logger
}

// NewHandler wires a handler.
func NewHandler(resolver identity.Resolver, store *repos.Store, backend Backend, log *slog.Logger) *Handler {
	if log == nil {
		log = slog.Default()
	}
	return &Handler{Resolver: resolver, Store: store, Backend: backend, Log: log}
}

type route struct {
	name      string
	service   string
	advertise bool
	op        authz.Op
	create    bool
}

var errUnsupported = fmt.Errorf("unsupported smart-HTTP request")

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	id, err := h.Resolver.Resolve(r.Context(), r.RemoteAddr)
	if err != nil {
		h.Log.Error("identity resolution failed", "remote", r.RemoteAddr, "error", err)
		http.Error(w, "identity unavailable", http.StatusServiceUnavailable)
		return
	}

	rt, err := parseRoute(r)
	if err != nil {
		h.notFound(w)
		return
	}

	decision := authz.Decide(id, rt.name, rt.op)
	if !decision.Allow {
		h.Log.Info("access denied",
			"node", id.Node,
			"repo", rt.name,
			"op", rt.op,
			"reason", decision.Reason,
		)
		h.notFound(w)
		return
	}

	repoPath, err := h.Store.Resolve(rt.name)
	if err != nil {
		h.Log.Warn("repository resolution refused", "repo", rt.name, "error", err)
		h.notFound(w)
		return
	}
	if !h.Store.Exists(rt.name) {
		if !rt.create || !decision.CanCreate {
			h.notFound(w)
			return
		}
		repoPath, err = h.Store.Create(r.Context(), rt.name)
		if err != nil {
			h.Log.Error("repository creation failed", "repo", rt.name, "error", err)
			http.Error(w, "repository creation failed", http.StatusInternalServerError)
			return
		}
		h.Log.Info("created repository", "repo", rt.name, "node", id.Node)
	}

	h.serveBackend(w, r, rt, repoPath)
}

func (h *Handler) notFound(w http.ResponseWriter) {
	http.Error(w, "not found", http.StatusNotFound)
}

func (h *Handler) serveBackend(w http.ResponseWriter, r *http.Request, rt route, repoPath string) {
	header := w.Header()
	header.Set("Cache-Control", "no-cache, max-age=0, must-revalidate")
	header.Set("Pragma", "no-cache")
	header.Set("Expires", "Fri, 01 Jan 1980 00:00:00 GMT")
	header.Set("X-Content-Type-Options", "nosniff")

	var prefix []byte
	if rt.advertise {
		header.Set("Content-Type", "application/x-"+rt.service+"-advertisement")
		if !(rt.service == ServiceUploadPack && protocolVersion(r.Header.Get("Git-Protocol")) == "2") {
			prefix = servicePrefix(rt.service)
		}
	} else {
		header.Set("Content-Type", "application/x-"+rt.service+"-result")
	}

	body := io.Reader(r.Body)
	if strings.EqualFold(r.Header.Get("Content-Encoding"), "gzip") {
		gz, err := gzip.NewReader(r.Body)
		if err != nil {
			http.Error(w, "invalid gzip request body", http.StatusBadRequest)
			return
		}
		defer gz.Close()
		body = gz
	}

	sw := &streamWriter{w: w, rc: http.NewResponseController(w), prefix: prefix}
	req := Request{
		RepoPath:    repoPath,
		Service:     rt.service,
		GitProtocol: r.Header.Get("Git-Protocol"),
		Advertise:   rt.advertise,
		Stateless:   true,
		Body:        body,
		Out:         sw,
	}
	if err := h.Backend.Serve(r.Context(), req); err != nil {
		if !sw.wrote {
			h.Log.Error("backend failed before response", "repo", rt.name, "service", rt.service, "error", err)
			http.Error(w, "backend failure", http.StatusInternalServerError)
			return
		}
		h.Log.Error("backend failed mid-response", "repo", rt.name, "service", rt.service, "error", err)
		return
	}
	sw.commit()
}

func parseRoute(r *http.Request) (route, error) {
	path := r.URL.Path
	switch {
	case strings.HasSuffix(path, "/info/refs"):
		if r.Method != http.MethodGet {
			return route{}, errUnsupported
		}
		rest := strings.TrimSuffix(path, "/info/refs")
		switch service := r.URL.Query().Get("service"); service {
		case ServiceUploadPack:
			return makeRoute(rest, service, true, authz.OpRead, false)
		case ServiceReceivePack:
			return makeRoute(rest, service, true, authz.OpWrite, true)
		default:
			return route{}, errUnsupported
		}
	case strings.HasSuffix(path, "/"+ServiceUploadPack):
		if r.Method != http.MethodPost {
			return route{}, errUnsupported
		}
		return makeRoute(strings.TrimSuffix(path, "/"+ServiceUploadPack), ServiceUploadPack, false, authz.OpRead, false)
	case strings.HasSuffix(path, "/"+ServiceReceivePack):
		if r.Method != http.MethodPost {
			return route{}, errUnsupported
		}
		return makeRoute(strings.TrimSuffix(path, "/"+ServiceReceivePack), ServiceReceivePack, false, authz.OpWrite, true)
	default:
		return route{}, errUnsupported
	}
}

func makeRoute(repoPath, service string, advertise bool, op authz.Op, create bool) (route, error) {
	if !strings.HasPrefix(repoPath, "/") {
		return route{}, errUnsupported
	}
	name, err := repos.ParseName(strings.TrimPrefix(repoPath, "/"))
	if err != nil {
		return route{}, errUnsupported
	}
	return route{name: name, service: service, advertise: advertise, op: op, create: create}, nil
}

func protocolVersion(header string) string {
	for _, field := range strings.Split(header, ":") {
		key, value, ok := strings.Cut(strings.TrimSpace(field), "=")
		if ok && key == "version" {
			return value
		}
	}
	return ""
}

func servicePrefix(service string) []byte {
	line := "# service=" + service + "\n"
	prefix := append([]byte(fmt.Sprintf("%04x", len(line)+4)), line...)
	return append(prefix, "0000"...)
}

// streamWriter delays the response status and any advertisement prefix until
// the backend produces its first byte, so a backend that fails early can still
// be reported as a server error.
type streamWriter struct {
	w      http.ResponseWriter
	rc     *http.ResponseController
	prefix []byte
	wrote  bool
}

func (s *streamWriter) Write(p []byte) (int, error) {
	if !s.wrote {
		s.wrote = true
		if len(s.prefix) > 0 {
			if _, err := s.w.Write(s.prefix); err != nil {
				return 0, err
			}
		}
	}
	n, err := s.w.Write(p)
	if flushErr := s.rc.Flush(); err == nil && flushErr != nil {
		err = flushErr
	}
	return n, err
}

func (s *streamWriter) commit() {
	if s.wrote {
		return
	}
	s.wrote = true
	s.w.WriteHeader(http.StatusOK)
	_ = s.rc.Flush()
}
