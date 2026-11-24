package repos

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"sort"
	"strings"
)

// RefChangeKind classifies how a ref moved between two snapshots.
type RefChangeKind int

const (
	// RefCreated means the ref did not exist in the before snapshot.
	RefCreated RefChangeKind = iota
	// RefUpdated means the ref moved from one object to another.
	RefUpdated
	// RefDeleted means the ref existed in the before snapshot and is gone.
	RefDeleted
)

func (k RefChangeKind) String() string {
	switch k {
	case RefCreated:
		return "created"
	case RefUpdated:
		return "updated"
	case RefDeleted:
		return "deleted"
	default:
		return "unknown"
	}
}

// RefChange describes one ref that differed between two snapshots. Old and
// New carry the empty object ID (all zeros, same length as the real SHA) for
// created and deleted refs respectively. Forced is only meaningful for
// RefUpdated and is filled in by the caller with Store.IsAncestor.
type RefChange struct {
	Ref    string
	Kind   RefChangeKind
	Old    string
	New    string
	Forced bool
}

// zeroOID returns an all-zero object ID of the same length as sha, matching
// the empty-object-id convention the smart-HTTP protocol uses.
func zeroOID(sha string) string {
	return strings.Repeat("0", len(sha))
}

// DiffRefs compares two snapshots and reports every ref that appeared, moved,
// or disappeared. The result is sorted by ref name for determinism.
func DiffRefs(before, after map[string]string) []RefChange {
	var changes []RefChange
	for ref, newSHA := range after {
		oldSHA, ok := before[ref]
		switch {
		case !ok:
			changes = append(changes, RefChange{Ref: ref, Kind: RefCreated, Old: zeroOID(newSHA), New: newSHA})
		case oldSHA != newSHA:
			changes = append(changes, RefChange{Ref: ref, Kind: RefUpdated, Old: oldSHA, New: newSHA})
		}
	}
	for ref, oldSHA := range before {
		if _, ok := after[ref]; ok {
			continue
		}
		changes = append(changes, RefChange{Ref: ref, Kind: RefDeleted, Old: oldSHA, New: zeroOID(oldSHA)})
	}
	sort.Slice(changes, func(i, j int) bool { return changes[i].Ref < changes[j].Ref })
	return changes
}

// SnapshotRefs returns refname -> object ID for every ref under refs/, using
// the same timeout discipline as the other read helpers. It takes the bare
// repository path directly because callers already resolved it.
func (s *Store) SnapshotRefs(ctx context.Context, path string) (map[string]string, error) {
	ctx, cancel := context.WithTimeout(ctx, metadataTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, s.gitPath, "--git-dir", path, "for-each-ref",
		"--format=%(refname)%00%(objectname)")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		if ctx.Err() != nil {
			return nil, fmt.Errorf("snapshot refs for %s: %w", path, ctx.Err())
		}
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return nil, fmt.Errorf("snapshot refs for %s: %w: %s", path, err, strings.TrimSpace(stderr.String()))
		}
		return nil, fmt.Errorf("snapshot refs for %s: %w", path, err)
	}

	refs := make(map[string]string)
	for _, line := range strings.Split(strings.TrimSuffix(string(out), "\n"), "\n") {
		if line == "" {
			continue
		}
		name, sha, ok := strings.Cut(line, "\x00")
		if !ok {
			return nil, fmt.Errorf("snapshot refs for %s: malformed for-each-ref line", path)
		}
		refs[name] = sha
	}
	return refs, nil
}

// IsAncestor reports whether oldSHA is an ancestor of newSHA, using
// git merge-base --is-ancestor. A missing object (for example a ref that was
// force-pushed away and then pruned) is an error, not a false answer.
func (s *Store) IsAncestor(ctx context.Context, path, oldSHA, newSHA string) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, metadataTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, s.gitPath, "--git-dir", path,
		"merge-base", "--is-ancestor", oldSHA, newSHA)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err == nil {
		return true, nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
		return false, nil
	}
	if ctx.Err() != nil {
		return false, fmt.Errorf("merge-base in %s: %w", path, ctx.Err())
	}
	return false, fmt.Errorf("merge-base in %s: %w: %s", path, err, strings.TrimSpace(stderr.String()))
}
