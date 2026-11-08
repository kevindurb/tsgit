package web

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/kevindurb/tsgit/internal/authz"
	"github.com/kevindurb/tsgit/internal/identity"
	"github.com/kevindurb/tsgit/internal/repos"
)

const (
	deletePrefix    = "/.tsgit/delete/"
	deleteAction    = "delete"
	deleteTokenTTL  = time.Hour
	deleteBodyLimit = 64 << 10
)

// DeleteStore is the slice of *repos.Store the deletion handler needs.
type DeleteStore interface {
	Info(ctx context.Context, name string) (repos.Info, error)
	Delete(ctx context.Context, name string) error
}

// DeleteConfig wires the admin deletion handler.
type DeleteConfig struct {
	Resolver identity.Resolver
	Store    DeleteStore
	Log      *slog.Logger
	Version  string
	Secret   []byte
}

// DeleteHandler serves the admin-gated repository deletion surface. It is
// separate from Handler so the discovery pages stay read-only.
type DeleteHandler struct {
	resolver identity.Resolver
	store    DeleteStore
	log      *slog.Logger
	version  string
	secret   []byte

	deleteTmpl *template.Template
	errorTmpl  *template.Template
}

// NewDeleteHandler parses the templates and returns a handler. Secret signs
// the CSRF tokens and must be at least 32 bytes.
func NewDeleteHandler(cfg DeleteConfig) (*DeleteHandler, error) {
	if len(cfg.Secret) < 32 {
		return nil, errors.New("delete handler requires a 32-byte CSRF key")
	}
	log := cfg.Log
	if log == nil {
		log = slog.Default()
	}
	return &DeleteHandler{
		resolver:   cfg.Resolver,
		store:      cfg.Store,
		log:        log,
		version:    cfg.Version,
		secret:     append([]byte(nil), cfg.Secret...),
		deleteTmpl: parsePage(deleteHTML),
		errorTmpl:  parsePage(errorHTML),
	}, nil
}

// MatchesDelete reports whether r targets the reserved deletion prefix.
func MatchesDelete(r *http.Request) bool {
	return r.URL.Path == "/.tsgit/delete" || strings.HasPrefix(r.URL.Path, deletePrefix)
}

// Matches reports whether r belongs to the deletion surface.
func (h *DeleteHandler) Matches(r *http.Request) bool {
	return MatchesDelete(r)
}

func (h *DeleteHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	id, err := h.resolver.Resolve(r.Context(), r.RemoteAddr)
	if err != nil {
		h.log.Error("identity resolution failed", "remote", r.RemoteAddr, "error", err)
		h.renderError(w, r, http.StatusServiceUnavailable, "Identity unavailable",
			"The tailnet identity for this request could not be resolved.")
		return
	}

	name, ok := parseDeletePath(r.URL.Path)
	if !ok {
		h.renderError(w, r, http.StatusNotFound, "Not found", "No page exists at this address.")
		return
	}

	decision := authz.Decide(id, name, authz.OpDelete)
	if !decision.Allow {
		h.log.Info("deletion denied", "node", id.Node, "repo", name, "reason", decision.Reason)
		h.renderError(w, r, http.StatusNotFound, "Not found", "No page exists at this address.")
		return
	}

	switch r.Method {
	case http.MethodGet, http.MethodHead:
		h.serveConfirm(w, r, id, name)
	case http.MethodPost:
		h.serveDelete(w, r, id, name)
	default:
		w.Header().Set("Allow", "GET, HEAD, POST")
		h.renderError(w, r, http.StatusMethodNotAllowed, "Method not allowed",
			"The deletion page accepts GET and POST.")
	}
}

func parseDeletePath(path string) (string, bool) {
	rest := strings.TrimPrefix(path, deletePrefix)
	parts := strings.Split(rest, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", false
	}
	name, err := repos.ParseName(parts[0] + "/" + parts[1])
	if err != nil {
		return "", false
	}
	return name, true
}

func (h *DeleteHandler) serveConfirm(w http.ResponseWriter, r *http.Request, id authz.Identity, name string) {
	info, err := h.store.Info(r.Context(), name)
	if errors.Is(err, repos.ErrNotExist) {
		h.renderError(w, r, http.StatusNotFound, "Not found", "No page exists at this address.")
		return
	}
	if err != nil {
		h.log.Warn("repository metadata unavailable", "repo", name, "error", err)
	}
	h.renderConfirm(w, r, id, name, info, err != nil, http.StatusOK, "")
}

func (h *DeleteHandler) serveDelete(w http.ResponseWriter, r *http.Request, id authz.Identity, name string) {
	if !sameOrigin(r) {
		h.log.Warn("deletion refused", "repo", name, "node", id.Node, "reason", "cross-origin request")
		h.renderError(w, r, http.StatusForbidden, "Request refused",
			"The deletion request did not come from this site.")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, deleteBodyLimit)
	if err := r.ParseForm(); err != nil {
		h.renderError(w, r, http.StatusBadRequest, "Invalid request",
			"The deletion form could not be read.")
		return
	}
	if !verifyDeleteToken(h.secret, r.PostForm.Get("csrf"), id.Node, name, time.Now()) {
		h.log.Warn("deletion refused", "repo", name, "node", id.Node, "reason", "invalid or expired token")
		h.renderError(w, r, http.StatusForbidden, "Request refused",
			"The confirmation expired. Reload the page and try again.")
		return
	}
	if r.PostForm.Get("confirm") != name {
		info, err := h.store.Info(r.Context(), name)
		if errors.Is(err, repos.ErrNotExist) {
			h.renderError(w, r, http.StatusNotFound, "Not found", "No page exists at this address.")
			return
		}
		if err != nil {
			h.log.Warn("repository metadata unavailable", "repo", name, "error", err)
		}
		h.renderConfirm(w, r, id, name, info, err != nil, http.StatusBadRequest,
			"Type the exact repository name to confirm.")
		return
	}

	info, infoErr := h.store.Info(r.Context(), name)
	if infoErr != nil && !errors.Is(infoErr, repos.ErrNotExist) {
		h.log.Warn("repository metadata unavailable", "repo", name, "error", infoErr)
	}

	err := h.store.Delete(r.Context(), name)
	switch {
	case errors.Is(err, repos.ErrNotExist):
		h.renderError(w, r, http.StatusNotFound, "Not found", "No page exists at this address.")
		return
	case errors.Is(err, repos.ErrCleanup):
		h.log.Warn("repository deleted but cleanup failed", "repo", name, "error", err)
	case err != nil:
		h.log.Error("repository deletion failed", "repo", name, "error", err)
		h.renderError(w, r, http.StatusInternalServerError, "Deletion failed",
			"The repository could not be deleted.")
		return
	}

	h.log.Info("deleted repository",
		"repo", name,
		"node", id.Node,
		"login", id.Login,
		"tags", id.Tags,
		"latest", info.Latest.Hash,
		"remote", r.RemoteAddr,
	)
	http.Redirect(w, r, "/?deleted="+url.QueryEscape(name), http.StatusSeeOther)
}

func (h *DeleteHandler) renderConfirm(w http.ResponseWriter, r *http.Request, id authz.Identity, name string, info repos.Info, metadataErr bool, status int, message string) {
	if info.Name == "" {
		info.Name = name
	}
	view := newRepoView(r, info)
	view.MetadataErr = metadataErr
	h.renderPage(w, r, status, h.deleteTmpl, deletePage{
		pageData: pageData{
			Title:    "Delete " + view.Name,
			Version:  h.version,
			Identity: identityLabel(id),
			Admin:    true,
		},
		Repo:  view,
		Token: signDeleteToken(h.secret, id.Node, name, time.Now()),
		Error: message,
	})
}

func (h *DeleteHandler) renderError(w http.ResponseWriter, r *http.Request, status int, title, message string) {
	h.renderPage(w, r, status, h.errorTmpl, errorPage{
		pageData: pageData{Title: title, Version: h.version, Admin: true},
		Message:  message,
	})
}

func (h *DeleteHandler) renderPage(w http.ResponseWriter, r *http.Request, status int, tmpl *template.Template, data any) {
	renderPage(w, r, status, tmpl, data, h.log)
}

type deletePage struct {
	pageData
	Repo  repoView
	Token string
	Error string
}

// sameOrigin rejects an obviously cross-site POST. The signed token is the
// primary CSRF defense; these headers are depth for browsers that send them.
func sameOrigin(r *http.Request) bool {
	if site := r.Header.Get("Sec-Fetch-Site"); site != "" && site != "same-origin" {
		return false
	}
	if origin := r.Header.Get("Origin"); origin != "" {
		scheme := "http"
		if r.TLS != nil {
			scheme = "https"
		}
		if origin != scheme+"://"+r.Host {
			return false
		}
	}
	return true
}

func signDeleteToken(secret []byte, node, repo string, now time.Time) string {
	expiry := now.Add(deleteTokenTTL).Unix()
	mac := deleteTokenMAC(secret, node, repo, expiry)
	return strconv.FormatInt(expiry, 10) + "." + base64.RawURLEncoding.EncodeToString(mac)
}

func verifyDeleteToken(secret []byte, token, node, repo string, now time.Time) bool {
	expiryPart, macPart, ok := strings.Cut(token, ".")
	if !ok {
		return false
	}
	expiry, err := strconv.ParseInt(expiryPart, 10, 64)
	if err != nil || now.Unix() >= expiry {
		return false
	}
	got, err := base64.RawURLEncoding.DecodeString(macPart)
	if err != nil {
		return false
	}
	return hmac.Equal(got, deleteTokenMAC(secret, node, repo, expiry))
}

func deleteTokenMAC(secret []byte, node, repo string, expiry int64) []byte {
	mac := hmac.New(sha256.New, secret)
	_, _ = fmt.Fprintf(mac, "%s\x00%s\x00%s\x00%d", node, repo, deleteAction, expiry)
	return mac.Sum(nil)
}
