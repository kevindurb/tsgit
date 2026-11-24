package repos

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestDiffRefs(t *testing.T) {
	sha := func(c string) string {
		return strings.Repeat(c, 40)
	}
	tests := []struct {
		name   string
		before map[string]string
		after  map[string]string
		want   []RefChange
	}{
		{
			name:   "no refs moved",
			before: map[string]string{"refs/heads/main": sha("a")},
			after:  map[string]string{"refs/heads/main": sha("a")},
			want:   nil,
		},
		{
			name:   "branch created",
			before: map[string]string{},
			after:  map[string]string{"refs/heads/main": sha("a")},
			want: []RefChange{
				{Ref: "refs/heads/main", Kind: RefCreated, Old: strings.Repeat("0", 40), New: sha("a")},
			},
		},
		{
			name:   "branch updated",
			before: map[string]string{"refs/heads/main": sha("a")},
			after:  map[string]string{"refs/heads/main": sha("b")},
			want: []RefChange{
				{Ref: "refs/heads/main", Kind: RefUpdated, Old: sha("a"), New: sha("b")},
			},
		},
		{
			name:   "branch deleted",
			before: map[string]string{"refs/heads/old": sha("a")},
			after:  map[string]string{},
			want: []RefChange{
				{Ref: "refs/heads/old", Kind: RefDeleted, Old: sha("a"), New: strings.Repeat("0", 40)},
			},
		},
		{
			name: "mixed refs sorted by name",
			before: map[string]string{
				"refs/heads/main": sha("a"),
				"refs/tags/v1":    sha("c"),
			},
			after: map[string]string{
				"refs/heads/main": sha("b"),
				"refs/tags/v2":    sha("d"),
			},
			want: []RefChange{
				{Ref: "refs/heads/main", Kind: RefUpdated, Old: sha("a"), New: sha("b")},
				{Ref: "refs/tags/v1", Kind: RefDeleted, Old: sha("c"), New: strings.Repeat("0", 40)},
				{Ref: "refs/tags/v2", Kind: RefCreated, Old: strings.Repeat("0", 40), New: sha("d")},
			},
		},
		{
			name:   "sha256-length zeros",
			before: map[string]string{"refs/heads/main": strings.Repeat("a", 64)},
			after:  map[string]string{},
			want: []RefChange{
				{Ref: "refs/heads/main", Kind: RefDeleted, Old: strings.Repeat("a", 64), New: strings.Repeat("0", 64)},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := DiffRefs(tt.before, tt.after)
			if len(got) != len(tt.want) {
				t.Fatalf("DiffRefs returned %d changes %v, want %d", len(got), got, len(tt.want))
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("change %d = %+v, want %+v", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestSnapshotRefs(t *testing.T) {
	store, err := Open(t.TempDir(), "main")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	when := time.Date(2026, 11, 24, 12, 0, 0, 0, time.UTC)
	path, first := writeCommit(t, store, "acme/widget", "main", "first", when)
	env := metadataGitEnv(t, when)
	runGit(t, env, "--git-dir", path, "tag", "-a", "v1", "-m", "release")

	tagOID := gitOutput(t, env, "", "--git-dir", path, "rev-parse", "refs/tags/v1")

	before, err := store.SnapshotRefs(context.Background(), path)
	if err != nil {
		t.Fatalf("SnapshotRefs: %v", err)
	}
	want := map[string]string{
		"refs/heads/main": first,
		"refs/tags/v1":    tagOID,
	}
	if len(before) != len(want) {
		t.Fatalf("SnapshotRefs returned %v, want %v", before, want)
	}
	for ref, oid := range want {
		if before[ref] != oid {
			t.Errorf("ref %s = %q, want %q", ref, before[ref], oid)
		}
	}
}

func TestSnapshotRefsEmptyRepository(t *testing.T) {
	store, err := Open(t.TempDir(), "main")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	path, err := store.Create(context.Background(), "acme/empty")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	refs, err := store.SnapshotRefs(context.Background(), path)
	if err != nil {
		t.Fatalf("SnapshotRefs: %v", err)
	}
	if len(refs) != 0 {
		t.Fatalf("SnapshotRefs on empty repository = %v, want empty", refs)
	}
}

func TestIsAncestor(t *testing.T) {
	store, err := Open(t.TempDir(), "main")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	when := time.Date(2026, 11, 24, 12, 0, 0, 0, time.UTC)
	path, first := writeCommit(t, store, "acme/widget", "main", "first", when)
	second := func() string {
		env := metadataGitEnv(t, when.Add(time.Minute))
		blob := gitOutput(t, env, "two\n", "--git-dir", path, "hash-object", "-w", "--stdin")
		tree := gitOutput(t, env, "100644 blob "+blob+"\tfile.txt\n", "--git-dir", path, "mktree")
		commit := gitOutput(t, env, "", "--git-dir", path, "commit-tree", tree, "-p", first, "-m", "second")
		runGit(t, env, "--git-dir", path, "update-ref", "refs/heads/main", commit)
		return commit
	}()

	forced, err := store.IsAncestor(context.Background(), path, first, second)
	if err != nil {
		t.Fatalf("IsAncestor(first, second): %v", err)
	}
	if !forced {
		t.Error("IsAncestor(first, second) = false, want true")
	}

	forced, err = store.IsAncestor(context.Background(), path, second, first)
	if err != nil {
		t.Fatalf("IsAncestor(second, first): %v", err)
	}
	if forced {
		t.Error("IsAncestor(second, first) = true, want false")
	}

	if _, err := store.IsAncestor(context.Background(), path, strings.Repeat("f", 40), second); err == nil {
		t.Error("IsAncestor with a missing object: want error, got nil")
	}
}
