package web

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/kevindurb/tsgit/internal/repos"
)

// TestBrowsingIntegration exercises the real git-backed read layer through the
// HTTP handler with branches, an annotated tag, a commit reachable only by its
// hash, a README, a binary file, and a symlink.
func TestBrowsingIntegration(t *testing.T) {
	store, err := repos.Open(t.TempDir(), "main")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if _, err := store.Create(context.Background(), "acme/widget"); err != nil {
		t.Fatalf("Create: %v", err)
	}
	path, err := store.Resolve("acme/widget")
	if err != nil {
		t.Fatal(err)
	}
	env := integrationGitEnv(t, time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC))

	blob := func(content string) string {
		return integrationGit(t, env, content, "--git-dir", path, "hash-object", "-w", "--stdin")
	}
	nestedTree := integrationMktree(t, env, path, []string{
		"100644 blob " + blob("nested content\n") + "\tnested.txt",
	})
	mainTree := integrationMktree(t, env, path, []string{
		"100644 blob " + blob("# Main README\n\n[link](nested/nested.txt)\n") + "\tREADME.md",
		"100644 blob " + blob("text\x00binary") + "\tbinary.bin",
		"120000 blob " + blob("nested/nested.txt") + "\tlink.txt",
		"040000 tree " + nestedTree + "\tnested",
	})
	mainHash := integrationGit(t, env, "", "--git-dir", path, "commit-tree", mainTree, "-m", "main commit")

	featureTree := integrationMktree(t, env, path, []string{
		"100644 blob " + blob("# Feature README\n") + "\tREADME.md",
		"100644 blob " + blob("feature\n") + "\tfeature.txt",
	})
	featureHash := integrationGit(t, env, "", "--git-dir", path, "commit-tree", featureTree, "-m", "feature commit")

	detachedTree := integrationMktree(t, env, path, []string{
		"100644 blob " + blob("detached content\n") + "\tdetached.txt",
	})
	detachedHash := integrationGit(t, env, "", "--git-dir", path, "commit-tree", detachedTree, "-m", "detached commit")

	integrationRun(t, env, "--git-dir", path, "update-ref", "refs/heads/main", mainHash)
	integrationRun(t, env, "--git-dir", path, "update-ref", "refs/heads/feature", featureHash)
	integrationRun(t, env, "--git-dir", path, "tag", "-a", "v1", "-m", "version one", featureHash)

	handler := newTestHandler(t, readerIdentity(), store)
	get := func(target string) (int, string) {
		t.Helper()
		rec := request(handler, "GET", target)
		return rec.Code, rec.Body.String()
	}

	code, body := get("/acme/widget")
	if code != 200 || !strings.Contains(body, "Main README") {
		t.Fatalf("summary = %d, missing HEAD README:\n%s", code, body)
	}
	if !strings.Contains(body, `href="/acme/widget/blob/nested/nested.txt"`) {
		t.Fatalf("summary README link is wrong:\n%s", body)
	}

	code, body = get("/acme/widget/tree")
	if code != 200 {
		t.Fatalf("tree = %d", code)
	}
	for _, want := range []string{"README.md", "binary.bin", "link.txt", "nested"} {
		if !strings.Contains(body, want) {
			t.Errorf("tree missing %q:\n%s", want, body)
		}
	}

	code, body = get("/acme/widget/tree/nested")
	if code != 200 || !strings.Contains(body, "nested.txt") {
		t.Fatalf("nested tree = %d:\n%s", code, body)
	}

	code, body = get("/acme/widget/blob/nested/nested.txt")
	if code != 200 || !strings.Contains(body, "nested content") {
		t.Fatalf("nested blob = %d:\n%s", code, body)
	}

	code, body = get("/acme/widget/blob/binary.bin")
	if code != 200 || !strings.Contains(body, "binary and is not displayed") {
		t.Fatalf("binary blob = %d:\n%s", code, body)
	}

	code, body = get("/acme/widget/blob/link.txt")
	if code != 200 || !strings.Contains(body, "Symbolic link to") {
		t.Fatalf("symlink blob = %d:\n%s", code, body)
	}

	code, body = get("/acme/widget/tree?ref=feature")
	if code != 200 || !strings.Contains(body, "Feature README") || !strings.Contains(body, "feature.txt") {
		t.Fatalf("feature tree = %d:\n%s", code, body)
	}

	code, body = get("/acme/widget/tree?ref=v1")
	if code != 200 || !strings.Contains(body, "Feature README") {
		t.Fatalf("tag tree = %d:\n%s", code, body)
	}
	if !strings.Contains(body, "ref=refs%2Ftags%2Fv1") {
		t.Fatalf("tag tree did not preserve the full tag ref:\n%s", body)
	}

	code, body = get("/acme/widget/tree?ref=" + detachedHash)
	if code != 200 || !strings.Contains(body, "detached.txt") {
		t.Fatalf("detached commit tree = %d:\n%s", code, body)
	}

	code, body = get("/acme/widget/blob/detached.txt?ref=" + detachedHash)
	if code != 200 || !strings.Contains(body, "detached content") {
		t.Fatalf("detached commit blob = %d:\n%s", code, body)
	}

	if code, _ := get("/acme/widget/tree?ref=no-such-ref"); code != 404 {
		t.Fatalf("unknown ref = %d, want 404", code)
	}
}

func integrationGit(t *testing.T, env []string, input string, args ...string) string {
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

func integrationRun(t *testing.T, env []string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Env = env
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}

func integrationMktree(t *testing.T, env []string, path string, records []string) string {
	t.Helper()
	cmd := exec.Command("git", "--git-dir", path, "mktree", "-z")
	cmd.Env = env
	cmd.Stdin = strings.NewReader(strings.Join(records, "\x00") + "\x00")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git mktree: %v\n%s", err, stderr.String())
	}
	return strings.TrimSpace(string(out))
}

func integrationGitEnv(t *testing.T, when time.Time) []string {
	t.Helper()
	home := t.TempDir()
	stamp := when.Format(time.RFC3339)
	drop := map[string]bool{
		"HOME": true, "XDG_CONFIG_HOME": true,
		"GIT_CONFIG_NOSYSTEM": true, "GIT_TERMINAL_PROMPT": true,
		"GIT_DIR": true, "GIT_WORK_TREE": true, "GIT_INDEX_FILE": true,
		"GIT_AUTHOR_NAME": true, "GIT_AUTHOR_EMAIL": true, "GIT_AUTHOR_DATE": true,
		"GIT_COMMITTER_NAME": true, "GIT_COMMITTER_EMAIL": true, "GIT_COMMITTER_DATE": true,
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
		"GIT_AUTHOR_DATE="+stamp,
		"GIT_COMMITTER_NAME=Test",
		"GIT_COMMITTER_EMAIL=test@example.com",
		"GIT_COMMITTER_DATE="+stamp,
	)
}
