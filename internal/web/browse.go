package web

import (
	"context"
	"errors"
	"fmt"
	"html/template"
	"net/http"
	"net/url"
	"strings"

	"github.com/kevindurb/tsgit/internal/authz"
	"github.com/kevindurb/tsgit/internal/repos"
)

type browsePage struct {
	pageData
	Name         string
	ShortHash    string
	Commit       repos.Commit
	Crumbs       []crumb
	Parent       string
	Entries      []entryView
	Refs         []refOption
	FormAction   string
	Blob         blobView
	Readme       template.HTML
	ReadmeNotice string
}

type crumb struct {
	Name string
	Href string
}

type entryView struct {
	Name string
	Href string
	Kind string
	Size string
	Mode string
}

type refOption struct {
	Value    string
	Label    string
	Selected bool
}

type blobView struct {
	Name       string
	Path       string
	Href       string
	Kind       string
	Mode       string
	Size       string
	Hash       string
	Text       string
	Binary     bool
	TooLarge   bool
	Empty      bool
	Symlink    bool
	LinkTarget string
	Submodule  bool
}

// serveRepositoryPath routes a canonical repository path to the summary, tree,
// or blob page. Parsing and authorization happen before any store read.
func (h *Handler) serveRepositoryPath(w http.ResponseWriter, r *http.Request, id authz.Identity) {
	parts := splitPath(r.URL.Path)
	if len(parts) < 2 || parts[0] == "" || parts[1] == "" {
		h.notFound(w, r)
		return
	}
	name, err := repos.ParseName(parts[0] + "/" + parts[1])
	if err != nil {
		h.notFound(w, r)
		return
	}

	if strings.HasSuffix(parts[1], ".git") {
		target, ok := browseGitRedirect(name, parts, r.URL.RawQuery)
		if !ok {
			h.notFound(w, r)
			return
		}
		http.Redirect(w, r, target, http.StatusMovedPermanently)
		return
	}

	decision := authz.Decide(id, name, authz.OpRead)
	if !decision.Allow {
		h.log.Info("access denied", "node", id.Node, "repo", name, "reason", decision.Reason)
		h.notFound(w, r)
		return
	}

	switch {
	case len(parts) == 2:
		if r.URL.Query().Has("ref") {
			target := repoPath(name) + "/tree"
			if r.URL.RawQuery != "" {
				target += "?" + r.URL.RawQuery
			}
			http.Redirect(w, r, target, http.StatusFound)
			return
		}
		h.serveSummary(w, r, id, name, decision)
	case parts[2] == "tree":
		path, err := repos.ParseTreePath(parts[3:])
		if err != nil {
			h.notFound(w, r)
			return
		}
		h.serveTree(w, r, id, name, path, decision)
	case parts[2] == "blob":
		path, err := repos.ParseTreePath(parts[3:])
		if err != nil || path == "" {
			h.notFound(w, r)
			return
		}
		h.serveBlob(w, r, id, name, path, decision)
	default:
		h.notFound(w, r)
	}
}

func (h *Handler) serveSummary(w http.ResponseWriter, r *http.Request, id authz.Identity, name string, decision authz.Decision) {
	info, err := h.store.Info(r.Context(), name)
	if errors.Is(err, repos.ErrNotExist) {
		h.notFound(w, r)
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
	readme, notice := h.summaryReadme(r, name, info)

	h.render(w, r, http.StatusOK, h.repoTmpl, repoPage{
		pageData: pageData{
			Title:    view.Name,
			Version:  h.version,
			Identity: identityLabel(id),
			Public:   h.public,
		},
		Repo:         view,
		Readme:       readme,
		ReadmeNotice: notice,
	})
}

// summaryReadme reads the README at HEAD. Info.Latest is deliberately not used:
// it is the newest commit across all refs, not the HEAD tip.
func (h *Handler) summaryReadme(r *http.Request, name string, info repos.Info) (template.HTML, string) {
	if info.Empty {
		return "", ""
	}
	head, err := h.store.ResolveRef(r.Context(), name, "")
	if err != nil {
		if !errors.Is(err, repos.ErrNotExist) {
			h.log.Warn("read HEAD for README", "repo", name, "error", err)
			return "", "The README is unavailable."
		}
		return "", ""
	}
	blob, found, err := h.store.Readme(r.Context(), name, head.Hash)
	if err != nil {
		h.log.Warn("read README", "repo", name, "error", err)
		return "", "The README is unavailable."
	}
	return h.renderReadme(name, blob, found, "")
}

func (h *Handler) serveTree(w http.ResponseWriter, r *http.Request, id authz.Identity, name, path string, decision authz.Decision) {
	info, infoErr := h.store.Info(r.Context(), name)
	if errors.Is(infoErr, repos.ErrNotExist) {
		h.notFound(w, r)
		return
	}
	if infoErr != nil {
		h.log.Warn("repository metadata unavailable", "repo", name, "error", infoErr)
	}

	commit, err := h.store.ResolveRef(r.Context(), name, r.URL.Query().Get("ref"))
	if err != nil {
		h.browseError(w, r, name, err)
		return
	}
	entries, err := h.store.Tree(r.Context(), name, commit.Hash, path)
	if errors.Is(err, repos.ErrNotTree) {
		redirect := browseURL(name, "blob", path, selectedLinkRef(commit, info.Branch))
		http.Redirect(w, r, redirect, http.StatusFound)
		return
	}
	if err != nil {
		h.browseError(w, r, name, err)
		return
	}

	linkRef := selectedLinkRef(commit, info.Branch)
	page := browsePage{
		pageData: pageData{
			Title:    name,
			Version:  h.version,
			Identity: identityLabel(id),
			Public:   h.public,
		},
		Name:       name,
		ShortHash:  shortHash(commit.Hash),
		Commit:     commit,
		Crumbs:     browseCrumbs(name, "tree", path, linkRef),
		Parent:     parentTreeURL(name, path, linkRef),
		Entries:    treeEntries(name, path, linkRef, entries),
		Refs:       refOptions(info.Branch, info.Refs, linkRef),
		FormAction: browseURL(name, "tree", path, ""),
	}

	if path == "" {
		blob, found, err := h.store.Readme(r.Context(), name, commit.Hash)
		if err != nil {
			h.log.Warn("read README", "repo", name, "error", err)
			page.ReadmeNotice = "The README is unavailable."
		} else {
			page.Readme, page.ReadmeNotice = h.renderReadme(name, blob, found, linkRef)
		}
	}

	h.render(w, r, http.StatusOK, h.treeTmpl, page)
}

func (h *Handler) serveBlob(w http.ResponseWriter, r *http.Request, id authz.Identity, name, path string, decision authz.Decision) {
	info, infoErr := h.store.Info(r.Context(), name)
	if errors.Is(infoErr, repos.ErrNotExist) {
		h.notFound(w, r)
		return
	}
	if infoErr != nil {
		h.log.Warn("repository metadata unavailable", "repo", name, "error", infoErr)
	}

	commit, err := h.store.ResolveRef(r.Context(), name, r.URL.Query().Get("ref"))
	if err != nil {
		h.browseError(w, r, name, err)
		return
	}
	linkRef := selectedLinkRef(commit, info.Branch)
	blob, err := h.store.Blob(r.Context(), name, commit.Hash, path)
	if errors.Is(err, repos.ErrNotBlob) {
		http.Redirect(w, r, browseURL(name, "tree", path, linkRef), http.StatusFound)
		return
	}
	if err != nil {
		h.browseError(w, r, name, err)
		return
	}

	view := blobView{
		Name:       pathBase(path),
		Path:       path,
		Href:       browseURL(name, "blob", path, linkRef),
		Mode:       blob.Mode,
		Hash:       blob.Hash,
		Binary:     blob.Binary,
		TooLarge:   blob.TooLarge,
		Empty:      blob.Empty,
		Symlink:    blob.Symlink,
		LinkTarget: blob.LinkTarget,
		Submodule:  blob.Submodule,
	}
	view.Kind, view.Size, _ = describeEntry(blob.Mode, blob.Size, blob.Submodule, blob.Symlink, false)
	view.Text = string(blob.Data)

	page := browsePage{
		pageData: pageData{
			Title:    name + " / " + path,
			Version:  h.version,
			Identity: identityLabel(id),
			Public:   h.public,
		},
		Name:       name,
		ShortHash:  shortHash(commit.Hash),
		Commit:     commit,
		Crumbs:     browseCrumbs(name, "blob", path, linkRef),
		Parent:     parentTreeURL(name, path, linkRef),
		Blob:       view,
		Refs:       refOptions(info.Branch, info.Refs, linkRef),
		FormAction: browseURL(name, "blob", path, ""),
	}
	h.render(w, r, http.StatusOK, h.blobTmpl, page)
}

// renderReadme applies the safe Markdown pipeline and explains why an
// unrenderable README was skipped.
func (h *Handler) renderReadme(name string, blob repos.Blob, found bool, linkRef string) (template.HTML, string) {
	if !found {
		return "", ""
	}
	switch {
	case blob.TooLarge:
		return "", "The README is too large to render."
	case blob.Submodule:
		return "", "The README is a submodule and is not rendered."
	case blob.Symlink:
		return "", "The README is a symbolic link and is not rendered."
	case blob.Binary:
		return "", "The README is binary and is not rendered."
	}
	rendered, err := renderMarkdown(blob.Data, markdownOptions{Repo: name, Ref: linkRef})
	if err != nil {
		h.log.Warn("render README", "repo", name, "error", err)
		return "", "The README could not be rendered."
	}
	return rendered, ""
}

func (h *Handler) browseError(w http.ResponseWriter, r *http.Request, name string, err error) {
	switch {
	case errors.Is(err, repos.ErrNotExist), errors.Is(err, repos.ErrInvalidPath):
		h.notFound(w, r)
	case errors.Is(err, context.DeadlineExceeded), errors.Is(err, context.Canceled):
		h.log.Warn("browsing read unavailable", "repo", name, "error", err)
		w.Header().Set("Retry-After", "5")
		h.renderError(w, r, http.StatusServiceUnavailable, "Repository unavailable",
			"The repository could not be read in time.")
	default:
		h.log.Error("browsing read failed", "repo", name, "error", err)
		h.renderError(w, r, http.StatusInternalServerError, "Repository unavailable",
			"The repository could not be read.")
	}
}

func (h *Handler) notFound(w http.ResponseWriter, r *http.Request) {
	h.renderError(w, r, http.StatusNotFound, "Not found", "No page exists at this address.")
}

// IsBrowsingPath reports whether r is a git-backed public browsing read. The
// public listener uses it to share the pack-exchange concurrency budget.
func IsBrowsingPath(r *http.Request) bool {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		return false
	}
	parts := splitPath(r.URL.Path)
	if len(parts) < 3 {
		return false
	}
	if _, err := repos.ParseName(parts[0] + "/" + parts[1]); err != nil {
		return false
	}
	if parts[2] != "tree" && parts[2] != "blob" {
		return false
	}
	path, err := repos.ParseTreePath(parts[3:])
	if err != nil {
		return false
	}
	if parts[2] == "blob" && path == "" {
		return false
	}
	return true
}

func browseGitRedirect(name string, parts []string, rawQuery string) (string, bool) {
	base := repoPath(name)
	if len(parts) == 2 {
		if rawQuery != "" {
			base += "?" + rawQuery
		}
		return base, true
	}
	route := parts[2]
	if route != "tree" && route != "blob" {
		return "", false
	}
	path, err := repos.ParseTreePath(parts[3:])
	if err != nil {
		return "", false
	}
	if route == "blob" && path == "" {
		return "", false
	}
	target := browseURL(name, route, path, "")
	if rawQuery != "" {
		target += "?" + rawQuery
	}
	return target, true
}

func treeEntries(name, path, ref string, entries []repos.Entry) []entryView {
	views := make([]entryView, 0, len(entries))
	for _, entry := range entries {
		full := entry.Name
		if path != "" {
			full = path + "/" + entry.Name
		}
		route := "blob"
		if entry.Dir {
			route = "tree"
		}
		kind, size, mode := describeEntry(entry.Mode, entry.Size, entry.Submodule, entry.Symlink, entry.Dir)
		views = append(views, entryView{
			Name: strings.ToValidUTF8(entry.Name, "\uFFFD"),
			Href: browseURL(name, route, full, ref),
			Kind: kind,
			Size: size,
			Mode: mode,
		})
	}
	return views
}

func describeEntry(mode string, size int64, submodule, symlink, dir bool) (kind, humanSize string, shownMode string) {
	switch {
	case dir:
		kind = "directory"
	case submodule:
		kind = "submodule"
	case symlink:
		kind = "symlink"
	case mode == "100755":
		kind = "executable"
	default:
		kind = "file"
	}
	if size >= 0 {
		humanSize = formatSize(size)
	}
	return kind, humanSize, mode
}

func formatSize(size int64) string {
	switch {
	case size < 1024:
		return fmt.Sprintf("%d B", size)
	case size < 1024*1024:
		return fmt.Sprintf("%.1f KiB", float64(size)/1024)
	default:
		return fmt.Sprintf("%.1f MiB", float64(size)/(1024*1024))
	}
}

func browseCrumbs(name, route, path, ref string) []crumb {
	crumbs := []crumb{
		{Name: name, Href: repoPath(name)},
		{Name: "tree", Href: browseURL(name, "tree", "", ref)},
	}
	if path == "" {
		return crumbs
	}
	parts := strings.Split(path, "/")
	for i, part := range parts {
		full := strings.Join(parts[:i+1], "/")
		href := browseURL(name, "tree", full, ref)
		if route == "blob" && i == len(parts)-1 {
			href = browseURL(name, "blob", full, ref)
		}
		crumbs = append(crumbs, crumb{Name: strings.ToValidUTF8(part, "\uFFFD"), Href: href})
	}
	return crumbs
}

func parentTreeURL(name, path, ref string) string {
	if path == "" {
		return ""
	}
	return browseURL(name, "tree", pathParent(path), ref)
}

func refOptions(defaultBranch string, refs []string, linkRef string) []refOption {
	label := defaultBranch
	if label == "" {
		label = "HEAD"
	}
	options := []refOption{{Value: "", Label: label, Selected: linkRef == ""}}
	seen := map[string]bool{"": true}
	for _, ref := range refs {
		value := strings.ToValidUTF8(ref, "\uFFFD")
		if seen[value] || (defaultBranch != "" && value == defaultBranch) {
			continue
		}
		seen[value] = true
		selected := value == linkRef ||
			value == strings.TrimPrefix(linkRef, "refs/tags/") ||
			value == strings.TrimPrefix(linkRef, "refs/heads/")
		options = append(options, refOption{Value: value, Label: value, Selected: selected})
	}
	return options
}

func selectedLinkRef(commit repos.Commit, defaultBranch string) string {
	switch {
	case commit.Ref == "" || commit.Ref == "HEAD":
		return ""
	case defaultBranch != "" && commit.Ref == "refs/heads/"+defaultBranch:
		return ""
	case strings.HasPrefix(commit.Ref, "refs/heads/"):
		return strings.TrimPrefix(commit.Ref, "refs/heads/")
	case strings.HasPrefix(commit.Ref, "refs/tags/"):
		return commit.Ref
	default:
		return strings.ToLower(commit.Ref)
	}
}

func browseURL(name, route, path, ref string) string {
	target := repoPath(name) + "/" + route
	if path != "" {
		target += "/" + escapeTreePath(path)
	}
	if ref != "" {
		target += "?ref=" + url.QueryEscape(ref)
	}
	return target
}

func escapeTreePath(path string) string {
	parts := strings.Split(path, "/")
	for i, part := range parts {
		parts[i] = url.PathEscape(part)
	}
	return strings.Join(parts, "/")
}

func splitPath(path string) []string {
	return strings.Split(strings.TrimPrefix(path, "/"), "/")
}

func pathBase(path string) string {
	if index := strings.LastIndex(path, "/"); index >= 0 {
		path = path[index+1:]
	}
	return strings.ToValidUTF8(path, "\uFFFD")
}

func pathParent(path string) string {
	index := strings.LastIndex(path, "/")
	if index < 0 {
		return ""
	}
	return path[:index]
}

func shortHash(hash string) string {
	if len(hash) > 12 {
		return hash[:12]
	}
	return hash
}
