package transport

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/kevindurb/tsgit/internal/authz"
	"github.com/kevindurb/tsgit/internal/identity"
	"github.com/kevindurb/tsgit/internal/repos"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func newGitServer(t *testing.T, id authz.Identity) (*httptest.Server, *repos.Store) {
	t.Helper()
	store, err := repos.Open(t.TempDir(), "main")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	handler := NewHandler(identity.Fixed{Identity: id}, store, &GitBin{Git: store.GitPath(), Log: discardLogger()}, discardLogger())
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return srv, store
}

func gitEnv(t *testing.T) []string {
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
		"XDG_CONFIG_HOME="+filepath.Join(home, ".config"),
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_TERMINAL_PROMPT=0",
		"GIT_AUTHOR_NAME=Test",
		"GIT_AUTHOR_EMAIL=test@example.com",
		"GIT_COMMITTER_NAME=Test",
		"GIT_COMMITTER_EMAIL=test@example.com",
	)
}

func runGit(t *testing.T, dir string, env []string, args ...string) (string, error) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func mustGit(t *testing.T, dir string, env []string, args ...string) string {
	t.Helper()
	out, err := runGit(t, dir, env, args...)
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return out
}

func initRepoWithCommit(t *testing.T, env []string) string {
	t.Helper()
	dir := t.TempDir()
	mustGit(t, dir, env, "init", "-b", "main")
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mustGit(t, dir, env, "add", ".")
	mustGit(t, dir, env, "commit", "-m", "initial")
	return dir
}

func TestPushCloneAndFetch(t *testing.T) {
	srv, store := newGitServer(t, writerIdentity())
	env := gitEnv(t)
	repo := initRepoWithCommit(t, env)
	url := srv.URL + "/acme/widget.git"

	mustGit(t, repo, env, "push", url, "main")
	if !store.Exists("acme/widget") {
		t.Fatal("push did not create acme/widget")
	}

	cloneParent := t.TempDir()
	mustGit(t, cloneParent, env, "clone", url, "clone")
	got, err := os.ReadFile(filepath.Join(cloneParent, "clone", "README.md"))
	if err != nil || string(got) != "hello\n" {
		t.Fatalf("clone contents = %q, %v", got, err)
	}

	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("hello again\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mustGit(t, repo, env, "commit", "-am", "second")
	mustGit(t, repo, env, "push", url, "main")
	want := strings.TrimSpace(mustGit(t, repo, env, "rev-parse", "HEAD"))
	mustGit(t, cloneParent, env, "-C", "clone", "fetch", "origin")
	gotHead := strings.TrimSpace(mustGit(t, cloneParent, env, "-C", "clone", "rev-parse", "origin/main"))
	if gotHead != want {
		t.Fatalf("origin/main = %s, want %s", gotHead, want)
	}
}

func TestPushToCreateIsAtomic(t *testing.T) {
	srv, store := newGitServer(t, writerIdentity())
	env := gitEnv(t)
	repo := initRepoWithCommit(t, env)
	url := srv.URL + "/acme/new.git"

	mustGit(t, repo, env, "push", url, "main")
	path, err := store.Resolve("acme/new")
	if err != nil {
		t.Fatal(err)
	}
	head := strings.TrimSpace(mustGit(t, t.TempDir(), env, "--git-dir", path, "rev-parse", "refs/heads/main"))
	if head == "" {
		t.Fatal("pushed ref is missing from the created repository")
	}
}

func TestConcurrentFirstPushes(t *testing.T) {
	srv, store := newGitServer(t, writerIdentity())
	env := gitEnv(t)
	url := srv.URL + "/acme/race.git"
	alice := initRepoWithCommit(t, env)
	bob := initRepoWithCommit(t, env)

	var wg sync.WaitGroup
	errs := make([]error, 2)
	wg.Add(2)
	go func() {
		defer wg.Done()
		_, errs[0] = runGit(t, alice, env, "push", url, "HEAD:refs/heads/alice")
	}()
	go func() {
		defer wg.Done()
		_, errs[1] = runGit(t, bob, env, "push", url, "HEAD:refs/heads/bob")
	}()
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("push %d failed: %v", i, err)
		}
	}
	if !store.Exists("acme/race") {
		t.Fatal("concurrent pushes did not create the repository")
	}
	path, err := store.Resolve("acme/race")
	if err != nil {
		t.Fatal(err)
	}
	refs := mustGit(t, t.TempDir(), env, "--git-dir", path, "for-each-ref", "--format=%(refname)")
	for _, want := range []string{"refs/heads/alice", "refs/heads/bob"} {
		if !strings.Contains(refs, want) {
			t.Fatalf("refs %q do not contain %s", refs, want)
		}
	}
}

type protocolRecorder struct {
	inner Backend
	mu    sync.Mutex
	seen  []string
}

func (p *protocolRecorder) Serve(ctx context.Context, req Request) error {
	p.mu.Lock()
	p.seen = append(p.seen, req.GitProtocol)
	p.mu.Unlock()
	return p.inner.Serve(ctx, req)
}

func (p *protocolRecorder) sawVersion2() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, v := range p.seen {
		if v == "version=2" {
			return true
		}
	}
	return false
}

func TestProtocolV2Clone(t *testing.T) {
	store, err := repos.Open(t.TempDir(), "main")
	if err != nil {
		t.Fatal(err)
	}
	backend := &protocolRecorder{inner: &GitBin{Git: store.GitPath(), Log: discardLogger()}}
	handler := NewHandler(identity.Fixed{Identity: writerIdentity()}, store, backend, discardLogger())
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	env := gitEnv(t)
	repo := initRepoWithCommit(t, env)
	url := srv.URL + "/acme/widget.git"
	mustGit(t, repo, env, "push", url, "main")

	cloneParent := t.TempDir()
	mustGit(t, cloneParent, env, "-c", "protocol.version=2", "clone", url, "clone")
	if !backend.sawVersion2() {
		t.Fatalf("server never saw Git-Protocol version=2; saw %v", backend.seen)
	}
	if _, err := os.Stat(filepath.Join(cloneParent, "clone", "README.md")); err != nil {
		t.Fatalf("v2 clone is missing the checkout: %v", err)
	}
}

func TestTraversalIsRefused(t *testing.T) {
	srv, store := newGitServer(t, writerIdentity())
	targets := []string{
		srv.URL + "/%2e%2e/evil.git/info/refs?service=git-upload-pack",
		srv.URL + "/acme/%2e%2e/%2e%2e/evil.git/info/refs?service=git-upload-pack",
	}
	for _, target := range targets {
		resp, err := http.Get(target)
		if err != nil {
			t.Fatalf("GET %s: %v", target, err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("GET %s = %d, want 404", target, resp.StatusCode)
		}
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(store.Root()), "evil.git")); !os.IsNotExist(err) {
		t.Fatalf("traversal touched a path outside the root: %v", err)
	}
}

func TestReadMissingRepositoryDoesNotCreate(t *testing.T) {
	srv, store := newGitServer(t, readerIdentity())
	env := gitEnv(t)
	parent := t.TempDir()
	out, err := runGit(t, parent, env, "clone", srv.URL+"/acme/missing.git", "clone")
	if err == nil {
		t.Fatalf("clone of a missing repository succeeded:\n%s", out)
	}
	if store.Exists("acme/missing") {
		t.Fatal("a read request created a repository")
	}
}
