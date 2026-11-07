package transport

import (
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/kevindurb/tsgit/internal/authz"
	"github.com/kevindurb/tsgit/internal/identity"
	"github.com/kevindurb/tsgit/internal/repos"
)

type fakeBackend struct {
	mu      sync.Mutex
	calls   int
	last    Request
	sawBody []byte
	fn      func(context.Context, Request) error
}

func (f *fakeBackend) Serve(ctx context.Context, req Request) error {
	f.mu.Lock()
	f.calls++
	f.last = req
	f.mu.Unlock()
	if f.fn != nil {
		return f.fn(ctx, req)
	}
	_, err := io.WriteString(req.Out, "advert")
	return err
}

func (f *fakeBackend) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

func testHandler(t *testing.T, id authz.Identity, backend Backend) (*Handler, *repos.Store) {
	t.Helper()
	store, err := repos.Open(t.TempDir(), "main")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return NewHandler(identity.Fixed{Identity: id}, store, backend, logger), store
}

func readerIdentity() authz.Identity {
	return authz.Identity{
		Node:   "reader",
		Grants: []authz.Grant{{Repos: []string{"acme/*"}, Access: authz.AccessRead}},
	}
}

func writerIdentity() authz.Identity {
	return authz.Identity{
		Node:   "writer",
		Grants: []authz.Grant{{Repos: []string{"acme/*"}, Access: authz.AccessWrite}},
	}
}

func request(h http.Handler, method, target string, body io.Reader) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, target, body)
	req.RemoteAddr = "100.64.0.1:1234"
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestDeniedPushNeverStartsBackend(t *testing.T) {
	backend := &fakeBackend{}
	handler, store := testHandler(t, readerIdentity(), backend)
	rec := request(handler, http.MethodGet, "/acme/widget.git/info/refs?service=git-receive-pack", nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
	if backend.callCount() != 0 {
		t.Fatal("backend was called for a denied push")
	}
	if store.Exists("acme/widget") {
		t.Fatal("denied push created a repository")
	}
}

func TestAllowedCloneStartsBackend(t *testing.T) {
	backend := &fakeBackend{}
	handler, store := testHandler(t, readerIdentity(), backend)
	if _, err := store.Create(context.Background(), "acme/widget"); err != nil {
		t.Fatal(err)
	}
	rec := request(handler, http.MethodGet, "/acme/widget.git/info/refs?service=git-upload-pack", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if backend.callCount() != 1 {
		t.Fatalf("backend calls = %d, want 1", backend.callCount())
	}
	if got := rec.Header().Get("Content-Type"); got != "application/x-git-upload-pack-advertisement" {
		t.Fatalf("Content-Type = %q", got)
	}
	want := string(servicePrefix(ServiceUploadPack)) + "advert"
	if rec.Body.String() != want {
		t.Fatalf("body = %q, want %q", rec.Body.String(), want)
	}
	if backend.last.Advertise != true || backend.last.Service != ServiceUploadPack {
		t.Fatalf("backend request = %+v", backend.last)
	}
}

func TestPushToCreate(t *testing.T) {
	backend := &fakeBackend{}
	handler, store := testHandler(t, writerIdentity(), backend)
	rec := request(handler, http.MethodGet, "/acme/widget.git/info/refs?service=git-receive-pack", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if !store.Exists("acme/widget") {
		t.Fatal("authorized discovery did not create the repository")
	}
}

func TestReaderCannotCreate(t *testing.T) {
	backend := &fakeBackend{}
	handler, store := testHandler(t, readerIdentity(), backend)
	rec := request(handler, http.MethodGet, "/acme/widget.git/info/refs?service=git-receive-pack", nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
	if store.Exists("acme/widget") {
		t.Fatal("reader created a repository")
	}
	if backend.callCount() != 0 {
		t.Fatal("backend was called for a denied create")
	}
}

func TestWriterCannotCreateOutsidePattern(t *testing.T) {
	backend := &fakeBackend{}
	handler, store := testHandler(t, writerIdentity(), backend)
	rec := request(handler, http.MethodGet, "/other/repo.git/info/refs?service=git-receive-pack", nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
	if store.Exists("other/repo") {
		t.Fatal("writer created a repository outside its patterns")
	}
}

func TestMissingAndDeniedAreIndistinguishable(t *testing.T) {
	handler, _ := testHandler(t, readerIdentity(), &fakeBackend{})
	denied := request(handler, http.MethodGet, "/acme/widget.git/info/refs?service=git-receive-pack", nil)
	missing := request(handler, http.MethodGet, "/acme/missing.git/info/refs?service=git-upload-pack", nil)
	if denied.Code != http.StatusNotFound || missing.Code != http.StatusNotFound {
		t.Fatalf("statuses = %d/%d, want 404/404", denied.Code, missing.Code)
	}
	if denied.Body.String() != missing.Body.String() {
		t.Fatalf("bodies differ: %q vs %q", denied.Body.String(), missing.Body.String())
	}
}

type errorResolver struct{}

func (errorResolver) Resolve(context.Context, string) (authz.Identity, error) {
	return authz.Identity{}, errors.New("localapi unavailable")
}

func TestIdentityFailureIs503(t *testing.T) {
	store, err := repos.Open(t.TempDir(), "main")
	if err != nil {
		t.Fatal(err)
	}
	backend := &fakeBackend{}
	handler := NewHandler(errorResolver{}, store, backend, slog.New(slog.NewTextHandler(io.Discard, nil)))
	rec := request(handler, http.MethodGet, "/acme/widget.git/info/refs?service=git-upload-pack", nil)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
	if backend.callCount() != 0 {
		t.Fatal("backend started after identity failure")
	}
}

func TestBackendFailureBeforeHeadersIs500(t *testing.T) {
	backend := &fakeBackend{fn: func(context.Context, Request) error {
		return errors.New("git exploded")
	}}
	handler, store := testHandler(t, writerIdentity(), backend)
	if _, err := store.Create(context.Background(), "acme/widget"); err != nil {
		t.Fatal(err)
	}
	rec := request(handler, http.MethodPost, "/acme/widget.git/git-upload-pack", strings.NewReader("body"))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
}

func TestBackendFailureAfterHeadersKeepsStatus(t *testing.T) {
	backend := &fakeBackend{fn: func(_ context.Context, req Request) error {
		_, _ = io.WriteString(req.Out, "partial")
		return errors.New("mid-stream")
	}}
	handler, store := testHandler(t, writerIdentity(), backend)
	if _, err := store.Create(context.Background(), "acme/widget"); err != nil {
		t.Fatal(err)
	}
	rec := request(handler, http.MethodPost, "/acme/widget.git/git-upload-pack", strings.NewReader("body"))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if rec.Body.String() != "partial" {
		t.Fatalf("body = %q", rec.Body.String())
	}
}

type recordingResolver struct {
	addr string
	id   authz.Identity
}

func (r *recordingResolver) Resolve(_ context.Context, addr string) (authz.Identity, error) {
	r.addr = addr
	return r.id, nil
}

func TestSpoofedHeadersAreIgnored(t *testing.T) {
	store, err := repos.Open(t.TempDir(), "main")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Create(context.Background(), "acme/widget"); err != nil {
		t.Fatal(err)
	}
	resolver := &recordingResolver{id: writerIdentity()}
	backend := &fakeBackend{}
	handler := NewHandler(resolver, store, backend, slog.New(slog.NewTextHandler(io.Discard, nil)))

	req := httptest.NewRequest(http.MethodGet, "/acme/widget.git/info/refs?service=git-upload-pack", nil)
	req.RemoteAddr = "100.64.0.1:1234"
	req.Header.Set("X-Forwarded-For", "203.0.113.9")
	req.Header.Set("Tailscale-User-Login", "mallory@example.com")
	req.Header.Set("Tailscale-Funnel-Request", "true")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if resolver.addr != "100.64.0.1:1234" {
		t.Fatalf("resolver saw %q, want the peer address", resolver.addr)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
}

func TestUnsupportedRequestsNeverStartBackend(t *testing.T) {
	tests := []struct{ method, target string }{
		{http.MethodGet, "/"},
		{http.MethodGet, "/acme/widget.git/info/refs"},
		{http.MethodGet, "/acme/widget.git/info/refs?service=git-foo"},
		{http.MethodPost, "/acme/widget.git/info/refs?service=git-upload-pack"},
		{http.MethodGet, "/acme/widget.git/git-upload-pack"},
		{http.MethodPut, "/acme/widget.git/git-receive-pack"},
		{http.MethodGet, "/team/acme/widget.git/info/refs?service=git-upload-pack"},
	}
	for _, tt := range tests {
		t.Run(tt.method+" "+tt.target, func(t *testing.T) {
			backend := &fakeBackend{}
			handler, _ := testHandler(t, writerIdentity(), backend)
			rec := request(handler, tt.method, tt.target, nil)
			if rec.Code != http.StatusNotFound {
				t.Fatalf("status = %d, want 404", rec.Code)
			}
			if backend.callCount() != 0 {
				t.Fatal("backend started for an unsupported request")
			}
		})
	}
}

func TestGzipRequestBodyIsDecoded(t *testing.T) {
	backend := &fakeBackend{fn: func(_ context.Context, req Request) error {
		body, err := io.ReadAll(req.Body)
		if err != nil {
			return err
		}
		_, err = req.Out.Write(body)
		return err
	}}
	handler, store := testHandler(t, writerIdentity(), backend)
	if _, err := store.Create(context.Background(), "acme/widget"); err != nil {
		t.Fatal(err)
	}

	var compressed bytes.Buffer
	zw := gzip.NewWriter(&compressed)
	if _, err := zw.Write([]byte("payload")); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPost, "/acme/widget.git/git-upload-pack", &compressed)
	req.RemoteAddr = "100.64.0.1:1234"
	req.Header.Set("Content-Encoding", "gzip")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Body.String() != "payload" {
		t.Fatalf("body = %q", rec.Body.String())
	}
}

func TestProtocolV2AdvertisementOmitsServiceLine(t *testing.T) {
	backend := &fakeBackend{fn: func(_ context.Context, req Request) error {
		_, err := io.WriteString(req.Out, "version 2\n")
		return err
	}}
	handler, store := testHandler(t, writerIdentity(), backend)
	if _, err := store.Create(context.Background(), "acme/widget"); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/acme/widget.git/info/refs?service=git-upload-pack", nil)
	req.RemoteAddr = "100.64.0.1:1234"
	req.Header.Set("Git-Protocol", "version=2")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Body.String() != "version 2\n" {
		t.Fatalf("v2 body = %q, want the advertisement without a service line", rec.Body.String())
	}
	if backend.last.GitProtocol != "version=2" {
		t.Fatalf("GitProtocol = %q", backend.last.GitProtocol)
	}
}

func TestReceivePackV2KeepsServiceLine(t *testing.T) {
	backend := &fakeBackend{}
	handler, _ := testHandler(t, writerIdentity(), backend)
	req := httptest.NewRequest(http.MethodGet, "/acme/widget.git/info/refs?service=git-receive-pack", nil)
	req.RemoteAddr = "100.64.0.1:1234"
	req.Header.Set("Git-Protocol", "version=2")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if !strings.HasPrefix(rec.Body.String(), string(servicePrefix(ServiceReceivePack))) {
		t.Fatalf("receive-pack v2 body = %q, want the service line", rec.Body.String())
	}
}

func TestNoCacheHeaders(t *testing.T) {
	backend := &fakeBackend{}
	handler, store := testHandler(t, writerIdentity(), backend)
	if _, err := store.Create(context.Background(), "acme/widget"); err != nil {
		t.Fatal(err)
	}
	rec := request(handler, http.MethodPost, "/acme/widget.git/git-receive-pack", strings.NewReader("body"))
	if got := rec.Header().Get("Cache-Control"); !strings.Contains(got, "no-cache") {
		t.Fatalf("Cache-Control = %q", got)
	}
	if got := rec.Header().Get("Content-Type"); got != "application/x-git-receive-pack-result" {
		t.Fatalf("Content-Type = %q", got)
	}
}

func TestReadOnlyHandlerRefusesReceivePack(t *testing.T) {
	backend := &fakeBackend{}
	handler, store := testHandler(t, writerIdentity(), backend)
	handler.ReadOnly = true

	for _, target := range []string{
		"/acme/widget.git/info/refs?service=git-receive-pack",
	} {
		rec := request(handler, http.MethodGet, target, nil)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("%s = %d, want 404", target, rec.Code)
		}
	}
	rec := request(handler, http.MethodPost, "/acme/widget.git/git-receive-pack", strings.NewReader("body"))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("receive-pack POST = %d, want 404", rec.Code)
	}
	if backend.callCount() != 0 {
		t.Fatal("read-only handler started the backend for a write")
	}
	if store.Exists("acme/widget") {
		t.Fatal("read-only handler created a repository")
	}

	if _, err := store.Create(context.Background(), "acme/widget"); err != nil {
		t.Fatal(err)
	}
	rec = request(handler, http.MethodPost, "/acme/widget.git/git-upload-pack", strings.NewReader("body"))
	if rec.Code != http.StatusOK {
		t.Fatalf("upload-pack POST = %d, want 200", rec.Code)
	}
	if backend.callCount() != 1 {
		t.Fatalf("backend calls = %d, want 1", backend.callCount())
	}
}
