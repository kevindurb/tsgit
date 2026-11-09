package web

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
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
	infos     []repos.Info
	listErr   error
	infoErr   map[string]error
	deleteErr error
	deleted   []string

	onResolveRef func(name, ref string) (repos.Commit, error)
	onCommit     func(name, commit string) (repos.Commit, error)
	onTree       func(name, commit, path string) ([]repos.Entry, error)
	onBlob       func(name, commit, path string) (repos.Blob, error)
	onReadme     func(name, commit string) (repos.Blob, bool, error)
}

func (f *fakeStore) ResolveRef(_ context.Context, name, ref string) (repos.Commit, error) {
	if f.onResolveRef != nil {
		return f.onResolveRef(name, ref)
	}
	return repos.Commit{}, repos.ErrNotExist
}

func (f *fakeStore) Commit(_ context.Context, name, commit string) (repos.Commit, error) {
	if f.onCommit != nil {
		return f.onCommit(name, commit)
	}
	return repos.Commit{}, repos.ErrNotExist
}

func (f *fakeStore) Tree(_ context.Context, name, commit, path string) ([]repos.Entry, error) {
	if f.onTree != nil {
		return f.onTree(name, commit, path)
	}
	return nil, repos.ErrNotExist
}

func (f *fakeStore) Blob(_ context.Context, name, commit, path string) (repos.Blob, error) {
	if f.onBlob != nil {
		return f.onBlob(name, commit, path)
	}
	return repos.Blob{}, repos.ErrNotExist
}

func (f *fakeStore) Readme(_ context.Context, name, commit string) (repos.Blob, bool, error) {
	if f.onReadme != nil {
		return f.onReadme(name, commit)
	}
	return repos.Blob{}, false, nil
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

func (f *fakeStore) Delete(_ context.Context, name string) error {
	if f.deleteErr != nil {
		return f.deleteErr
	}
	for i, info := range f.infos {
		if info.Name == name {
			f.infos = append(f.infos[:i], f.infos[i+1:]...)
			f.deleted = append(f.deleted, name)
			return nil
		}
	}
	return repos.ErrNotExist
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

func adminIdentity() authz.Identity {
	return authz.Identity{
		Node:   "admin",
		Login:  "admin@example.com",
		Grants: []authz.Grant{{Repos: []string{"*/*"}, Access: authz.AccessAdmin}},
	}
}

func request(h http.Handler, method, target string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, target, nil)
	req.RemoteAddr = "100.64.0.1:1234"
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func postForm(h http.Handler, target string, form url.Values, headers map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, target, strings.NewReader(form.Encode()))
	req.RemoteAddr = "100.64.0.1:1234"
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

const testDeleteSecret = "0123456789abcdef0123456789abcdef"

func newDeleteTestHandler(t *testing.T, id authz.Identity, store DeleteStore) *DeleteHandler {
	t.Helper()
	h, err := NewDeleteHandler(DeleteConfig{
		Resolver: fakeResolver{id: id},
		Store:    store,
		Log:      slog.New(slog.NewTextHandler(io.Discard, nil)),
		Version:  "test",
		Secret:   []byte(testDeleteSecret),
	})
	if err != nil {
		t.Fatalf("NewDeleteHandler: %v", err)
	}
	return h
}

func deleteTokenFor(id authz.Identity, repo string, now time.Time) string {
	return signDeleteToken([]byte(testDeleteSecret), id.Node, repo, now)
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

func publicIdentity() authz.Identity {
	return authz.PublicIdentity([]authz.FunnelGrant{{Repos: []string{"public/*"}}})
}

func newPublicTestHandler(t *testing.T, store RepositoryStore) *Handler {
	t.Helper()
	return New(Config{
		Resolver: fakeResolver{id: publicIdentity()},
		Store:    store,
		Log:      slog.New(slog.NewTextHandler(io.Discard, nil)),
		Version:  "test",
		Public:   true,
	})
}

func TestPublicIndexIsFilteredAndReadOnly(t *testing.T) {
	store := &fakeStore{infos: []repos.Info{
		{Name: "public/widget", Description: "published"},
		{Name: "other/secret", Description: "private"},
	}}
	body := request(newPublicTestHandler(t, store), http.MethodGet, "/").Body.String()

	if !strings.Contains(body, "public/widget") {
		t.Error("public index does not list the public repository")
	}
	if strings.Contains(body, "other/secret") {
		t.Error("public index listed a non-public repository")
	}
	if strings.Contains(body, "badge-write") {
		t.Error("public index has a writable indicator")
	}
	if strings.Contains(body, "pushing to it") {
		t.Error("public index tells the viewer to push")
	}
}

func TestPublicEmptyRepositoryHidesPushInstructions(t *testing.T) {
	store := &fakeStore{infos: []repos.Info{{
		Name:   "public/empty",
		Branch: "main",
		Empty:  true,
	}}}
	body := request(newPublicTestHandler(t, store), http.MethodGet, "/public/empty").Body.String()

	if !strings.Contains(body, "no commits") && !strings.Contains(body, "Empty repository") {
		t.Fatalf("public empty summary does not identify the empty state: %q", body)
	}
	for _, unwanted := range []string{"git push", "git remote add origin", "Initial commit"} {
		if strings.Contains(body, unwanted) {
			t.Errorf("public empty summary contains first-push instruction %q", unwanted)
		}
	}
}

func TestTailnetIndexMarksPublicRepositories(t *testing.T) {
	store := &fakeStore{infos: []repos.Info{
		{Name: "public/widget"},
		{Name: "other/secret"},
	}}
	handler := New(Config{
		Resolver:       fakeResolver{id: writerIdentity()},
		PublicResolver: fakeResolver{id: publicIdentity()},
		Store:          store,
		Log:            slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	body := request(handler, http.MethodGet, "/").Body.String()

	if got := strings.Count(body, "badge-public"); got != 1 {
		t.Fatalf("public badge count = %d, want 1\n%s", got, body)
	}
	if !strings.Contains(body, "other/secret") {
		t.Fatal("tailnet index no longer lists a non-public readable repository")
	}
}

func TestTailnetIndexSurvivesPublicLookupFailure(t *testing.T) {
	store := &fakeStore{infos: []repos.Info{{Name: "acme/widget"}}}
	handler := New(Config{
		Resolver:       fakeResolver{id: readerIdentity()},
		PublicResolver: fakeResolver{err: errors.New("status down")},
		Store:          store,
		Log:            slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	rec := request(handler, http.MethodGet, "/")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "acme/widget") {
		t.Fatalf("tailnet index failed after a public lookup error: %d", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "badge-public") {
		t.Error("a failed public lookup marked a repository public")
	}
}

func TestPublicSummaryDeniedAndMissingAreIdentical(t *testing.T) {
	store := &fakeStore{infos: []repos.Info{
		{Name: "public/widget"},
		{Name: "other/secret"},
	}}
	handler := newPublicTestHandler(t, store)

	visible := request(handler, http.MethodGet, "/public/widget")
	if visible.Code != http.StatusOK {
		t.Fatalf("public summary = %d, want 200", visible.Code)
	}
	denied := request(handler, http.MethodGet, "/other/secret")
	missing := request(handler, http.MethodGet, "/other/missing")
	if denied.Code != http.StatusNotFound || missing.Code != http.StatusNotFound {
		t.Fatalf("statuses = %d/%d, want 404/404", denied.Code, missing.Code)
	}
	if denied.Body.String() != missing.Body.String() {
		t.Fatal("public non-public and missing summaries differ")
	}
}

func TestDeleteConfirmPage(t *testing.T) {
	store := &fakeStore{infos: []repos.Info{{
		Name:        "acme/widget",
		Description: "widget service",
		Branch:      "main",
		Latest:      repos.Commit{Hash: "abc1234", Subject: "fix", When: time.Now().Add(-time.Hour)},
	}}}
	rec := request(newDeleteTestHandler(t, adminIdentity(), store), http.MethodGet, "/.tsgit/delete/acme/widget")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{
		"Delete acme/widget",
		"permanently",
		"widget service",
		"main",
		"abc1234",
		"http://example.com/acme/widget.git",
		`name="csrf"`,
		`name="confirm"`,
		"repository administration",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("confirmation page does not contain %q", want)
		}
	}
}

func TestDeleteWriterRefused(t *testing.T) {
	store := &fakeStore{infos: []repos.Info{{Name: "acme/widget"}}}
	rec := request(newDeleteTestHandler(t, writerIdentity(), store), http.MethodGet, "/.tsgit/delete/acme/widget")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestDeleteDeniedAndMissingIdentical(t *testing.T) {
	store := &fakeStore{infos: []repos.Info{{Name: "acme/widget"}}}
	denied := request(newDeleteTestHandler(t, writerIdentity(), store), http.MethodGet, "/.tsgit/delete/acme/widget")
	missing := request(newDeleteTestHandler(t, adminIdentity(), store), http.MethodGet, "/.tsgit/delete/acme/missing")
	if denied.Code != http.StatusNotFound || missing.Code != http.StatusNotFound {
		t.Fatalf("statuses = %d and %d, want 404 and 404", denied.Code, missing.Code)
	}
	if denied.Body.String() != missing.Body.String() {
		t.Fatal("denied and missing deletion pages differ")
	}
}

func TestDeleteMatchingConfirmation(t *testing.T) {
	store := &fakeStore{infos: []repos.Info{{Name: "acme/widget"}}}
	id := adminIdentity()
	h := newDeleteTestHandler(t, id, store)
	form := url.Values{
		"csrf":    []string{deleteTokenFor(id, "acme/widget", time.Now())},
		"confirm": []string{"acme/widget"},
	}
	rec := postForm(h, "/.tsgit/delete/acme/widget", form, map[string]string{
		"Origin":         "http://example.com",
		"Sec-Fetch-Site": "same-origin",
	})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303", rec.Code)
	}
	if got := rec.Header().Get("Location"); got != "/?deleted=acme%2Fwidget" {
		t.Fatalf("Location = %q", got)
	}
	if len(store.deleted) != 1 || store.deleted[0] != "acme/widget" {
		t.Fatalf("deleted = %v, want acme/widget", store.deleted)
	}
	if _, err := store.Info(context.Background(), "acme/widget"); !errors.Is(err, repos.ErrNotExist) {
		t.Fatalf("repository still exists after delete: %v", err)
	}
}

func TestDeleteMismatchedConfirmation(t *testing.T) {
	store := &fakeStore{infos: []repos.Info{{Name: "acme/widget"}}}
	id := adminIdentity()
	h := newDeleteTestHandler(t, id, store)
	form := url.Values{
		"csrf":    []string{deleteTokenFor(id, "acme/widget", time.Now())},
		"confirm": []string{"acme/widg"},
	}
	rec := postForm(h, "/.tsgit/delete/acme/widget", form, nil)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	if len(store.deleted) != 0 {
		t.Fatalf("mismatched confirmation deleted %v", store.deleted)
	}
	if !strings.Contains(rec.Body.String(), "Type the exact repository name") {
		t.Fatalf("mismatch body = %q", rec.Body.String())
	}
}

func TestDeleteCrossSiteRefused(t *testing.T) {
	store := &fakeStore{infos: []repos.Info{{Name: "acme/widget"}}}
	id := adminIdentity()
	h := newDeleteTestHandler(t, id, store)
	form := url.Values{
		"csrf":    []string{deleteTokenFor(id, "acme/widget", time.Now())},
		"confirm": []string{"acme/widget"},
	}
	for _, headers := range []map[string]string{
		{"Origin": "https://evil.example"},
		{"Sec-Fetch-Site": "cross-site"},
		{"Sec-Fetch-Site": "same-site"},
	} {
		store.deleted = nil
		rec := postForm(h, "/.tsgit/delete/acme/widget", form, headers)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("headers %v: status = %d, want 403", headers, rec.Code)
		}
		if len(store.deleted) != 0 {
			t.Fatalf("headers %v: deleted %v", headers, store.deleted)
		}
	}
}

func TestDeleteBadTokenRefused(t *testing.T) {
	store := &fakeStore{infos: []repos.Info{{Name: "acme/widget"}}}
	id := adminIdentity()
	h := newDeleteTestHandler(t, id, store)
	valid := deleteTokenFor(id, "acme/widget", time.Now())
	otherNode := id
	otherNode.Node = "other"
	for name, token := range map[string]string{
		"missing":    "",
		"altered":    valid + "x",
		"expired":    deleteTokenFor(id, "acme/widget", time.Now().Add(-2*time.Hour)),
		"other repo": deleteTokenFor(id, "acme/other", time.Now()),
		"other node": deleteTokenFor(otherNode, "acme/widget", time.Now()),
	} {
		t.Run(name, func(t *testing.T) {
			store.deleted = nil
			form := url.Values{
				"csrf":    []string{token},
				"confirm": []string{"acme/widget"},
			}
			rec := postForm(h, "/.tsgit/delete/acme/widget", form, map[string]string{"Origin": "http://example.com"})
			if rec.Code != http.StatusForbidden {
				t.Fatalf("status = %d, want 403", rec.Code)
			}
			if len(store.deleted) != 0 {
				t.Fatalf("bad token deleted %v", store.deleted)
			}
		})
	}
}

func TestDeleteIdentityFailure(t *testing.T) {
	h, err := NewDeleteHandler(DeleteConfig{
		Resolver: fakeResolver{err: errors.New("no whois")},
		Store:    &fakeStore{},
		Log:      slog.New(slog.NewTextHandler(io.Discard, nil)),
		Secret:   []byte(testDeleteSecret),
	})
	if err != nil {
		t.Fatalf("NewDeleteHandler: %v", err)
	}
	rec := request(h, http.MethodGet, "/.tsgit/delete/acme/widget")
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
}

func TestDeleteMethodNotAllowed(t *testing.T) {
	store := &fakeStore{infos: []repos.Info{{Name: "acme/widget"}}}
	rec := request(newDeleteTestHandler(t, adminIdentity(), store), http.MethodPut, "/.tsgit/delete/acme/widget")
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", rec.Code)
	}
	if allow := rec.Header().Get("Allow"); allow != "GET, HEAD, POST" {
		t.Fatalf("Allow = %q", allow)
	}
}

func TestDeleteCleanupErrorStillRedirects(t *testing.T) {
	store := &fakeStore{
		infos:     []repos.Info{{Name: "acme/widget"}},
		deleteErr: repos.ErrCleanup,
	}
	id := adminIdentity()
	h := newDeleteTestHandler(t, id, store)
	form := url.Values{
		"csrf":    []string{deleteTokenFor(id, "acme/widget", time.Now())},
		"confirm": []string{"acme/widget"},
	}
	rec := postForm(h, "/.tsgit/delete/acme/widget", form, nil)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303", rec.Code)
	}
}

func TestDeleteStoreError(t *testing.T) {
	store := &fakeStore{
		infos:     []repos.Info{{Name: "acme/widget"}},
		deleteErr: errors.New("boom"),
	}
	id := adminIdentity()
	h := newDeleteTestHandler(t, id, store)
	form := url.Values{
		"csrf":    []string{deleteTokenFor(id, "acme/widget", time.Now())},
		"confirm": []string{"acme/widget"},
	}
	rec := postForm(h, "/.tsgit/delete/acme/widget", form, nil)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
}

func TestNewDeleteHandlerRequiresSecret(t *testing.T) {
	if _, err := NewDeleteHandler(DeleteConfig{
		Resolver: fakeResolver{},
		Store:    &fakeStore{},
		Secret:   []byte("short"),
	}); err == nil {
		t.Fatal("short CSRF key was accepted")
	}
}

func TestDeleteTokenVerification(t *testing.T) {
	secret := []byte(testDeleteSecret)
	now := time.Now()
	token := signDeleteToken(secret, "admin", "acme/widget", now)
	if !verifyDeleteToken(secret, token, "admin", "acme/widget", now) {
		t.Fatal("valid token was rejected")
	}
	for name, verify := range map[string]bool{
		"other node": verifyDeleteToken(secret, token, "other", "acme/widget", now),
		"other repo": verifyDeleteToken(secret, token, "admin", "acme/other", now),
		"expired":    verifyDeleteToken(secret, token, "admin", "acme/widget", now.Add(2*time.Hour)),
		"altered":    verifyDeleteToken(secret, token+"x", "admin", "acme/widget", now),
	} {
		if verify {
			t.Errorf("%s token was accepted", name)
		}
	}
}

func TestSummaryShowsDeleteLinkForAdmin(t *testing.T) {
	store := &fakeStore{infos: []repos.Info{{Name: "acme/widget"}}}
	adminBody := request(newTestHandler(t, adminIdentity(), store), http.MethodGet, "/acme/widget").Body.String()
	if !strings.Contains(adminBody, "/.tsgit/delete/acme/widget") {
		t.Fatal("admin summary has no delete link")
	}
	if !strings.Contains(adminBody, "badge-admin") {
		t.Fatal("admin summary has no admin badge")
	}

	writerBody := request(newTestHandler(t, writerIdentity(), store), http.MethodGet, "/acme/widget").Body.String()
	if strings.Contains(writerBody, "/.tsgit/delete/") {
		t.Fatal("writer summary has a delete link")
	}
	if !strings.Contains(writerBody, "badge-write") {
		t.Fatal("writer summary has no write badge")
	}
}

func TestIndexShowsDeletedNotice(t *testing.T) {
	handler := newTestHandler(t, adminIdentity(), &fakeStore{})
	body := request(handler, http.MethodGet, "/?deleted=acme%2Fwidget").Body.String()
	if !strings.Contains(body, "Deleted acme/widget") {
		t.Fatalf("index does not show the deletion notice: %q", body)
	}
	body = request(handler, http.MethodGet, "/?deleted=not-a-name").Body.String()
	if strings.Contains(body, "Deleted ") {
		t.Fatal("invalid deletion notice was rendered")
	}
}
