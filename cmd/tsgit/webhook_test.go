package main

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"tailscale.com/tsnet"

	"github.com/kevindurb/tsgit/internal/config"
	"github.com/kevindurb/tsgit/internal/hooks"
)

type capturedDelivery struct {
	header http.Header
	body   []byte
}

func TestWebhookDeliveryEndToEndInDevMode(t *testing.T) {
	var mu sync.Mutex
	var deliveries []capturedDelivery
	receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("receiver read body: %v", err)
		}
		mu.Lock()
		deliveries = append(deliveries, capturedDelivery{header: r.Header.Clone(), body: body})
		mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	}))
	defer receiver.Close()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	base := "http://" + ln.Addr().String()

	var currentHooks atomic.Value
	currentHooks.Store([]hooks.Hook{{Repos: []string{"*/*"}, URL: receiver.URL, SecretEnv: "TSGIT_TEST_HOOK_SECRET"}})

	devWebhooks = hooks.NewDispatcher(hooks.Config{
		Load: func(context.Context) ([]hooks.Hook, error) {
			return currentHooks.Load().([]hooks.Hook), nil
		},
		Client:  http.DefaultClient,
		BaseURL: base,
		Version: "test",
		Log:     slog.New(slog.NewTextHandler(io.Discard, nil)),
		LookupEnv: func(name string) (string, bool) {
			if name == "TSGIT_TEST_HOOK_SECRET" {
				return "acceptance-secret", true
			}
			return os.LookupEnv(name)
		},
	})
	t.Cleanup(func() { devWebhooks = nil })

	originalListen := devListen
	devListen = func(string, string) (net.Listener, error) { return ln, nil }
	t.Cleanup(func() { devListen = originalListen })

	originalTailnet := startTailnet
	startTailnet = func(context.Context, *config.Config, *slog.Logger) (*tailnet, error) {
		t.Error("development mode contacted tsnet")
		return nil, errors.New("unexpected tsnet start")
	}
	t.Cleanup(func() { startTailnet = originalTailnet })

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	cfg := &config.Config{DevListen: "test", DefaultBranch: "main", Log: "error"}
	go func() { done <- serve(ctx, cfg, io.Discard) }()

	deadline := time.Now().Add(10 * time.Second)
	for {
		resp, err := http.Get(base + "/")
		if err == nil {
			resp.Body.Close()
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("development server did not start")
		}
		time.Sleep(10 * time.Millisecond)
	}

	env := testGitEnv(t)
	work := t.TempDir()
	mustRunGit(t, work, env, "init", "-b", "main")
	if err := os.WriteFile(filepath.Join(work, "README.md"), []byte("hooks\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mustRunGit(t, work, env, "add", ".")
	mustRunGit(t, work, env, "commit", "-m", "initial")

	mustRunGit(t, work, env, "push", base+"/acme/widget.git", "main")

	waitForDeliveries(t, &mu, &deliveries, 1, 5*time.Second)

	mu.Lock()
	first := deliveries[0]
	mu.Unlock()

	if got := first.header.Get("X-GitHub-Event"); got != "push" {
		t.Errorf("X-GitHub-Event = %q, want push", got)
	}
	mac := hmac.New(sha256.New, []byte("acceptance-secret"))
	mac.Write(first.body)
	wantSig := "sha256=" + hex.EncodeToString(mac.Sum(nil))
	if got := first.header.Get("X-Hub-Signature-256"); got != wantSig {
		t.Errorf("X-Hub-Signature-256 = %q, want %q", got, wantSig)
	}
	if got := first.header.Get("X-GitHub-Delivery"); len(got) != 36 {
		t.Errorf("X-GitHub-Delivery = %q", got)
	}

	var payload map[string]any
	if err := json.Unmarshal(first.body, &payload); err != nil {
		t.Fatalf("payload: %v", err)
	}
	if payload["ref"] != "refs/heads/main" {
		t.Errorf("ref = %v", payload["ref"])
	}
	if payload["created"] != true {
		t.Errorf("created = %v, want true for a first push", payload["created"])
	}
	repo, ok := payload["repository"].(map[string]any)
	if !ok {
		t.Fatalf("repository missing from payload: %s", first.body)
	}
	if repo["full_name"] != "acme/widget" || repo["clone_url"] != base+"/acme/widget.git" {
		t.Errorf("repository = %v", repo)
	}
	pusher, ok := payload["pusher"].(map[string]any)
	if !ok || pusher["name"] == "" {
		t.Errorf("pusher = %v", payload["pusher"])
	}

	// A rejected push moves no refs and must produce no delivery.
	repoRoot := cfg.RepoRoot
	if repoRoot == "" {
		t.Fatal("cfg.RepoRoot was not populated by the dev server")
	}
	repoDir := filepath.Join(repoRoot, "acme", "widget.git")
	if err := os.WriteFile(filepath.Join(repoDir, "hooks", "pre-receive"), []byte("#!/bin/sh\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(work, "README.md"), []byte("change\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mustRunGit(t, work, env, "commit", "-am", "second")
	cmd := exec.Command("git", "push", base+"/acme/widget.git", "main")
	cmd.Dir = work
	cmd.Env = env
	if out, err := cmd.CombinedOutput(); err == nil {
		t.Fatalf("expected push to be rejected by pre-receive, got success:\n%s", out)
	}
	time.Sleep(500 * time.Millisecond)
	mu.Lock()
	got := len(deliveries)
	mu.Unlock()
	if got != 1 {
		t.Fatalf("deliveries after rejected push = %d, want 1 (no new delivery)", got)
	}

	// A hook without secretEnv delivers unsigned; the policy is re-read per push.
	currentHooks.Store([]hooks.Hook{{Repos: []string{"*/*"}, URL: receiver.URL}})
	if err := os.Remove(filepath.Join(repoDir, "hooks", "pre-receive")); err != nil {
		t.Fatal(err)
	}
	mustRunGit(t, work, env, "commit", "--allow-empty", "-m", "third")
	mustRunGit(t, work, env, "push", base+"/acme/widget.git", "main")
	waitForDeliveries(t, &mu, &deliveries, 2, 5*time.Second)

	mu.Lock()
	second := deliveries[1]
	mu.Unlock()
	if sig := second.header.Get("X-Hub-Signature-256"); sig != "" {
		t.Errorf("unsigned delivery carried a signature: %q", sig)
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("development serve: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("development serve did not stop")
	}
}

func waitForDeliveries(t *testing.T, mu *sync.Mutex, deliveries *[]capturedDelivery, want int, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		mu.Lock()
		n := len(*deliveries)
		mu.Unlock()
		if n >= want {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	mu.Lock()
	defer mu.Unlock()
	t.Fatalf("timed out waiting for %d deliveries, got %d", want, len(*deliveries))
}

func TestTailnetHTTPClientUsesTsnetAndRefusesRedirects(t *testing.T) {
	n := &tailnet{server: &tsnet.Server{Hostname: "tsgit-webhook-test", Dir: t.TempDir()}}
	client := n.HTTPClient()

	tr, ok := client.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("transport is %T, want *http.Transport", client.Transport)
	}
	if tr.DialContext == nil {
		t.Fatal("transport has no DialContext")
	}
	// The tsnet transport must not be the host's default transport.
	if tr == http.DefaultTransport {
		t.Fatal("webhook client would use the host network")
	}
	req, err := http.NewRequest(http.MethodGet, "http://example.test/", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := client.CheckRedirect(req, nil); err != http.ErrUseLastResponse {
		t.Fatalf("CheckRedirect = %v, want http.ErrUseLastResponse", err)
	}
}

func TestCloneBaseURL(t *testing.T) {
	cases := []struct {
		scheme string
		host   string
		port   int
		want   string
	}{
		{"https", "tsgit.example.ts.net", 443, "https://tsgit.example.ts.net"},
		{"https", "tsgit.example.ts.net", 8443, "https://tsgit.example.ts.net:8443"},
		{"http", "tsgit", 80, "http://tsgit"},
		{"http", "tsgit", 8080, "http://tsgit:8080"},
	}
	for _, tc := range cases {
		if got := cloneBaseURL(tc.scheme, tc.host, tc.port); got != tc.want {
			t.Errorf("cloneBaseURL(%q, %q, %d) = %q, want %q", tc.scheme, tc.host, tc.port, got, tc.want)
		}
	}
}
