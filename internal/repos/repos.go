// Package repos maps canonical repository names to bare git directories under
// a single root. It owns containment checks and atomic creation; it does not
// know who asked.
package repos

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"unicode"
)

var (
	// ErrInvalidName means the requested name is not a canonical
	// <namespace>/<repo>.
	ErrInvalidName = errors.New("invalid repository name")
	// ErrOutsideRoot means the requested name resolves outside the repository
	// root, directly or through a symlink.
	ErrOutsideRoot = errors.New("repository path escapes root")
	// ErrNotExist means the repository has not been created.
	ErrNotExist = errors.New("repository does not exist")
)

// Store is a repository tree rooted at a single directory.
type Store struct {
	root          string
	staging       string
	defaultBranch string
	gitPath       string
}

// Open validates the repository root and the git binary, creating the root and
// its staging directory when they are missing.
func Open(root, defaultBranch string) (*Store, error) {
	if root == "" {
		return nil, errors.New("repository root must not be empty")
	}
	if defaultBranch == "" {
		return nil, errors.New("default branch must not be empty")
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolve repository root %q: %w", root, err)
	}
	if err := os.MkdirAll(abs, 0o750); err != nil {
		return nil, fmt.Errorf("create repository root %q: %w", abs, err)
	}
	real, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return nil, fmt.Errorf("resolve repository root %q: %w", abs, err)
	}
	info, err := os.Stat(real)
	if err != nil {
		return nil, fmt.Errorf("stat repository root %q: %w", real, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("repository root %q is not a directory", real)
	}

	staging := filepath.Join(real, ".tsgit-tmp")
	if err := os.MkdirAll(staging, 0o750); err != nil {
		return nil, fmt.Errorf("repository root %q is not writable: %w", real, err)
	}
	probe, err := os.CreateTemp(staging, "probe-*")
	if err != nil {
		return nil, fmt.Errorf("repository root %q is not writable: %w", real, err)
	}
	_ = probe.Close()
	_ = os.Remove(probe.Name())

	gitPath, err := exec.LookPath("git")
	if err != nil {
		return nil, fmt.Errorf("git is required but was not found in PATH: %w", err)
	}

	return &Store{
		root:          real,
		staging:       staging,
		defaultBranch: defaultBranch,
		gitPath:       gitPath,
	}, nil
}

// Root is the resolved, symlink-free repository root.
func (s *Store) Root() string { return s.root }

// GitPath is the git binary used for creation.
func (s *Store) GitPath() string { return s.gitPath }

// ParseName validates and canonicalizes a two-segment <namespace>/<repo> name,
// removing one trailing .git from the repository segment.
func ParseName(raw string) (string, error) {
	if raw == "" || strings.HasPrefix(raw, "/") || strings.Contains(raw, `\`) {
		return "", ErrInvalidName
	}
	segments := strings.Split(raw, "/")
	if len(segments) != 2 {
		return "", ErrInvalidName
	}
	namespace, repo := segments[0], segments[1]
	if err := validateSegment(namespace); err != nil {
		return "", err
	}
	repo = strings.TrimSuffix(repo, ".git")
	if err := validateSegment(repo); err != nil {
		return "", err
	}
	return namespace + "/" + repo, nil
}

func validateSegment(segment string) error {
	if segment == "" || segment == "." || segment == ".." {
		return ErrInvalidName
	}
	if strings.HasPrefix(segment, ".") {
		return ErrInvalidName
	}
	for _, r := range segment {
		if unicode.IsControl(r) {
			return ErrInvalidName
		}
	}
	return nil
}

// Resolve maps a canonical name to its bare directory after checking that the
// path stays inside the root, including through existing symlinks.
func (s *Store) Resolve(name string) (string, error) {
	canonical, err := ParseName(name)
	if err != nil {
		return "", err
	}
	namespace, repo, _ := strings.Cut(canonical, "/")
	target := filepath.Join(s.root, namespace, repo+".git")
	if !within(s.root, target) {
		return "", ErrOutsideRoot
	}

	existing := target
	for {
		if _, err := os.Lstat(existing); err == nil {
			break
		} else if !errors.Is(err, os.ErrNotExist) {
			return "", fmt.Errorf("inspect %q: %w", existing, err)
		}
		parent := filepath.Dir(existing)
		if parent == existing {
			return "", ErrOutsideRoot
		}
		existing = parent
	}
	real, err := filepath.EvalSymlinks(existing)
	if err != nil {
		return "", fmt.Errorf("resolve %q: %w", existing, err)
	}
	if !within(s.root, real) {
		return "", ErrOutsideRoot
	}
	return target, nil
}

// Exists reports whether the bare repository directory exists.
func (s *Store) Exists(name string) bool {
	target, err := s.Resolve(name)
	if err != nil {
		return false
	}
	info, err := os.Stat(target)
	return err == nil && info.IsDir()
}

// Create initializes a bare repository atomically. If another creator wins the
// race, the existing repository is returned.
func (s *Store) Create(ctx context.Context, name string) (string, error) {
	target, err := s.Resolve(name)
	if err != nil {
		return "", err
	}
	if dirNonEmpty(target) {
		return target, nil
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o750); err != nil {
		return "", fmt.Errorf("create namespace for %s: %w", name, err)
	}
	// Re-check after creating the namespace: a component could have been a
	// symlink that the initial resolution did not see.
	if _, err := s.Resolve(name); err != nil {
		return "", err
	}

	staging, err := os.MkdirTemp(s.staging, "create-*")
	if err != nil {
		return "", fmt.Errorf("stage repository %s: %w", name, err)
	}
	defer os.RemoveAll(staging)

	cmd := exec.CommandContext(ctx, s.gitPath, "init", "--bare", "--initial-branch="+s.defaultBranch, staging)
	if out, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("git init %s: %w: %s", name, err, strings.TrimSpace(string(out)))
	}
	if err := os.Chmod(staging, 0o750); err != nil {
		return "", fmt.Errorf("chmod staged repository %s: %w", name, err)
	}

	if err := os.Rename(staging, target); err != nil {
		// A concurrent creator may have renamed its own staging directory
		// into place after our existence check.
		if dirNonEmpty(target) {
			return target, nil
		}
		return "", fmt.Errorf("publish repository %s: %w", name, err)
	}
	return target, nil
}

func within(root, target string) bool {
	rel, err := filepath.Rel(root, target)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}

func dirNonEmpty(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	names, err := f.Readdirnames(1)
	return err == nil && len(names) > 0
}
