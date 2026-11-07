package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kevindurb/tsgit/internal/config"
)

func TestVersionCommand(t *testing.T) {
	for _, args := range [][]string{{"version"}, {"--version"}} {
		var out, errOut bytes.Buffer
		if code := run(args, &out, &errOut); code != 0 {
			t.Fatalf("run(%v) = %d, stderr %s", args, code, errOut.String())
		}
		if strings.TrimSpace(out.String()) != version {
			t.Fatalf("run(%v) printed %q, want %q", args, out.String(), version)
		}
	}
}

func TestUnknownCommandExitsNonZero(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := run([]string{"frobnicate"}, &out, &errOut); code == 0 {
		t.Fatal("unknown command exited zero")
	}
	if !strings.Contains(errOut.String(), "unknown command") {
		t.Fatalf("stderr = %q", errOut.String())
	}
}

func TestServeConfigurationError(t *testing.T) {
	t.Setenv("TSGIT_REPO_ROOT", "")
	t.Setenv("TSGIT_DEV_LISTEN", "")
	var out, errOut bytes.Buffer
	code := run([]string{"serve"}, &out, &errOut)
	if code != 2 {
		t.Fatalf("code = %d, want 2; stderr %s", code, errOut.String())
	}
	if !strings.Contains(errOut.String(), "--repo-root") {
		t.Fatalf("stderr = %q, want the required repo root error", errOut.String())
	}
}

func TestServeVersionFlag(t *testing.T) {
	var out, errOut bytes.Buffer
	code := run([]string{"serve", "--version"}, &out, &errOut)
	if code != 0 || strings.TrimSpace(out.String()) != version {
		t.Fatalf("code = %d, out = %q, err = %q", code, out.String(), errOut.String())
	}
}

func TestDevModeDoesNotStartTsnet(t *testing.T) {
	original := startTailnet
	t.Cleanup(func() { startTailnet = original })
	called := false
	startTailnet = func(context.Context, *config.Config, *slog.Logger) (*tailnet, error) {
		called = true
		return nil, errors.New("tsnet must not start in development mode")
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	cfg := &config.Config{DevListen: "127.0.0.1:0", DefaultBranch: "main", Log: "info"}
	if err := serve(ctx, cfg, io.Discard); err != nil {
		t.Fatalf("development serve: %v", err)
	}
	if called {
		t.Fatal("development mode started tsnet")
	}
}

func TestHTTPSEnabled(t *testing.T) {
	tests := []struct {
		explicit bool
		hasCap   bool
		domains  []string
		want     bool
	}{
		{true, true, []string{"tsgit.tail.ts.net"}, true},
		{false, true, []string{"tsgit.tail.ts.net"}, false},
		{true, false, []string{"tsgit.tail.ts.net"}, false},
		{true, true, nil, false},
		{true, true, []string{}, false},
	}
	for _, tt := range tests {
		if got := httpsEnabled(tt.explicit, tt.hasCap, tt.domains); got != tt.want {
			t.Errorf("httpsEnabled(%v, %v, %v) = %v, want %v", tt.explicit, tt.hasCap, tt.domains, got, tt.want)
		}
	}
}

func TestRedirectHandler(t *testing.T) {
	tests := []struct {
		host string
		port int
		want string
	}{
		{"tsgit.tail.ts.net", 443, "https://tsgit.tail.ts.net/foo?x=1"},
		{"tsgit.tail.ts.net", 8443, "https://tsgit.tail.ts.net:8443/foo?x=1"},
	}
	for _, tt := range tests {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "http://tsgit.tail.ts.net/foo?x=1", nil)
		redirectHandler(tt.host, tt.port).ServeHTTP(rec, req)
		if rec.Code != http.StatusPermanentRedirect {
			t.Errorf("status = %d", rec.Code)
		}
		if got := rec.Header().Get("Location"); got != tt.want {
			t.Errorf("Location = %q, want %q", got, tt.want)
		}
	}
}

func TestHSTSHandler(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	hstsHandler(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "ok")
	})).ServeHTTP(rec, req)
	if got := rec.Header().Get("Strict-Transport-Security"); got == "" {
		t.Fatal("missing Strict-Transport-Security header")
	}
}

func TestFrontDispatches(t *testing.T) {
	git := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "git")
	})
	ui := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "ui")
	})
	handler := front(git, ui)

	tests := []struct {
		method string
		target string
		want   string
	}{
		{http.MethodGet, "/acme/widget.git/info/refs?service=git-upload-pack", "git"},
		{http.MethodPost, "/acme/widget.git/git-upload-pack", "git"},
		{http.MethodGet, "/", "ui"},
		{http.MethodGet, "/acme/widget", "ui"},
		{http.MethodGet, "/.tsgit/web.css", "ui"},
		{http.MethodGet, "/acme/widget.git/info/refs", "ui"},
	}
	for _, tt := range tests {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(tt.method, tt.target, nil)
		handler.ServeHTTP(rec, req)
		if got := rec.Body.String(); got != tt.want {
			t.Errorf("%s %s = %q, want %q", tt.method, tt.target, got, tt.want)
		}
	}
}

func TestGracefulShutdownDrainsInflightRequest(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	started := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		once.Do(func() { close(started) })
		<-release
		_, _ = io.WriteString(w, "ok")
	})}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	go func() { done <- runHTTP(ctx, srv, ln, logger) }()

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
		t.Fatal("request never reached the handler")
	}
	cancel()
	close(release)

	select {
	case body := <-bodyCh:
		if body != "ok" {
			t.Fatalf("in-flight response = %q", body)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("in-flight request was not drained")
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("runHTTP: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("runHTTP did not return after shutdown")
	}
}

func TestDevelopmentModeServesCloneAndPush(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
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
	done := make(chan error, 1)
	cfg := &config.Config{DevListen: "test", DefaultBranch: "main", Log: "error"}
	go func() { done <- serve(ctx, cfg, io.Discard) }()

	base := "http://" + ln.Addr().String()
	deadline := time.Now().Add(10 * time.Second)
	for {
		resp, err := http.Get(base + "/")
		if err == nil {
			resp.Body.Close()
			break
		}
		if time.Now().After(deadline) {
			cancel()
			t.Fatalf("development server did not start: %v", err)
		}
		time.Sleep(20 * time.Millisecond)
	}

	env := testGitEnv(t)
	repo := t.TempDir()
	mustRunGit(t, repo, env, "init", "-b", "main")
	if err := os.WriteFile(repo+"/README.md", []byte("dev mode\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mustRunGit(t, repo, env, "add", ".")
	mustRunGit(t, repo, env, "commit", "-m", "initial")
	mustRunGit(t, repo, env, "push", base+"/acme/widget.git", "main")

	cloneParent := t.TempDir()
	mustRunGit(t, cloneParent, env, "clone", base+"/acme/widget.git", "clone")
	got, err := os.ReadFile(cloneParent + "/clone/README.md")
	if err != nil || string(got) != "dev mode\n" {
		t.Fatalf("clone contents = %q, %v", got, err)
	}

	for _, tt := range []struct {
		path string
		want []string
	}{
		{"/", []string{"acme/widget", base + "/acme/widget.git"}},
		{"/acme/widget", []string{"Default branch", "main", base + "/acme/widget.git"}},
		{"/.tsgit/web.css", []string{"prefers-color-scheme: dark"}},
	} {
		resp, err := http.Get(base + tt.path)
		if err != nil {
			cancel()
			t.Fatalf("GET %s: %v", tt.path, err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			cancel()
			t.Fatalf("GET %s = %d", tt.path, resp.StatusCode)
		}
		for _, want := range tt.want {
			if !strings.Contains(string(body), want) {
				cancel()
				t.Fatalf("GET %s does not contain %q:\n%s", tt.path, want, body)
			}
		}
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

func testGitEnv(t *testing.T) []string {
	t.Helper()
	home := t.TempDir()
	drop := map[string]bool{
		"HOME":                true,
		"XDG_CONFIG_HOME":     true,
		"GIT_CONFIG_NOSYSTEM": true,
		"GIT_TERMINAL_PROMPT": true,
		"GIT_DIR":             true,
		"GIT_WORK_TREE":       true,
		"GIT_INDEX_FILE":      true,
		"GIT_AUTHOR_NAME":     true,
		"GIT_AUTHOR_EMAIL":    true,
		"GIT_COMMITTER_NAME":  true,
		"GIT_COMMITTER_EMAIL": true,
	}
	var env []string
	for _, kv := range os.Environ() {
		key, _, _ := strings.Cut(kv, "=")
		if !drop[key] {
			env = append(env, kv)
		}
	}
	return append(env,
		"HOME="+home,
		"XDG_CONFIG_HOME="+home+"/.config",
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_TERMINAL_PROMPT=0",
		"GIT_AUTHOR_NAME=Test",
		"GIT_AUTHOR_EMAIL=test@example.com",
		"GIT_COMMITTER_NAME=Test",
		"GIT_COMMITTER_EMAIL=test@example.com",
	)
}

func mustRunGit(t *testing.T, dir string, env []string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}
