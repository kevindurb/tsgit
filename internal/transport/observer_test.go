package transport

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kevindurb/tsgit/internal/repos"
)

func pushGitEnv(t *testing.T) []string {
	t.Helper()
	home := t.TempDir()
	when := time.Now().UTC().Format(time.RFC3339)
	drop := map[string]bool{
		"HOME": true, "XDG_CONFIG_HOME": true, "GIT_CONFIG_NOSYSTEM": true,
		"GIT_TERMINAL_PROMPT": true, "GIT_DIR": true, "GIT_WORK_TREE": true,
		"GIT_INDEX_FILE": true,
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
		"GIT_AUTHOR_DATE="+when,
		"GIT_COMMITTER_NAME=Test",
		"GIT_COMMITTER_EMAIL=test@example.com",
		"GIT_COMMITTER_DATE="+when,
	)
}

func gitOut(t *testing.T, env []string, input string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Env = env
	cmd.Stdin = strings.NewReader(input)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, stderr.String())
	}
	return strings.TrimSpace(string(out))
}

// simulatePush moves refs in the bare repository like a receive-pack would.
func simulatePush(t *testing.T, repoPath, branch string) {
	t.Helper()
	env := pushGitEnv(t)
	blob := gitOut(t, env, "content\n", "--git-dir", repoPath, "hash-object", "-w", "--stdin")
	tree := gitOut(t, env, "100644 blob "+blob+"\tfile.txt\n", "--git-dir", repoPath, "mktree")
	commit := gitOut(t, env, "", "--git-dir", repoPath, "commit-tree", tree, "-m", "subject")
	gitOut(t, env, "", "--git-dir", repoPath, "update-ref", "refs/heads/"+branch, commit)
}

func TestPushObserverReceivesRefChanges(t *testing.T) {
	backend := &fakeBackend{fn: func(_ context.Context, req Request) error {
		simulatePush(t, req.RepoPath, "main")
		_, err := io.WriteString(req.Out, "ok\n")
		return err
	}}
	handler, store := testHandler(t, writerIdentity(), backend)
	if _, err := store.Create(context.Background(), "acme/widget"); err != nil {
		t.Fatal(err)
	}

	var mu sync.Mutex
	var events []PushEvent
	handler.PushObserver = func(_ context.Context, ev PushEvent) {
		mu.Lock()
		events = append(events, ev)
		mu.Unlock()
	}

	rec := request(handler, "POST", "/acme/widget.git/git-receive-pack", strings.NewReader("pack"))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body %q", rec.Code, rec.Body.String())
	}
	mu.Lock()
	defer mu.Unlock()
	if len(events) != 1 {
		t.Fatalf("observer calls = %d, want 1", len(events))
	}
	ev := events[0]
	if ev.Repo != "acme/widget" {
		t.Errorf("Repo = %q", ev.Repo)
	}
	if ev.Identity.Login != "" && ev.Identity.Node == "" {
		t.Errorf("identity not propagated: %+v", ev.Identity)
	}
	if len(ev.Changes) != 1 {
		t.Fatalf("changes = %+v, want 1", ev.Changes)
	}
	change := ev.Changes[0]
	if change.Ref != "refs/heads/main" || change.Kind != repos.RefCreated {
		t.Errorf("change = %+v, want created refs/heads/main", change)
	}
	if change.Old == change.New {
		t.Errorf("before and after object IDs are equal: %q", change.New)
	}
}

func TestPushObserverNotCalledWithoutRefChanges(t *testing.T) {
	backend := &fakeBackend{} // does nothing to the repo
	handler, store := testHandler(t, writerIdentity(), backend)
	if _, err := store.Create(context.Background(), "acme/widget"); err != nil {
		t.Fatal(err)
	}
	called := false
	handler.PushObserver = func(context.Context, PushEvent) { called = true }

	req := request(handler, "POST", "/acme/widget.git/git-receive-pack", strings.NewReader("pack"))
	if req.Code != http.StatusOK {
		t.Fatalf("status = %d", req.Code)
	}
	if called {
		t.Fatal("observer called although no ref moved")
	}
}

func TestPushObserverNeverCalledForReads(t *testing.T) {
	backend := &fakeBackend{}
	handler, store := testHandler(t, writerIdentity(), backend)
	if _, err := store.Create(context.Background(), "acme/widget"); err != nil {
		t.Fatal(err)
	}
	called := false
	handler.PushObserver = func(context.Context, PushEvent) { called = true }

	request(handler, "POST", "/acme/widget.git/git-upload-pack", strings.NewReader("want"))
	request(handler, "GET", "/acme/widget.git/info/refs?service=git-receive-pack", nil)
	if called {
		t.Fatal("observer called for a read or an advertisement")
	}
}

func TestPushObserverSkippedWhenBackendFailsBeforeCreatingBackendRefs(t *testing.T) {
	backend := &fakeBackend{fn: func(context.Context, Request) error {
		return context.DeadlineExceeded
	}}
	handler, _ := testHandler(t, writerIdentity(), backend)
	called := false
	handler.PushObserver = func(context.Context, PushEvent) { called = true }

	req := request(handler, "POST", "/acme/widget.git/git-receive-pack", strings.NewReader("pack"))
	if req.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", req.Code)
	}
	if called {
		t.Fatal("observer called without any ref change")
	}
}
