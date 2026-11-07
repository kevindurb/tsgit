package repos

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
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
