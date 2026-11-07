package repos

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestParseName(t *testing.T) {
	valid := map[string]string{
		"acme/widget":         "acme/widget",
		"acme/widget.git":     "acme/widget",
		"Acme/Widget":         "Acme/Widget",
		"a/b":                 "a/b",
		"acme/wid.get":        "acme/wid.get",
		"acme/widget.git.git": "acme/widget.git",
	}
	for in, want := range valid {
		got, err := ParseName(in)
		if err != nil {
			t.Errorf("ParseName(%q): %v", in, err)
			continue
		}
		if got != want {
			t.Errorf("ParseName(%q) = %q, want %q", in, got, want)
		}
	}

	invalid := []string{
		"",
		"/acme/widget",
		"acme",
		"acme/widget/extra",
		"acme/",
		"/",
		"acme/../widget",
		"acme/..",
		"acme/.",
		".hidden/widget",
		"acme/.hidden",
		"acme/.git",
		"acme/..git",
		`acme\widget`,
		"acme/wi\x00dget",
		"acme/wi\x1fdget",
	}
	for _, in := range invalid {
		if got, err := ParseName(in); err == nil {
			t.Errorf("ParseName(%q) = %q, want error", in, got)
		}
	}
}

func TestResolveStaysInsideRoot(t *testing.T) {
	store, err := Open(t.TempDir(), "main")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	got, err := store.Resolve("acme/widget")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	want := filepath.Join(store.Root(), "acme", "widget.git")
	if got != want {
		t.Fatalf("Resolve = %q, want %q", got, want)
	}
}

func TestResolveRejectsSymlinkEscape(t *testing.T) {
	root := t.TempDir()
	store, err := Open(root, "main")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(store.Root(), "acme")); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	if _, err := store.Resolve("acme/widget"); !errors.Is(err, ErrOutsideRoot) {
		t.Fatalf("Resolve through namespace symlink = %v, want ErrOutsideRoot", err)
	}

	if err := os.Remove(filepath.Join(store.Root(), "acme")); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(store.Root(), "acme"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(store.Root(), "acme", "widget.git")); err != nil {
		t.Fatalf("symlink repo: %v", err)
	}
	if _, err := store.Resolve("acme/widget"); !errors.Is(err, ErrOutsideRoot) {
		t.Fatalf("Resolve through repository symlink = %v, want ErrOutsideRoot", err)
	}
}

func TestCreateLayoutAndBranch(t *testing.T) {
	store, err := Open(t.TempDir(), "trunk")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	path, err := store.Create(context.Background(), "acme/widget")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if filepath.Base(path) != "widget.git" {
		t.Fatalf("path = %q", path)
	}
	if !store.Exists("acme/widget") {
		t.Fatal("created repository does not exist")
	}
	head, err := os.ReadFile(filepath.Join(path, "HEAD"))
	if err != nil {
		t.Fatalf("read HEAD: %v", err)
	}
	if strings.TrimSpace(string(head)) != "ref: refs/heads/trunk" {
		t.Fatalf("HEAD = %q", head)
	}
	for _, entry := range []string{"objects", "refs"} {
		if info, err := os.Stat(filepath.Join(path, entry)); err != nil || !info.IsDir() {
			t.Fatalf("%s missing from bare repository: %v", entry, err)
		}
	}
	if info, err := os.Stat(filepath.Join(store.Root(), ".tsgit-tmp")); err != nil || !info.IsDir() {
		t.Fatalf(".tsgit-tmp missing: %v", err)
	}
}

func TestCreateExistingReturnsSamePath(t *testing.T) {
	store, err := Open(t.TempDir(), "main")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	first, err := store.Create(context.Background(), "acme/widget")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	second, err := store.Create(context.Background(), "acme/widget")
	if err != nil {
		t.Fatalf("Create again: %v", err)
	}
	if first != second {
		t.Fatalf("second create = %q, want %q", second, first)
	}
}

func TestCreateConcurrent(t *testing.T) {
	store, err := Open(t.TempDir(), "main")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	const workers = 8
	var wg sync.WaitGroup
	paths := make([]string, workers)
	errs := make([]error, workers)
	for i := range workers {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			paths[i], errs[i] = store.Create(context.Background(), "acme/widget")
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("worker %d: %v", i, err)
		}
	}
	for i := 1; i < workers; i++ {
		if paths[i] != paths[0] {
			t.Fatalf("worker %d returned %q, want %q", i, paths[i], paths[0])
		}
	}
	entries, err := os.ReadDir(filepath.Join(store.Root(), "acme"))
	if err != nil {
		t.Fatalf("read namespace: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("namespace has %d entries, want exactly one repository", len(entries))
	}
	staged, err := os.ReadDir(filepath.Join(store.Root(), ".tsgit-tmp"))
	if err != nil {
		t.Fatalf("read staging: %v", err)
	}
	if len(staged) != 0 {
		t.Fatalf("staging directory is not clean: %v", staged)
	}
}

func TestOpenCreatesRootAndChecksWritable(t *testing.T) {
	root := filepath.Join(t.TempDir(), "nested", "repos")
	store, err := Open(root, "main")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if info, err := os.Stat(store.Root()); err != nil || !info.IsDir() {
		t.Fatalf("root was not created: %v", err)
	}

	blocker := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(filepath.Join(blocker, "repos"), "main"); err == nil {
		t.Fatal("Open under a regular file must fail")
	}
}

func TestOpenRejectsReadOnlyRoot(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root can write to a read-only directory")
	}
	root := t.TempDir()
	if err := os.Chmod(root, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(root, 0o700) })
	if _, err := Open(root, "main"); err == nil {
		t.Fatal("Open on a read-only root must fail")
	}
}

func TestOpenRequiresGit(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	if _, err := Open(t.TempDir(), "main"); err == nil || !strings.Contains(err.Error(), "git") {
		t.Fatalf("expected a missing-git error, got %v", err)
	}
}

// TestDocumentedStorageLayout keeps docs/architecture.md in step with the
// layout Create produces.
func TestDocumentedStorageLayout(t *testing.T) {
	data, err := os.ReadFile("../../docs/architecture.md")
	if err != nil {
		t.Fatalf("read architecture doc: %v", err)
	}
	text := string(data)
	for _, want := range []string{".tsgit-tmp", "widget.git", "HEAD", "objects", "refs"} {
		if !strings.Contains(text, want) {
			t.Errorf("architecture doc does not mention %q", want)
		}
	}
}

func TestCleanDescription(t *testing.T) {
	tests := map[string]string{
		"": "",
		"Unnamed repository; edit this file 'description' to name the repository.\n": "",
		"  widget service\n": "widget service",
	}
	for in, want := range tests {
		if got := cleanDescription(in); got != want {
			t.Errorf("cleanDescription(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestListAndInfo(t *testing.T) {
	store, err := Open(t.TempDir(), "main")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	when := time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC)
	path, hash := writeCommit(t, store, "acme/widget", "main", "initial commit", when)
	if err := os.WriteFile(filepath.Join(path, "description"), []byte("widget service\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, metadataGitEnv(t, when), "--git-dir", path, "update-ref", "refs/tags/v1", hash)
	if _, err := store.Create(context.Background(), "acme/empty"); err != nil {
		t.Fatal(err)
	}

	infos, err := store.List(context.Background())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	sort.Slice(infos, func(i, j int) bool { return infos[i].Name < infos[j].Name })
	if len(infos) != 2 {
		t.Fatalf("List returned %d repositories, want 2", len(infos))
	}

	empty := infos[0]
	if empty.Name != "acme/empty" || !empty.Empty || empty.Latest.Hash != "" {
		t.Fatalf("empty info = %+v", empty)
	}

	widget := infos[1]
	if widget.Name != "acme/widget" {
		t.Fatalf("widget name = %q", widget.Name)
	}
	if widget.Description != "widget service" {
		t.Fatalf("description = %q", widget.Description)
	}
	if widget.Branch != "main" || widget.Detached || widget.Empty {
		t.Fatalf("head = %+v", widget)
	}
	if widget.Latest.Subject != "initial commit" || widget.Latest.Hash == "" {
		t.Fatalf("latest = %+v", widget.Latest)
	}
	if widget.Branches != 1 || widget.Tags != 1 {
		t.Fatalf("branches=%d tags=%d, want 1 and 1", widget.Branches, widget.Tags)
	}
	if len(widget.Refs) != 2 {
		t.Fatalf("refs = %v, want a branch and a tag", widget.Refs)
	}
	if widget.Err != nil {
		t.Fatalf("unexpected metadata error: %v", widget.Err)
	}

	info, err := store.Info(context.Background(), "acme/widget")
	if err != nil {
		t.Fatalf("Info: %v", err)
	}
	if info.Name != widget.Name || info.Latest.Subject != widget.Latest.Subject {
		t.Fatalf("Info = %+v, want %+v", info, widget)
	}

	if _, err := store.Info(context.Background(), "acme/missing"); !errors.Is(err, ErrNotExist) {
		t.Fatalf("Info missing = %v, want ErrNotExist", err)
	}
}

func TestInfoUnbornHeadWithOtherBranch(t *testing.T) {
	store, err := Open(t.TempDir(), "main")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	when := time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC)
	writeCommit(t, store, "acme/widget", "master", "on master", when)

	info, err := store.Info(context.Background(), "acme/widget")
	if err != nil {
		t.Fatalf("Info: %v", err)
	}
	if info.Branch != "main" {
		t.Fatalf("branch = %q, want main", info.Branch)
	}
	if !info.Empty {
		t.Fatal("repository with an unborn default branch is not marked empty")
	}
	if info.Branches != 1 || len(info.Refs) != 1 || info.Refs[0] != "master" {
		t.Fatalf("branches=%d refs=%v, want master", info.Branches, info.Refs)
	}
	if info.Latest.Subject != "on master" {
		t.Fatalf("latest subject = %q", info.Latest.Subject)
	}
}

func TestListSkipsSymlinkEscape(t *testing.T) {
	root := t.TempDir()
	store, err := Open(root, "main")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	outside := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "acme"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "acme", "evil.git")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "linked")); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Create(context.Background(), "acme/ok"); err != nil {
		t.Fatal(err)
	}

	infos, err := store.List(context.Background())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(infos) != 1 || infos[0].Name != "acme/ok" {
		t.Fatalf("List = %+v, want only acme/ok", infos)
	}
}

func TestListDegradesOnBrokenRepo(t *testing.T) {
	root := t.TempDir()
	store, err := Open(root, "main")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, "acme", "broken.git"), 0o750); err != nil {
		t.Fatal(err)
	}

	infos, err := store.List(context.Background())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(infos) != 1 || infos[0].Name != "acme/broken" {
		t.Fatalf("List = %+v, want one broken entry", infos)
	}
	if infos[0].Err == nil {
		t.Fatal("broken repository did not report a metadata error")
	}
}

func TestInfoHonorsCanceledContext(t *testing.T) {
	store, err := Open(t.TempDir(), "main")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	when := time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC)
	writeCommit(t, store, "acme/widget", "main", "initial", when)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	info, err := store.Info(ctx, "acme/widget")
	if err == nil {
		t.Fatal("Info with a canceled context returned no error")
	}
	if info.Name != "acme/widget" {
		t.Fatalf("Info name = %q, want acme/widget", info.Name)
	}
}

func TestInfoNormalizesInvalidUTF8(t *testing.T) {
	store, err := Open(t.TempDir(), "main")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	when := time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC)
	path, _ := writeCommit(t, store, "acme/widget", "main", "initial", when)
	if err := os.WriteFile(filepath.Join(path, "description"), []byte("bad\xffdescription"), 0o644); err != nil {
		t.Fatal(err)
	}

	info, err := store.Info(context.Background(), "acme/widget")
	if err != nil {
		t.Fatalf("Info: %v", err)
	}
	if !strings.Contains(info.Description, "\uFFFD") {
		t.Fatalf("description = %q, want a replacement character", info.Description)
	}
}

func metadataGitEnv(t *testing.T, when time.Time) []string {
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

func writeCommit(t *testing.T, store *Store, name, branch, subject string, when time.Time) (string, string) {
	t.Helper()
	path, err := store.Create(context.Background(), name)
	if err != nil {
		t.Fatalf("Create %s: %v", name, err)
	}
	env := metadataGitEnv(t, when)
	blob := gitOutput(t, env, "hello\n", "--git-dir", path, "hash-object", "-w", "--stdin")
	tree := gitOutput(t, env, "100644 blob "+blob+"\tfile.txt\n", "--git-dir", path, "mktree")
	commit := gitOutput(t, env, "", "--git-dir", path, "commit-tree", tree, "-m", subject)
	runGit(t, env, "--git-dir", path, "update-ref", "refs/heads/"+branch, commit)
	return path, commit
}

func gitOutput(t *testing.T, env []string, input string, args ...string) string {
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

func runGit(t *testing.T, env []string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Env = env
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, stderr.String())
	}
}
