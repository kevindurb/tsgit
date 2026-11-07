package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"tailscale.com/ipn"
	"tailscale.com/ipn/ipnstate"
	"tailscale.com/tailcfg"
	"tailscale.com/tailcfg/nodecap"

	"github.com/kevindurb/tsgit/internal/authz"
	"github.com/kevindurb/tsgit/internal/config"
	"github.com/kevindurb/tsgit/internal/identity"
	"github.com/kevindurb/tsgit/internal/repos"
	"github.com/kevindurb/tsgit/internal/transport"
)

func testLogger(w io.Writer) *slog.Logger {
	return slog.New(slog.NewTextHandler(w, nil))
}

func capabilities(https, funnel bool) tailcfg.NodeCapMap {
	cm := tailcfg.NodeCapMap{}
	if https {
		cm[nodecap.HTTPS] = nil
	}
	if funnel {
		cm[nodecap.Funnel] = nil
	}
	return cm
}

func peerStatus(https, funnel bool) *ipnstate.PeerStatus {
	return &ipnstate.PeerStatus{ID: "n123", CapMap: capabilities(https, funnel)}
}

type fakeStatus struct {
	status *ipnstate.Status
	err    error
}

func (f fakeStatus) StatusWithoutPeers(context.Context) (*ipnstate.Status, error) {
	return f.status, f.err
}

func publicStatus(payloads ...string) *ipnstate.Status {
	values := make([]tailcfg.RawMessage, len(payloads))
	for i, payload := range payloads {
		values[i] = tailcfg.RawMessage(payload)
	}
	return &ipnstate.Status{Self: &ipnstate.PeerStatus{
		ID:     "n123",
		CapMap: tailcfg.NodeCapMap{authz.CapFunnel: values},
	}}
}

type recordingBackend struct {
	mu    sync.Mutex
	calls int
}

func (b *recordingBackend) Serve(context.Context, transport.Request) error {
	b.mu.Lock()
	b.calls++
	b.mu.Unlock()
	return nil
}

func (b *recordingBackend) count() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.calls
}

type recordingResolver struct {
	id    authz.Identity
	err   error
	calls int
	addrs []string
}

func (r *recordingResolver) Resolve(_ context.Context, addr string) (authz.Identity, error) {
	r.calls++
	r.addrs = append(r.addrs, addr)
	return r.id, r.err
}

func TestOpenFunnelListener(t *testing.T) {
	var logs bytes.Buffer
	logger := testLogger(&logs)
	cfg := &config.Config{Funnel: false, Port: 443}
	domains := []string{"tsgit.tailnet.ts.net"}

	var bound bool
	bind := func(string, string) (net.Listener, error) {
		bound = true
		return net.Listen("tcp", "127.0.0.1:0")
	}

	if _, on := openFunnelListener(cfg, peerStatus(true, true), domains, bind, logger); on || bound {
		t.Fatal("Funnel listener opened while --funnel is off")
	}

	cfg.Funnel = true
	logs.Reset()
	if _, on := openFunnelListener(cfg, peerStatus(true, false), domains, bind, logger); on {
		t.Fatal("Funnel listener opened without the funnel attribute")
	}
	if bound {
		t.Fatal("bind was attempted without the funnel attribute")
	}
	if !strings.Contains(logs.String(), "funnel attribute") {
		t.Fatalf("missing prerequisite was not logged: %s", logs.String())
	}

	logs.Reset()
	if _, on := openFunnelListener(cfg, peerStatus(false, true), domains, bind, logger); on {
		t.Fatal("Funnel listener opened without HTTPS")
	}
	if !strings.Contains(logs.String(), "https capability") {
		t.Fatalf("missing HTTPS was not logged: %s", logs.String())
	}

	logs.Reset()
	if _, on := openFunnelListener(cfg, peerStatus(true, true), nil, bind, logger); on {
		t.Fatal("Funnel listener opened without a certificate domain")
	}
	if !strings.Contains(logs.String(), "certificate domain") {
		t.Fatalf("missing certificate domain was not logged: %s", logs.String())
	}

	logs.Reset()
	failingBind := func(string, string) (net.Listener, error) {
		return nil, errors.New("funnel disabled by control plane")
	}
	if _, on := openFunnelListener(cfg, peerStatus(true, true), domains, failingBind, logger); on {
		t.Fatal("Funnel listener opened after a bind failure")
	}
	if !strings.Contains(logs.String(), "Funnel listener failed") {
		t.Fatalf("bind failure was not logged prominently: %s", logs.String())
	}

	logs.Reset()
	ln, on := openFunnelListener(cfg, peerStatus(true, true), domains, bind, logger)
	if !on || ln == nil {
		t.Fatalf("Funnel listener not opened with every prerequisite: on=%v", on)
	}
	ln.Close()
}

func TestFunnelPort(t *testing.T) {
	for _, port := range []int{443, 8443, 10000} {
		want := ":" + strconv.Itoa(port)
		if got := funnelPort(port); got != want {
			t.Fatalf("funnelPort(%d) = %q, want %q", port, got, want)
		}
	}
}

func TestPublicServerLimitsMatchDesign(t *testing.T) {
	srv := newPublicServer(http.NotFoundHandler())
	if srv.ReadHeaderTimeout != 10*time.Second {
		t.Errorf("ReadHeaderTimeout = %v, want 10s", srv.ReadHeaderTimeout)
	}
	if srv.ReadTimeout != 60*time.Second {
		t.Errorf("ReadTimeout = %v, want 60s", srv.ReadTimeout)
	}
	if srv.IdleTimeout != 60*time.Second {
		t.Errorf("IdleTimeout = %v, want 60s", srv.IdleTimeout)
	}
	if srv.WriteTimeout != 0 {
		t.Errorf("WriteTimeout = %v, want none", srv.WriteTimeout)
	}
	if srv.MaxHeaderBytes != 1<<20 {
		t.Errorf("MaxHeaderBytes = %d, want 1 MiB", srv.MaxHeaderBytes)
	}
	if srv.ConnContext == nil {
		t.Error("public server has no ConnContext")
	}
	if publicMaxBodyBytes != 64<<20 {
		t.Errorf("publicMaxBodyBytes = %d, want 64 MiB", publicMaxBodyBytes)
	}
}

func TestPublicOversizedDeclaredBodyIsRefused(t *testing.T) {
	called := false
	handler := publicSurface(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		called = true
	}), 4, testLogger(io.Discard))

	req := httptest.NewRequest(http.MethodPost, "/acme/widget.git/git-upload-pack", strings.NewReader("x"))
	req.ContentLength = publicMaxBodyBytes + 1
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413", rec.Code)
	}
	if called {
		t.Fatal("an oversized body reached the handler")
	}
}

func TestPublicPackConcurrencyLimit(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once

	handler := publicSurface(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		once.Do(func() { close(started) })
		<-release
		_, _ = io.WriteString(w, "ok")
	}), 1, testLogger(io.Discard))

	firstDone := make(chan int, 1)
	go func() {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/acme/widget.git/git-upload-pack", strings.NewReader("body")))
		firstDone <- rec.Code
	}()

	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("the first pack request never started")
	}

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/acme/widget.git/git-upload-pack", strings.NewReader("body")))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("saturated limiter status = %d, want 503", rec.Code)
	}
	if rec.Header().Get("Retry-After") == "" {
		t.Fatal("saturated limiter response has no Retry-After")
	}

	close(release)
	if code := <-firstDone; code != http.StatusOK {
		t.Fatalf("first pack request = %d, want 200", code)
	}

	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/acme/widget.git/git-upload-pack", strings.NewReader("body")))
	if rec.Code != http.StatusOK {
		t.Fatalf("released slot status = %d, want 200", rec.Code)
	}
}

type stubConn struct {
	remote net.Addr
}

func (c *stubConn) Read([]byte) (int, error)         { return 0, io.EOF }
func (c *stubConn) Write(p []byte) (int, error)      { return len(p), nil }
func (c *stubConn) Close() error                     { return nil }
func (c *stubConn) LocalAddr() net.Addr              { return c.remote }
func (c *stubConn) RemoteAddr() net.Addr             { return c.remote }
func (c *stubConn) SetDeadline(time.Time) error      { return nil }
func (c *stubConn) SetReadDeadline(time.Time) error  { return nil }
func (c *stubConn) SetWriteDeadline(time.Time) error { return nil }

func TestPublicClientAddrComesFromFunnelConn(t *testing.T) {
	relay := &stubConn{remote: &net.TCPAddr{IP: net.IPv4(192, 0, 2, 10), Port: 443}}
	funnelConn := &ipn.FunnelConn{
		Conn: relay,
		Src:  netip.MustParseAddrPort("203.0.113.9:4321"),
	}
	conn := tls.Server(funnelConn, &tls.Config{})

	if got := publicClientAddr(conn); got != "203.0.113.9:4321" {
		t.Fatalf("publicClientAddr = %q, want the Funnel source", got)
	}
	if got := publicClientAddr(relay); got != "" {
		t.Fatalf("publicClientAddr(relay) = %q, want empty", got)
	}

	var logs bytes.Buffer
	handler := publicSurface(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}), 1, testLogger(&logs))
	req := httptest.NewRequest(http.MethodGet, "/", nil).WithContext(publicConnContext(context.Background(), conn))
	handler.ServeHTTP(httptest.NewRecorder(), req)

	out := logs.String()
	if !strings.Contains(out, "client=203.0.113.9:4321") {
		t.Fatalf("access log does not record the Funnel source: %s", out)
	}
	if !strings.Contains(out, "principal=funnel") {
		t.Fatalf("access log does not identify the public principal: %s", out)
	}
	if strings.Contains(out, "192.0.2.10") {
		t.Fatalf("access log recorded the relay address: %s", out)
	}
}

func TestPublicServerDrainsInFlightRequest(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	started := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once

	handler := publicSurface(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		once.Do(func() { close(started) })
		<-release
		_, _ = io.WriteString(w, "ok")
	}), 4, testLogger(io.Discard))
	srv := newPublicServer(handler)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- runServers(ctx, testLogger(io.Discard), serverRun{srv: srv, ln: ln}) }()

	bodyCh := make(chan string, 1)
	go func() {
		resp, err := http.Get("http://" + ln.Addr().String() + "/")
		if err != nil {
			bodyCh <- "error: " + err.Error()
			return
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		bodyCh <- string(body)
	}()

	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("public request never reached the handler")
	}
	cancel()
	close(release)

	select {
	case body := <-bodyCh:
		if body != "ok" {
			t.Fatalf("in-flight public response = %q", body)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("in-flight public request was not drained")
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("runServers: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("runServers did not return after shutdown")
	}
}

func TestComposeTailnetUsesTailnetResolverForGit(t *testing.T) {
	logger := testLogger(io.Discard)
	store, err := repos.Open(t.TempDir(), "main")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Create(context.Background(), "public/widget"); err != nil {
		t.Fatal(err)
	}

	tailnetResolver := &recordingResolver{id: authz.Identity{
		Node:   "peer",
		Grants: []authz.Grant{{Repos: []string{"private/*"}, Access: authz.AccessRead}},
	}}
	publicResolver := &recordingResolver{id: authz.PublicIdentity([]authz.FunnelGrant{{Repos: []string{"public/*"}}})}
	backend := &recordingBackend{}
	handler := composeTailnet(store, tailnetResolver, publicResolver, backend, logger)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet,
		"/public/widget.git/info/refs?service=git-upload-pack", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404: a public grant leaked into the tailnet path", rec.Code)
	}
	if tailnetResolver.calls != 1 {
		t.Fatalf("tailnet resolver calls = %d, want 1", tailnetResolver.calls)
	}
	if publicResolver.calls != 0 {
		t.Fatalf("public resolver was consulted for a git request (%d calls)", publicResolver.calls)
	}

	// The index uses the tailnet identity to filter and the public resolver
	// only to mark exposure. Give the tailnet viewer its own broader grant.
	indexResolver := &recordingResolver{id: authz.Identity{
		Node:   "peer",
		Grants: []authz.Grant{{Repos: []string{"*/*"}, Access: authz.AccessRead}},
	}}
	indexPublic := &recordingResolver{id: authz.PublicIdentity([]authz.FunnelGrant{{Repos: []string{"public/*"}}})}
	indexHandler := composeTailnet(store, indexResolver, indexPublic, backend, logger)

	rec = httptest.NewRecorder()
	indexHandler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if indexPublic.calls != 1 {
		t.Fatalf("public resolver calls on the index = %d, want 1", indexPublic.calls)
	}
	if !strings.Contains(rec.Body.String(), "badge-public") {
		t.Fatal("tailnet index does not mark the public repository")
	}
}

func TestPublicEndToEndWithFunnelResolver(t *testing.T) {
	logger := testLogger(io.Discard)
	store, err := repos.Open(t.TempDir(), "main")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"public/widget", "other/secret"} {
		if _, err := store.Create(context.Background(), name); err != nil {
			t.Fatal(err)
		}
	}

	backend := &recordingBackend{}
	resolver := identity.NewFunnel(fakeStatus{status: publicStatus(`{"repos":["public/*"]}`)}, logger)
	handler := composePublic(store, resolver, backend, 4, logger)

	request := func(method, target string, body io.Reader) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, target, body)
		// A Funnel connection's remote address is the relay; it must not
		// influence identity.
		req.RemoteAddr = "192.0.2.10:443"
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec
	}

	if rec := request(http.MethodGet, "/public/widget.git/info/refs?service=git-upload-pack", nil); rec.Code != http.StatusOK {
		t.Fatalf("public clone discovery = %d, want 200", rec.Code)
	}
	if backend.count() != 1 {
		t.Fatalf("backend calls after a public clone = %d, want 1", backend.count())
	}

	if rec := request(http.MethodGet, "/other/secret.git/info/refs?service=git-upload-pack", nil); rec.Code != http.StatusNotFound {
		t.Fatalf("non-public clone discovery = %d, want 404", rec.Code)
	}
	if backend.count() != 1 {
		t.Fatal("backend started for a non-public repository")
	}

	denied := request(http.MethodGet, "/public/widget.git/info/refs?service=git-receive-pack", nil)
	missing := request(http.MethodGet, "/other/missing.git/info/refs?service=git-upload-pack", nil)
	if denied.Code != http.StatusNotFound || missing.Code != http.StatusNotFound {
		t.Fatalf("statuses = %d/%d, want 404/404", denied.Code, missing.Code)
	}
	if denied.Body.String() != missing.Body.String() {
		t.Fatal("a denied public write is distinguishable from a missing repository")
	}
	if backend.count() != 1 {
		t.Fatal("backend started for a denied public write")
	}

	if rec := request(http.MethodPost, "/public/widget.git/git-receive-pack", strings.NewReader("body")); rec.Code != http.StatusNotFound {
		t.Fatalf("public receive-pack = %d, want 404", rec.Code)
	}
	if rec := request(http.MethodGet, "/public/new.git/info/refs?service=git-receive-pack", nil); rec.Code != http.StatusNotFound {
		t.Fatalf("public push-to-create = %d, want 404", rec.Code)
	}
	if store.Exists("public/new") {
		t.Fatal("a public request created a repository")
	}
	if backend.count() != 1 {
		t.Fatalf("backend calls after refused writes = %d, want 1", backend.count())
	}

	body := request(http.MethodGet, "/", nil).Body.String()
	if !strings.Contains(body, "public/widget") || strings.Contains(body, "other/secret") {
		t.Fatalf("public index = %q", body)
	}
}
