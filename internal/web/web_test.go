package web

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/kevindurb/tsgit/internal/authz"
	"github.com/kevindurb/tsgit/internal/repos"
)

type fakeResolver struct {
	id  authz.Identity
	err error
}

func (f fakeResolver) Resolve(context.Context, string) (authz.Identity, error) {
	return f.id, f.err
}

type fakeStore struct {
	infos   []repos.Info
	listErr error
	infoErr map[string]error
}

func (f *fakeStore) List(context.Context) ([]repos.Info, error) {
	return f.infos, f.listErr
}

func (f *fakeStore) Info(_ context.Context, name string) (repos.Info, error) {
	for _, info := range f.infos {
		if info.Name == name {
			return info, f.infoErr[name]
		}
	}
	return repos.Info{Name: name}, repos.ErrNotExist
}

func newTestHandler(t *testing.T, id authz.Identity, store RepositoryStore) *Handler {
	t.Helper()
	return New(Config{
		Resolver: fakeResolver{id: id},
		Store:    store,
		Log:      slog.New(slog.NewTextHandler(io.Discard, nil)),
		Version:  "test",
	})
}

func readerIdentity() authz.Identity {
	return authz.Identity{
		Node:   "reader",
		Login:  "reader@example.com",
		Grants: []authz.Grant{{Repos: []string{"acme/*"}, Access: authz.AccessRead}},
	}
}

func writerIdentity() authz.Identity {
	return authz.Identity{
		Node:   "writer",
		Login:  "writer@example.com",
		Grants: []authz.Grant{{Repos: []string{"*/*"}, Access: authz.AccessWrite}},
	}
}

func request(h http.Handler, method, target string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, target, nil)
	req.RemoteAddr = "100.64.0.1:1234"
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestIndexFiltersAndShowsCloneAndAccess(t *testing.T) {
	now := time.Now()
	store := &fakeStore{infos: []repos.Info{
		{Name: "acme/widget", Description: "widget service", Latest: repos.Commit{Hash: "abc1234", Subject: "fix", When: now.Add(-time.Hour)}},
		{Name: "other/secret", Latest: repos.Commit{Hash: "def5678", Subject: "hidden", When: now}},
	}}

	body := request(newTestHandler(t, readerIdentity(), store), http.MethodGet, "/").Body.String()
	for _, want := range []string{"acme/widget", "widget service", "http://example.com/acme/widget.git", `badge">read`} {
		if !strings.Contains(body, want) {
			t.Errorf("index does not contain %q", want)
		}
	}
	if strings.Contains(body, "other/secret") {
		t.Error("index listed a repository the viewer cannot read")
	}

	body = request(newTestHandler(t, writerIdentity(), store), http.MethodGet, "/").Body.String()
	if !strings.Contains(body, "badge-write") {
		t.Error("writer index does not mark a writable repository")
	}
}

func TestIndexEmpty(t *testing.T) {
	rec := request(newTestHandler(t, readerIdentity(), &fakeStore{}), http.MethodGet, "/")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "No repositories") {
		t.Fatalf("empty index = %q", rec.Body.String())
	}
}

func TestIndexSortsByActivity(t *testing.T) {
	now := time.Now()
	store := &fakeStore{infos: []repos.Info{
		{Name: "acme/old", Latest: repos.Commit{Hash: "a", When: now.Add(-48 * time.Hour)}},
		{Name: "acme/new", Latest: repos.Commit{Hash: "b", When: now.Add(-time.Hour)}},
	}}
	body := request(newTestHandler(t, readerIdentity(), store), http.MethodGet, "/").Body.String()
	if strings.Index(body, "acme/new") > strings.Index(body, "acme/old") {
		t.Fatal("index is not sorted by latest activity")
	}
}

func TestRepoSummary(t *testing.T) {
	store := &fakeStore{infos: []repos.Info{{
		Name:        "acme/widget",
		Description: "widget service",
		Branch:      "main",
		Latest:      repos.Commit{Hash: "abc1234", Subject: "fix the thing", When: time.Now().Add(-2 * time.Hour)},
		Branches:    3,
		Tags:        1,
	}}}
	rec := request(newTestHandler(t, readerIdentity(), store), http.MethodGet, "/acme/widget")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{"widget service", "main", "abc1234", "fix the thing", "http://example.com/acme/widget.git"} {
		if !strings.Contains(body, want) {
			t.Errorf("summary does not contain %q", want)
		}
	}
}

func TestRepoSummaryDotGitRedirect(t *testing.T) {
	rec := request(newTestHandler(t, readerIdentity(), &fakeStore{}), http.MethodGet, "/acme/widget.git")
	if rec.Code != http.StatusMovedPermanently {
		t.Fatalf("status = %d, want 301", rec.Code)
	}
	if got := rec.Header().Get("Location"); got != "/acme/widget" {
		t.Fatalf("Location = %q, want /acme/widget", got)
	}
}

func TestRepoSummaryEmpty(t *testing.T) {
	store := &fakeStore{infos: []repos.Info{{
		Name:   "acme/empty",
		Branch: "main",
		Empty:  true,
	}}}
	body := request(newTestHandler(t, readerIdentity(), store), http.MethodGet, "/acme/empty").Body.String()
	for _, want := range []string{"Empty repository", "git remote add origin", "git push -u origin main"} {
		if !strings.Contains(body, want) {
			t.Errorf("empty summary does not contain %q", want)
		}
	}
}

func TestRepoSummaryUnbornHeadWithOtherRefs(t *testing.T) {
	store := &fakeStore{infos: []repos.Info{{
		Name:     "acme/widget",
		Branch:   "main",
		Empty:    true,
		Refs:     []string{"master"},
		Branches: 1,
		Latest:   repos.Commit{Hash: "abc1234", Subject: "on master"},
	}}}
	body := request(newTestHandler(t, readerIdentity(), store), http.MethodGet, "/acme/widget").Body.String()
	if !strings.Contains(body, "no commits") || !strings.Contains(body, "master") {
		t.Fatalf("unborn-HEAD summary = %q", body)
	}
	if strings.Contains(body, "Empty repository") {
		t.Fatal("unborn-HEAD summary presented the repository as never pushed")
	}
}

func TestDeniedAndMissingAreIdentical(t *testing.T) {
	store := &fakeStore{infos: []repos.Info{{Name: "other/secret"}}}
	handler := newTestHandler(t, readerIdentity(), store)

	denied := request(handler, http.MethodGet, "/other/secret")
	missing := request(handler, http.MethodGet, "/other/missing")
	if denied.Code != http.StatusNotFound || missing.Code != http.StatusNotFound {
		t.Fatalf("statuses = %d and %d, want 404 and 404", denied.Code, missing.Code)
	}
	if denied.Body.String() != missing.Body.String() {
		t.Fatal("denied and missing responses differ")
	}
}

func TestMutatingMethodsRefused(t *testing.T) {
	handler := newTestHandler(t, readerIdentity(), &fakeStore{})
	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete} {
		for _, target := range []string{"/", "/acme/widget"} {
			rec := request(handler, method, target)
			if rec.Code != http.StatusMethodNotAllowed {
				t.Errorf("%s %s = %d, want 405", method, target, rec.Code)
			}
			if allow := rec.Header().Get("Allow"); !strings.Contains(allow, http.MethodGet) {
				t.Errorf("%s %s Allow = %q", method, target, allow)
			}
		}
	}
}

func TestIdentityFailure(t *testing.T) {
	handler := New(Config{
		Resolver: fakeResolver{err: errors.New("no whois")},
		Store:    &fakeStore{},
		Log:      slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	rec := request(handler, http.MethodGet, "/")
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "Identity unavailable") {
		t.Fatalf("body = %q", rec.Body.String())
	}
}

func TestSpoofedHeaderIgnored(t *testing.T) {
	store := &fakeStore{infos: []repos.Info{{Name: "acme/widget"}, {Name: "other/secret"}}}
	handler := newTestHandler(t, readerIdentity(), store)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "100.64.0.1:1234"
	req.Header.Set("Tailscale-User-Login", "other@example.com")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if strings.Contains(rec.Body.String(), "other/secret") {
		t.Fatal("a spoofed login header changed the repository list")
	}
}

func TestRepositoryTextIsEscaped(t *testing.T) {
	store := &fakeStore{infos: []repos.Info{{
		Name:        "acme/widget",
		Description: `<script>alert("x")</script>`,
		Branch:      "main",
		Latest:      repos.Commit{Hash: "abc1234", Subject: `<img src=x onerror=alert(1)>`},
	}}}
	body := request(newTestHandler(t, readerIdentity(), store), http.MethodGet, "/acme/widget").Body.String()
	for _, want := range []string{"&lt;script&gt;", "&lt;img"} {
		if !strings.Contains(body, want) {
			t.Errorf("escaped body does not contain %q", want)
		}
	}
	if strings.Contains(body, "<script>alert") || strings.Contains(body, "<img src=x") {
		t.Fatal("repository-controlled text was rendered as markup")
	}
}

func TestInvalidUTF8Normalized(t *testing.T) {
	store := &fakeStore{infos: []repos.Info{{
		Name:        "acme/widget",
		Description: "bad\xffdescription",
		Branch:      "main",
		Latest:      repos.Commit{Hash: "abc1234", Subject: "bad\xffsubject"},
	}}}
	body := request(newTestHandler(t, readerIdentity(), store), http.MethodGet, "/acme/widget").Body.String()
	for _, want := range []string{"bad\uFFFDdescription", "bad\uFFFDsubject"} {
		if !strings.Contains(body, want) {
			t.Errorf("body does not contain normalized text %q", want)
		}
	}
}

func TestPathEscaping(t *testing.T) {
	store := &fakeStore{infos: []repos.Info{
		{Name: "acme/my repo"},
		{Name: "acme/café"},
	}}
	handler := newTestHandler(t, writerIdentity(), store)

	body := request(handler, http.MethodGet, "/").Body.String()
	for _, want := range []string{"/acme/my%20repo", "/acme/caf%C3%A9"} {
		if !strings.Contains(body, want) {
			t.Errorf("index does not contain escaped link %q", want)
		}
	}

	rec := request(handler, http.MethodGet, "/acme/my%20repo")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "acme/my repo") {
		t.Fatalf("space-named repository page = %d, %q", rec.Code, rec.Body.String())
	}

	rec = request(handler, http.MethodGet, "/acme/my%20repo.git")
	if rec.Code != http.StatusMovedPermanently || rec.Header().Get("Location") != "/acme/my%20repo" {
		t.Fatalf("redirect = %d, %q", rec.Code, rec.Header().Get("Location"))
	}
}

func TestSecurityHeaders(t *testing.T) {
	rec := request(newTestHandler(t, readerIdentity(), &fakeStore{}), http.MethodGet, "/")
	for name, want := range map[string]string{
		"Content-Security-Policy": "default-src 'none'",
		"X-Content-Type-Options":  "nosniff",
		"Referrer-Policy":         "no-referrer",
		"Cache-Control":           "no-store",
	} {
		if got := rec.Header().Get(name); !strings.Contains(got, want) {
			t.Errorf("%s = %q, want it to contain %q", name, got, want)
		}
	}
	body := rec.Body.String()
	if !strings.Contains(body, `/.tsgit/web.css`) {
		t.Error("page does not load the embedded stylesheet")
	}
	if strings.Contains(body, "<script") {
		t.Error("page contains a script")
	}
}

func TestAssets(t *testing.T) {
	handler := newTestHandler(t, readerIdentity(), &fakeStore{})

	rec := request(handler, http.MethodGet, "/.tsgit/web.css")
	if rec.Code != http.StatusOK {
		t.Fatalf("css status = %d", rec.Code)
	}
	if got := rec.Header().Get("Content-Type"); !strings.HasPrefix(got, "text/css") {
		t.Fatalf("css Content-Type = %q", got)
	}
	etag := rec.Header().Get("ETag")
	if etag == "" {
		t.Fatal("css missing ETag")
	}
	if !strings.Contains(rec.Body.String(), "prefers-color-scheme: dark") {
		t.Fatal("css missing the dark scheme")
	}

	req := httptest.NewRequest(http.MethodGet, "/.tsgit/web.css", nil)
	req.Header.Set("If-None-Match", etag)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotModified {
		t.Fatalf("revalidation status = %d, want 304", rec.Code)
	}

	rec = request(handler, http.MethodGet, "/.tsgit/favicon.svg")
	if rec.Code != http.StatusOK || !strings.HasPrefix(rec.Header().Get("Content-Type"), "image/svg+xml") {
		t.Fatalf("favicon = %d, %q", rec.Code, rec.Header().Get("Content-Type"))
	}

	failing := New(Config{
		Resolver: fakeResolver{err: errors.New("no whois")},
		Store:    &fakeStore{},
		Log:      slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if rec := request(failing, http.MethodGet, "/.tsgit/web.css"); rec.Code != http.StatusOK {
		t.Fatalf("asset with a failing resolver = %d, want 200", rec.Code)
	}
}

func TestListError(t *testing.T) {
	store := &fakeStore{listErr: errors.New("disk gone")}
	rec := request(newTestHandler(t, readerIdentity(), store), http.MethodGet, "/")
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
}

func TestMetadataErrorSummary(t *testing.T) {
	store := &fakeStore{
		infos:   []repos.Info{{Name: "acme/widget", Branch: "main"}},
		infoErr: map[string]error{"acme/widget": errors.New("boom")},
	}
	rec := request(newTestHandler(t, readerIdentity(), store), http.MethodGet, "/acme/widget")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "could not be read") {
		t.Fatalf("metadata notice missing: %q", rec.Body.String())
	}
}

func TestHeadRequestHasNoBody(t *testing.T) {
	rec := request(newTestHandler(t, readerIdentity(), &fakeStore{}), http.MethodHead, "/")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if rec.Body.Len() != 0 {
		t.Fatalf("HEAD body = %q, want empty", rec.Body.String())
	}
}

func TestNotFoundPaths(t *testing.T) {
	handler := newTestHandler(t, readerIdentity(), &fakeStore{})
	for _, target := range []string{"/acme", "/a/b/c", "/acme/widget/"} {
		if rec := request(handler, http.MethodGet, target); rec.Code != http.StatusNotFound {
			t.Errorf("GET %s = %d, want 404", target, rec.Code)
		}
	}
}
