package repos

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestParseTreePath(t *testing.T) {
	valid := map[string][]string{
		"":                    nil,
		"README.md":           {"README.md"},
		".github/workflows":   {".github", "workflows"},
		"a b/café.txt":        {"a b", "café.txt"},
		"nested/.hidden/file": {"nested", ".hidden", "file"},
	}
	for want, parts := range valid {
		got, err := ParseTreePath(parts)
		if err != nil {
			t.Errorf("ParseTreePath(%q): %v", parts, err)
			continue
		}
		if got != want {
			t.Errorf("ParseTreePath(%q) = %q, want %q", parts, got, want)
		}
	}

	invalid := [][]string{
		{""},
		{"."},
		{".."},
		{"a", ""},
		{"a", "..", "b"},
		{"a\x00b"},
		{"a\x1fb"},
		{"a\tb"},
		{strings.Repeat("a", MaxTreePathBytes+1)},
	}
	for _, parts := range invalid {
		if got, err := ParseTreePath(parts); err == nil {
			t.Errorf("ParseTreePath(%q) = %q, want error", parts, got)
		}
	}

	deep := make([]string, MaxTreeDepth+1)
	for i := range deep {
		deep[i] = "d"
	}
	if _, err := ParseTreePath(deep); err == nil {
		t.Error("over-deep path was accepted")
	}
}

func TestResolveRef(t *testing.T) {
	store, path, env, mainHash, _ := newBrowseRepo(t)

	treeHash := gitOutput(t, env, "", "--git-dir", path, "rev-parse", mainHash+"^{tree}")
	nonCommit := treeHash
	missingHash := strings.Repeat("a", 40)
	hashBranch := "0123456789abcdef0123456789abcdef01234567"
	runGit(t, env, "--git-dir", path, "update-ref", "refs/heads/"+hashBranch, mainHash)
	runGit(t, env, "--git-dir", path, "update-ref", "refs/heads/release", mainHash)
	otherHash := gitOutput(t, env, "", "--git-dir", path, "rev-parse", "refs/heads/other")
	runGit(t, env, "--git-dir", path, "update-ref", "refs/tags/release", otherHash)
	runGit(t, env, "--git-dir", path, "update-ref", "refs/tags/fullref", mainHash)

	tests := []struct {
		label   string
		ref     string
		want    string
		wantRef string
	}{
		{label: "absent is HEAD", ref: "", want: mainHash, wantRef: "HEAD"},
		{label: "branch", ref: "main", want: mainHash, wantRef: "refs/heads/main"},
		{label: "annotated tag peels", ref: "v1", want: mainHash, wantRef: "refs/tags/v1"},
		{label: "full branch", ref: "refs/heads/main", want: mainHash, wantRef: "refs/heads/main"},
		{label: "full hash", ref: mainHash, want: mainHash, wantRef: mainHash},
		{label: "uppercase hash", ref: strings.ToUpper(mainHash), want: mainHash, wantRef: mainHash},
		{label: "hash fallback to branch", ref: hashBranch, want: mainHash, wantRef: "refs/heads/" + hashBranch},
		{label: "branch beats colliding tag", ref: "release", want: mainHash, wantRef: "refs/heads/release"},
		{label: "full tag wins collision", ref: "refs/tags/release", want: otherHash, wantRef: "refs/tags/release"},
	}
	for _, tt := range tests {
		t.Run(tt.label, func(t *testing.T) {
			commit, err := store.ResolveRef(context.Background(), "acme/widget", tt.ref)
			if err != nil {
				t.Fatalf("ResolveRef(%q): %v", tt.ref, err)
			}
			if commit.Hash != tt.want {
				t.Fatalf("hash = %q, want %q", commit.Hash, tt.want)
			}
			if commit.Ref != tt.wantRef {
				t.Fatalf("ref = %q, want %q", commit.Ref, tt.wantRef)
			}
		})
	}

	for name, ref := range map[string]string{
		"unknown":     "no-such-ref",
		"non-commit":  nonCommit,
		"missing hex": missingHash,
		"other refs":  "refs/remotes/origin/main",
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := store.ResolveRef(context.Background(), "acme/widget", ref); !errors.Is(err, ErrNotExist) {
				t.Fatalf("ResolveRef(%q) = %v, want ErrNotExist", ref, err)
			}
		})
	}

	// Detached HEAD.
	if err := os.WriteFile(path+"/HEAD", []byte(mainHash+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	commit, err := store.ResolveRef(context.Background(), "acme/widget", "")
	if err != nil {
		t.Fatalf("detached ResolveRef: %v", err)
	}
	if commit.Hash != mainHash {
		t.Fatalf("detached HEAD = %q, want %q", commit.Hash, mainHash)
	}
}

func TestTreeBlobAndReadme(t *testing.T) {
	store, _, _, mainHash, _ := newBrowseRepo(t)

	entries, err := store.Tree(context.Background(), "acme/widget", mainHash, "")
	if err != nil {
		t.Fatalf("Tree root: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("root listing is empty")
	}
	var names []string
	for i, entry := range entries {
		names = append(names, entry.Name)
		if i > 0 && entries[i-1].Dir == false && entry.Dir {
			t.Fatalf("directories are not listed first: %v", names)
		}
	}
	for _, want := range []string{"README.md", "nested", "link.txt", "submod", "file with space.txt", "café.txt", "line\nbreak.txt", "tab\tname.txt"} {
		if !contains(names, want) {
			t.Errorf("root listing missing %q: %v", want, names)
		}
	}

	nested, err := store.Tree(context.Background(), "acme/widget", mainHash, "nested")
	if err != nil {
		t.Fatalf("Tree nested: %v", err)
	}
	if len(nested) != 1 || nested[0].Name != "inner.txt" {
		t.Fatalf("nested tree = %+v", nested)
	}

	if _, err := store.Tree(context.Background(), "acme/widget", mainHash, "README.md"); !errors.Is(err, ErrNotTree) {
		t.Fatalf("Tree on a file = %v, want ErrNotTree", err)
	}

	text, err := store.Blob(context.Background(), "acme/widget", mainHash, "nested/inner.txt")
	if err != nil {
		t.Fatalf("Blob text: %v", err)
	}
	if string(text.Data) != "inner\n" || text.Binary || text.TooLarge {
		t.Fatalf("text blob = %+v", text)
	}

	empty, err := store.Blob(context.Background(), "acme/widget", mainHash, "empty.txt")
	if err != nil || !empty.Empty || string(empty.Data) != "" {
		t.Fatalf("empty blob = %+v, %v", empty, err)
	}

	binary, err := store.Blob(context.Background(), "acme/widget", mainHash, "bin.dat")
	if err != nil || !binary.Binary || binary.Data != nil {
		t.Fatalf("binary blob = %+v, %v", binary, err)
	}

	symlink, err := store.Blob(context.Background(), "acme/widget", mainHash, "link.txt")
	if err != nil || !symlink.Symlink || symlink.LinkTarget != "nested/inner.txt" {
		t.Fatalf("symlink blob = %+v, %v", symlink, err)
	}

	submodule, err := store.Blob(context.Background(), "acme/widget", mainHash, "submod")
	if err != nil || !submodule.Submodule || submodule.Hash == "" {
		t.Fatalf("submodule blob = %+v, %v", submodule, err)
	}

	oversized, err := store.Blob(context.Background(), "acme/widget", mainHash, "large.bin")
	if err != nil || !oversized.TooLarge || oversized.Data != nil {
		t.Fatalf("oversized blob = %+v, %v", oversized, err)
	}

	if _, err := store.Blob(context.Background(), "acme/widget", mainHash, "nested"); !errors.Is(err, ErrNotBlob) {
		t.Fatalf("Blob on a directory = %v, want ErrNotBlob", err)
	}
	if _, err := store.Blob(context.Background(), "acme/widget", mainHash, "missing.txt"); !errors.Is(err, ErrNotExist) {
		t.Fatalf("Blob missing = %v, want ErrNotExist", err)
	}
	if _, err := store.Blob(context.Background(), "acme/widget", mainHash, "nested/inner.txt/child"); !errors.Is(err, ErrNotExist) {
		t.Fatalf("Blob through a file = %v, want ErrNotExist", err)
	}

	readme, found, err := store.Readme(context.Background(), "acme/widget", mainHash)
	if err != nil || !found {
		t.Fatalf("Readme = %+v, %v, %v", readme, found, err)
	}
	if !strings.Contains(string(readme.Data), "readme") {
		t.Fatalf("README data = %q", readme.Data)
	}
}

func TestReadmePreferenceAndSkips(t *testing.T) {
	store, path, env, _, _ := newBrowseRepo(t)

	commit := func(records []string) string {
		tree := browseMktree(t, env, path, records)
		return gitOutput(t, env, "", "--git-dir", path, "commit-tree", tree, "-m", "readme case")
	}
	setRef := func(name, hash string) {
		runGit(t, env, "--git-dir", path, "update-ref", "refs/heads/"+name, hash)
	}

	preferred := commit([]string{
		browseBlob(t, env, path, "100644", "readme.md", "lower\n"),
		browseBlob(t, env, path, "100644", "README.markdown", "markdown\n"),
		browseBlob(t, env, path, "100644", "README", "plain\n"),
	})
	setRef("preferred", preferred)

	markdown := commit([]string{
		browseBlob(t, env, path, "100644", "README.markdown", "markdown\n"),
		browseBlob(t, env, path, "100644", "README", "plain\n"),
	})
	setRef("markdown", markdown)

	plain := commit([]string{
		browseBlob(t, env, path, "100644", "README", "plain\n"),
	})
	setRef("plain", plain)

	absent := commit([]string{
		browseBlob(t, env, path, "100644", "other.txt", "other\n"),
	})
	setRef("absent", absent)

	symlink := commit([]string{
		browseBlob(t, env, path, "120000", "README.md", "README.markdown"),
		browseBlob(t, env, path, "100644", "README.markdown", "fallback\n"),
	})
	setRef("symlink", symlink)

	gitlink := commit([]string{
		"160000 commit " + preferred + "\tREADME.md",
		browseBlob(t, env, path, "100644", "README.markdown", "fallback\n"),
	})
	setRef("gitlink", gitlink)

	binary := commit([]string{
		browseBlob(t, env, path, "100644", "README.md", "text\x00binary"),
		browseBlob(t, env, path, "100644", "README", "fallback\n"),
	})
	setRef("binary", binary)

	oversized := commit([]string{
		browseBlob(t, env, path, "100644", "README.md", strings.Repeat("x", int(MaxReadmeBytes)+1)),
		browseBlob(t, env, path, "100644", "README", "fallback\n"),
	})
	setRef("oversized", oversized)

	onlyInvalid := commit([]string{
		browseBlob(t, env, path, "120000", "README.md", "README.markdown"),
	})
	setRef("only-invalid", onlyInvalid)

	cases := []struct {
		ref     string
		found   bool
		want    string
		tooBig  bool
		binary  bool
		symlink bool
	}{
		{ref: "preferred", found: true, want: "lower\n"},
		{ref: "markdown", found: true, want: "markdown\n"},
		{ref: "plain", found: true, want: "plain\n"},
		{ref: "absent", found: false},
		{ref: "symlink", found: true, want: "fallback\n"},
		{ref: "gitlink", found: true, want: "fallback\n"},
		{ref: "binary", found: true, want: "fallback\n"},
		{ref: "oversized", found: true, want: "fallback\n"},
		{ref: "only-invalid", found: true, symlink: true},
	}
	for _, tc := range cases {
		t.Run(tc.ref, func(t *testing.T) {
			commit, err := store.ResolveRef(context.Background(), "acme/widget", tc.ref)
			if err != nil {
				t.Fatalf("ResolveRef: %v", err)
			}
			blob, found, err := store.Readme(context.Background(), "acme/widget", commit.Hash)
			if err != nil {
				t.Fatalf("Readme: %v", err)
			}
			if found != tc.found {
				t.Fatalf("found = %v, want %v (%+v)", found, tc.found, blob)
			}
			if tc.want != "" && string(blob.Data) != tc.want {
				t.Fatalf("data = %q, want %q", blob.Data, tc.want)
			}
			if blob.TooLarge != tc.tooBig || blob.Binary != tc.binary || blob.Symlink != tc.symlink {
				t.Fatalf("skip flags = %+v", blob)
			}
		})
	}
}

func TestOversizedBlobIsNotRead(t *testing.T) {
	store, _, _, mainHash, _ := newBrowseRepo(t)
	trace := t.TempDir() + "/trace"
	t.Setenv("GIT_TRACE", trace)

	blob, err := store.Blob(context.Background(), "acme/widget", mainHash, "large.bin")
	if err != nil {
		t.Fatalf("Blob: %v", err)
	}
	if !blob.TooLarge || blob.Data != nil {
		t.Fatalf("oversized blob = %+v", blob)
	}
	data, err := os.ReadFile(trace)
	if err != nil {
		t.Fatalf("read git trace: %v", err)
	}
	if bytes.Contains(data, []byte("cat-file blob")) {
		t.Fatal("oversized blob content was read")
	}
}

func TestCommitMetadata(t *testing.T) {
	store, _, _, mainHash, _ := newBrowseRepo(t)
	commit, err := store.Commit(context.Background(), "acme/widget", mainHash)
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}
	if commit.Hash != mainHash || commit.Subject != "initial" || commit.When.IsZero() {
		t.Fatalf("commit = %+v", commit)
	}
}

func TestBrowsingHonorsCanceledContext(t *testing.T) {
	store, _, _, mainHash, _ := newBrowseRepo(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	start := time.Now()
	if _, err := store.ResolveRef(ctx, "acme/widget", ""); err == nil {
		t.Fatal("ResolveRef with a canceled context returned no error")
	}
	if _, err := store.Tree(ctx, "acme/widget", mainHash, ""); err == nil {
		t.Fatal("Tree with a canceled context returned no error")
	}
	if _, err := store.Blob(ctx, "acme/widget", mainHash, "README.md"); err == nil {
		t.Fatal("Blob with a canceled context returned no error")
	}
	if _, _, err := store.Readme(ctx, "acme/widget", mainHash); err == nil {
		t.Fatal("Readme with a canceled context returned no error")
	}
	if time.Since(start) > time.Second {
		t.Fatalf("canceled reads took %v", time.Since(start))
	}
}

// newBrowseRepo builds a repository with branches, an annotated tag, a detached
// commit, a README, a binary blob, a symlink, a submodule, an oversized blob,
// and awkward file names. It returns the store, git dir, environment, the main
// commit, and the commit an annotated tag points at.
func newBrowseRepo(t *testing.T) (*Store, string, []string, string, string) {
	t.Helper()
	store, err := Open(t.TempDir(), "main")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	path, err := store.Create(context.Background(), "acme/widget")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	when := time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)
	env := metadataGitEnv(t, when)

	emptyTree := gitOutput(t, env, "", "--git-dir", path, "mktree")
	subCommit := gitOutput(t, env, "", "--git-dir", path, "commit-tree", emptyTree, "-m", "submodule target")

	innerTree := browseMktree(t, env, path, []string{
		browseBlob(t, env, path, "100644", "inner.txt", "inner\n"),
	})
	large := strings.Repeat("x", int(MaxBlobBytes)+1)
	rootTree := browseMktree(t, env, path, []string{
		browseBlob(t, env, path, "100644", "README.md", "# readme\n\nhello\n"),
		browseBlob(t, env, path, "100644", "bin.dat", "text\x00binary"),
		browseBlob(t, env, path, "100644", "empty.txt", ""),
		browseBlob(t, env, path, "120000", "link.txt", "nested/inner.txt"),
		"160000 commit " + subCommit + "\tsubmod",
		browseBlob(t, env, path, "100644", "file with space.txt", "space\n"),
		browseBlob(t, env, path, "100644", "café.txt", "unicode\n"),
		browseBlob(t, env, path, "100644", "line\nbreak.txt", "newline\n"),
		browseBlob(t, env, path, "100644", "tab\tname.txt", "tab\n"),
		browseBlob(t, env, path, "100644", "large.bin", large),
		"040000 tree " + innerTree + "\tnested",
	})
	mainHash := gitOutput(t, env, "", "--git-dir", path, "commit-tree", rootTree, "-m", "initial")
	runGit(t, env, "--git-dir", path, "update-ref", "refs/heads/main", mainHash)

	otherTree := browseMktree(t, env, path, []string{
		browseBlob(t, env, path, "100644", "other.txt", "other\n"),
	})
	otherHash := gitOutput(t, env, "", "--git-dir", path, "commit-tree", otherTree, "-m", "other branch")
	runGit(t, env, "--git-dir", path, "update-ref", "refs/heads/other", otherHash)

	runGit(t, env, "--git-dir", path, "tag", "-a", "v1", "-m", "version one", mainHash)
	tagHash := gitOutput(t, env, "", "--git-dir", path, "rev-parse", "refs/tags/v1")
	return store, path, env, mainHash, tagHash
}

func browseBlob(t *testing.T, env []string, path, mode, name, content string) string {
	t.Helper()
	hash := gitOutput(t, env, content, "--git-dir", path, "hash-object", "-w", "--stdin")
	return mode + " blob " + hash + "\t" + name
}

func browseMktree(t *testing.T, env []string, path string, records []string) string {
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

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
