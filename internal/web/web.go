// Package web serves the read-only repository discovery pages. It resolves the
// viewer from the tailnet, filters repositories through authz, and renders
// metadata from the repository store. It never mutates anything.
package web

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/kevindurb/tsgit/internal/authz"
	"github.com/kevindurb/tsgit/internal/identity"
	"github.com/kevindurb/tsgit/internal/repos"
)

// RepositoryStore is the read-only slice of *repos.Store the pages need.
type RepositoryStore interface {
	List(ctx context.Context) ([]repos.Info, error)
	Info(ctx context.Context, name string) (repos.Info, error)
}

// Config wires a discovery handler.
type Config struct {
	Resolver identity.Resolver
	Store    RepositoryStore
	Log      *slog.Logger
	Version  string

	// Public renders the public Funnel view: no push affordances and no
	// first-push instructions.
	Public bool

	// PublicResolver, when set, marks repositories that the public principal
	// may read in the tailnet index. A nil resolver marks none.
	PublicResolver identity.Resolver
}

// Handler serves the index, repository summaries, and embedded assets.
type Handler struct {
	resolver       identity.Resolver
	publicResolver identity.Resolver
	store          RepositoryStore
	log            *slog.Logger
	version        string
	public         bool

	indexTmpl *template.Template
	repoTmpl  *template.Template
	errorTmpl *template.Template
}

// New parses the embedded templates and returns a handler.
func New(cfg Config) *Handler {
	log := cfg.Log
	if log == nil {
		log = slog.Default()
	}
	return &Handler{
		resolver:       cfg.Resolver,
		publicResolver: cfg.PublicResolver,
		store:          cfg.Store,
		log:            log,
		version:        cfg.Version,
		public:         cfg.Public,
		indexTmpl:      parsePage(indexHTML),
		repoTmpl:       parsePage(repoHTML),
		errorTmpl:      parsePage(errorHTML),
	}
}

func parsePage(page []byte) *template.Template {
	t := template.Must(template.New("base").Parse(string(baseHTML)))
	template.Must(t.New("content").Parse(string(page)))
	return t
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/.tsgit/web.css":
		if !h.allowGet(w, r) {
			return
		}
		serveAsset(w, r, "web.css", "text/css; charset=utf-8", webCSS)
		return
	case "/.tsgit/favicon.svg":
		if !h.allowGet(w, r) {
			return
		}
		serveAsset(w, r, "favicon.svg", "image/svg+xml", faviconSVG)
		return
	case "/.tsgit", "/.tsgit/":
		if !h.allowGet(w, r) {
			return
		}
		http.Redirect(w, r, "/", http.StatusFound)
		return
	}

	id, err := h.resolver.Resolve(r.Context(), r.RemoteAddr)
	if err != nil {
		h.log.Error("identity resolution failed", "remote", r.RemoteAddr, "error", err)
		h.renderError(w, r, http.StatusServiceUnavailable, "Identity unavailable",
			"The tailnet identity for this request could not be resolved.")
		return
	}

	if r.URL.Path == "/" {
		if !h.allowGet(w, r) {
			return
		}
		h.serveIndex(w, r, id)
		return
	}
	if !h.allowGet(w, r) {
		return
	}
	h.servePath(w, r, id)
}

func (h *Handler) allowGet(w http.ResponseWriter, r *http.Request) bool {
	if r.Method == http.MethodGet || r.Method == http.MethodHead {
		return true
	}
	w.Header().Set("Allow", "GET, HEAD")
	h.renderError(w, r, http.StatusMethodNotAllowed, "Method not allowed",
		"The discovery pages are read-only.")
	return false
}

func (h *Handler) serveIndex(w http.ResponseWriter, r *http.Request, id authz.Identity) {
	infos, err := h.store.List(r.Context())
	if err != nil {
		h.log.Error("list repositories", "error", err)
		h.renderError(w, r, http.StatusInternalServerError, "Repositories unavailable",
			"The repository list could not be read.")
		return
	}

	views := make([]repoView, 0, len(infos))
	publicID, hasPublic := h.publicView(r)
	for _, info := range infos {
		decision := authz.Decide(id, info.Name, authz.OpRead)
		if !decision.Allow {
			continue
		}
		if info.Err != nil {
			h.log.Warn("repository metadata unavailable", "repo", info.Name, "error", info.Err)
		}
		view := h.view(r, info, decision.CanCreate, decision.CanDelete)
		if hasPublic {
			view.Public = authz.Decide(publicID, info.Name, authz.OpRead).Allow
		}
		views = append(views, view)
	}
	sortViews(views)

	var notice string
	if deleted := r.URL.Query().Get("deleted"); deleted != "" {
		if name, err := repos.ParseName(deleted); err == nil {
			notice = "Deleted " + name
		}
	}
	h.render(w, r, http.StatusOK, h.indexTmpl, indexPage{
		pageData: pageData{
			Title:    "Repositories",
			Version:  h.version,
			Identity: identityLabel(id),
			Public:   h.public,
		},
		Repos:  views,
		Notice: notice,
	})
}

// publicView resolves the public principal for the exposure badge. A failure
// degrades to no badges; the tailnet page must keep rendering.
func (h *Handler) publicView(r *http.Request) (authz.Identity, bool) {
	if h.publicResolver == nil {
		return authz.Identity{}, false
	}
	id, err := h.publicResolver.Resolve(r.Context(), "")
	if err != nil {
		h.log.Warn("public visibility unavailable", "error", err)
		return authz.Identity{}, false
	}
	return id, true
}

func (h *Handler) servePath(w http.ResponseWriter, r *http.Request, id authz.Identity) {
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/"), "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		h.renderError(w, r, http.StatusNotFound, "Not found", "No page exists at this address.")
		return
	}

	name, err := repos.ParseName(parts[0] + "/" + parts[1])
	if err != nil {
		h.renderError(w, r, http.StatusNotFound, "Not found", "No page exists at this address.")
		return
	}
	if strings.HasSuffix(parts[1], ".git") {
		http.Redirect(w, r, repoPath(name), http.StatusMovedPermanently)
		return
	}

	decision := authz.Decide(id, name, authz.OpRead)
	if !decision.Allow {
		h.log.Info("access denied", "node", id.Node, "repo", name, "reason", decision.Reason)
		h.renderError(w, r, http.StatusNotFound, "Not found", "No page exists at this address.")
		return
	}

	info, err := h.store.Info(r.Context(), name)
	if errors.Is(err, repos.ErrNotExist) {
		h.renderError(w, r, http.StatusNotFound, "Not found", "No page exists at this address.")
		return
	}
	metadataErr := info.Err != nil
	if err != nil {
		metadataErr = true
		h.log.Warn("repository metadata unavailable", "repo", name, "error", err)
	}
	if info.Name == "" {
		info.Name = name
	}
	if info.Err != nil {
		h.log.Warn("repository metadata unavailable", "repo", info.Name, "error", info.Err)
	}

	view := h.view(r, info, decision.CanCreate, decision.CanDelete)
	view.MetadataErr = metadataErr
	h.render(w, r, http.StatusOK, h.repoTmpl, repoPage{
		pageData: pageData{
			Title:    view.Name,
			Version:  h.version,
			Identity: identityLabel(id),
			Public:   h.public,
		},
		Repo: view,
	})
}

func (h *Handler) renderError(w http.ResponseWriter, r *http.Request, status int, title, message string) {
	h.render(w, r, status, h.errorTmpl, errorPage{
		pageData: pageData{Title: title, Version: h.version},
		Message:  message,
	})
}

func (h *Handler) render(w http.ResponseWriter, r *http.Request, status int, tmpl *template.Template, data any) {
	renderPage(w, r, status, tmpl, data, h.log)
}

func renderPage(w http.ResponseWriter, r *http.Request, status int, tmpl *template.Template, data any, log *slog.Logger) {
	securityHeaders(w)
	var buf bytes.Buffer
	if err := tmpl.ExecuteTemplate(&buf, "base", data); err != nil {
		log.Error("render page", "path", r.URL.Path, "error", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	if r.Method != http.MethodHead {
		_, _ = w.Write(buf.Bytes())
	}
}

func securityHeaders(w http.ResponseWriter) {
	h := w.Header()
	h.Set("Content-Security-Policy",
		"default-src 'none'; style-src 'self'; img-src 'self'; base-uri 'none'; form-action 'self'; frame-ancestors 'none'")
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Referrer-Policy", "no-referrer")
	h.Set("Cache-Control", "no-store")
}

type pageData struct {
	Title    string
	Version  string
	Identity string
	Public   bool
	Admin    bool
}

type indexPage struct {
	pageData
	Repos  []repoView
	Notice string
}

type repoPage struct {
	pageData
	Repo repoView
}

type errorPage struct {
	pageData
	Message string
}

type repoView struct {
	Name        string
	Path        string
	CloneURL    string
	Description string
	Branch      string
	Latest      repos.Commit
	Updated     string
	Branches    int
	Tags        int
	Writable    bool
	Deletable   bool
	Public      bool
	Empty       bool
	Refs        []string
	MetadataErr bool
}

func (h *Handler) view(r *http.Request, info repos.Info, writable, deletable bool) repoView {
	view := newRepoView(r, info)
	view.Writable = writable && !h.public
	view.Deletable = deletable && !h.public
	return view
}

func newRepoView(r *http.Request, info repos.Info) repoView {
	refs := make([]string, len(info.Refs))
	for i, ref := range info.Refs {
		refs[i] = strings.ToValidUTF8(ref, "\uFFFD")
	}
	return repoView{
		Name:        strings.ToValidUTF8(info.Name, "\uFFFD"),
		Path:        repoPath(info.Name),
		CloneURL:    cloneURL(r, info.Name),
		Description: strings.ToValidUTF8(info.Description, "\uFFFD"),
		Branch:      strings.ToValidUTF8(info.Branch, "\uFFFD"),
		Latest: repos.Commit{
			Hash:    info.Latest.Hash,
			Subject: strings.ToValidUTF8(info.Latest.Subject, "\uFFFD"),
			When:    info.Latest.When,
		},
		Updated:     relativeTime(info.Latest.When),
		Branches:    info.Branches,
		Tags:        info.Tags,
		Empty:       info.Empty,
		Refs:        refs,
		MetadataErr: info.Err != nil,
	}
}

func sortViews(views []repoView) {
	sort.SliceStable(views, func(i, j int) bool {
		a, b := views[i], views[j]
		if a.Empty != b.Empty {
			return !a.Empty
		}
		if !a.Latest.When.Equal(b.Latest.When) {
			return a.Latest.When.After(b.Latest.When)
		}
		return a.Name < b.Name
	})
}

func identityLabel(id authz.Identity) string {
	switch {
	case id.Login != "":
		return id.Login
	case len(id.Tags) > 0:
		return strings.Join(id.Tags, ", ")
	case id.Node != "":
		return id.Node
	default:
		return "unknown"
	}
}

func repoPath(name string) string {
	namespace, repo, _ := strings.Cut(name, "/")
	return "/" + url.PathEscape(namespace) + "/" + url.PathEscape(repo)
}

func cloneURL(r *http.Request, name string) string {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	namespace, repo, _ := strings.Cut(name, "/")
	return (&url.URL{
		Scheme: scheme,
		Host:   r.Host,
		Path:   "/" + namespace + "/" + repo + ".git",
	}).String()
}

func relativeTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	age := time.Since(t)
	switch {
	case age < time.Minute:
		return "just now"
	case age < time.Hour:
		return fmt.Sprintf("%dm ago", int(age.Minutes()))
	case age < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(age.Hours()))
	case age < 30*24*time.Hour:
		return fmt.Sprintf("%dd ago", int(age.Hours()/24))
	default:
		return t.Format("Jan 2, 2006")
	}
}
