// Package repos maps canonical repository names to bare git directories under
// a single root. It owns containment checks and atomic creation; it does not
// know who asked.
package repos

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
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
	// ErrCleanup means the repository disappeared from the served tree but
	// its staging copy could not be removed.
	ErrCleanup = errors.New("repository removed but cleanup failed")
)

// Store is a repository tree rooted at a single directory.
type Store struct {
	root          string
	staging       string
	defaultBranch string
	gitPath       string

	// mu serializes creation and deletion so a name cannot be created and
	// removed between another operation's existence check and its rename.
	mu sync.Mutex
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
	s.mu.Lock()
	defer s.mu.Unlock()
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

// Delete permanently removes a repository. It renames the bare directory into
// the staging area under the store lock, then removes that copy without the
// lock held. The rename makes the repository disappear atomically and lets an
// in-flight git process finish against its open files.
func (s *Store) Delete(ctx context.Context, name string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	canonical, err := ParseName(name)
	if err != nil {
		return err
	}
	target, err := s.Resolve(canonical)
	if err != nil {
		return err
	}

	s.mu.Lock()
	info, err := os.Lstat(target)
	if err != nil {
		s.mu.Unlock()
		if errors.Is(err, os.ErrNotExist) {
			return ErrNotExist
		}
		return fmt.Errorf("inspect repository %s: %w", canonical, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		s.mu.Unlock()
		return fmt.Errorf("repository %s is a symlink; refusing to delete it", canonical)
	}
	if !info.IsDir() {
		s.mu.Unlock()
		return ErrNotExist
	}

	trash, err := os.MkdirTemp(s.staging, "delete-*")
	if err != nil {
		s.mu.Unlock()
		return fmt.Errorf("stage deletion of %s: %w", canonical, err)
	}
	dest := filepath.Join(trash, filepath.Base(target))
	if err := os.Rename(target, dest); err != nil {
		s.mu.Unlock()
		_ = os.Remove(trash)
		return fmt.Errorf("remove repository %s: %w", canonical, err)
	}
	s.mu.Unlock()

	cleanupErr := os.RemoveAll(trash)
	if ns := filepath.Dir(target); ns != s.root {
		if nsInfo, err := os.Lstat(ns); err == nil && nsInfo.IsDir() {
			_ = os.Remove(ns)
		}
	}
	if cleanupErr != nil {
		return fmt.Errorf("%w: %v", ErrCleanup, cleanupErr)
	}
	return nil
}

// Commit is a commit's header metadata. Ref names the ref a browsing request
// resolved, when there was one; it is empty for metadata read outside browsing.
type Commit struct {
	Hash    string
	Ref     string
	Subject string
	When    time.Time
}

// Info is the metadata the discovery front end renders for one repository. A
// non-nil Err means the repository exists but part of its metadata could not
// be read; Name is still valid and the other fields may be partial.
type Info struct {
	Name        string
	Description string
	Branch      string
	Detached    bool
	Empty       bool
	Latest      Commit
	Branches    int
	Tags        int
	Refs        []string
	Err         error
}

// metadataTimeout bounds the git process used to read a repository's refs so a
// pathological repository cannot hold up the index.
const metadataTimeout = 2 * time.Second

// List returns metadata for every bare repository under the root. A failure to
// read one repository's metadata is reported on that Info's Err field; List
// returns an error only when the root itself cannot be read.
func (s *Store) List(ctx context.Context) ([]Info, error) {
	entries, err := os.ReadDir(s.root)
	if err != nil {
		return nil, fmt.Errorf("read repository root: %w", err)
	}

	var infos []Info
	for _, nsEntry := range entries {
		if !nsEntry.IsDir() || strings.HasPrefix(nsEntry.Name(), ".") {
			continue
		}
		namespace := nsEntry.Name()
		if err := validateSegment(namespace); err != nil {
			continue
		}

		repoEntries, err := os.ReadDir(filepath.Join(s.root, namespace))
		if err != nil {
			continue
		}
		for _, repoEntry := range repoEntries {
			if !repoEntry.IsDir() || strings.HasPrefix(repoEntry.Name(), ".") {
				continue
			}
			if !strings.HasSuffix(repoEntry.Name(), ".git") {
				continue
			}
			name, err := ParseName(namespace + "/" + strings.TrimSuffix(repoEntry.Name(), ".git"))
			if err != nil {
				continue
			}
			if _, err := s.Resolve(name); err != nil {
				continue
			}

			info, err := s.Info(ctx, name)
			if err != nil {
				if errors.Is(err, ErrNotExist) {
					continue
				}
				info.Err = err
			}
			infos = append(infos, info)
		}
	}
	return infos, nil
}

// Info reads the metadata for one canonical repository name. ErrNotExist means
// the repository directory is missing. Any other error means the repository
// exists but its metadata could not be read; the returned Info still carries
// the name and whatever was read before the failure.
func (s *Store) Info(ctx context.Context, name string) (Info, error) {
	canonical, err := ParseName(name)
	if err != nil {
		return Info{}, err
	}
	target, err := s.Resolve(canonical)
	if err != nil {
		return Info{Name: canonical}, err
	}
	st, err := os.Stat(target)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Info{Name: canonical}, ErrNotExist
		}
		return Info{Name: canonical}, fmt.Errorf("stat repository %s: %w", canonical, err)
	}
	if !st.IsDir() {
		return Info{Name: canonical}, ErrNotExist
	}

	info := Info{Name: canonical}
	if b, err := readRegularFile(filepath.Join(target, "description")); err == nil {
		info.Description = strings.ToValidUTF8(cleanDescription(string(b)), "\uFFFD")
	}

	head, err := readRegularFile(filepath.Join(target, "HEAD"))
	if err != nil {
		return info, fmt.Errorf("read HEAD for %s: %w", canonical, err)
	}
	headText := strings.TrimSpace(string(head))
	if ref, ok := strings.CutPrefix(headText, "ref: "); ok {
		if branch, ok := strings.CutPrefix(ref, "refs/heads/"); ok {
			info.Branch = branch
		}
	} else if headText != "" {
		info.Detached = true
	}

	refs, err := s.readRefs(ctx, canonical, target)
	if err != nil {
		return info, err
	}
	branchFound := false
	for _, ref := range refs {
		switch {
		case strings.HasPrefix(ref.Name, "refs/heads/"):
			info.Branches++
			short := strings.TrimPrefix(ref.Name, "refs/heads/")
			short = strings.ToValidUTF8(short, "\uFFFD")
			info.Refs = append(info.Refs, short)
			if short == info.Branch {
				branchFound = true
			}
		case strings.HasPrefix(ref.Name, "refs/tags/"):
			info.Tags++
			short := strings.TrimPrefix(ref.Name, "refs/tags/")
			info.Refs = append(info.Refs, strings.ToValidUTF8(short, "\uFFFD"))
		}
	}
	if len(refs) > 0 {
		info.Latest = refs[0].commit()
	}
	if info.Branch != "" {
		info.Empty = !branchFound
	} else if info.Detached {
		info.Empty = false
	} else {
		info.Empty = info.Latest.Hash == ""
	}
	return info, nil
}

type refInfo struct {
	Name    string
	Hash    string
	When    time.Time
	Subject string
}

func (r refInfo) commit() Commit {
	return Commit{Hash: r.Hash, Subject: r.Subject, When: r.When}
}

func (s *Store) readRefs(ctx context.Context, name, target string) ([]refInfo, error) {
	ctx, cancel := context.WithTimeout(ctx, metadataTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, s.gitPath, "--git-dir", target, "for-each-ref",
		"--sort=-committerdate",
		"--format=%(refname)%00%(objectname:short)%00%(committerdate:iso-strict)%00%(subject)",
		"refs/heads", "refs/tags",
	)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		if ctx.Err() != nil {
			return nil, fmt.Errorf("read refs for %s: %w", name, ctx.Err())
		}
		return nil, fmt.Errorf("read refs for %s: %w: %s", name, err, strings.TrimSpace(stderr.String()))
	}

	var refs []refInfo
	for _, line := range strings.Split(strings.TrimSuffix(string(out), "\n"), "\n") {
		if line == "" {
			continue
		}
		fields := strings.Split(line, "\x00")
		if len(fields) != 4 {
			return nil, fmt.Errorf("read refs for %s: malformed for-each-ref output", name)
		}
		when, _ := time.Parse(time.RFC3339, fields[2])
		refs = append(refs, refInfo{
			Name:    fields[0],
			Hash:    fields[1],
			When:    when,
			Subject: strings.ToValidUTF8(fields[3], "\uFFFD"),
		})
	}
	return refs, nil
}

func readRegularFile(path string) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%s is not a regular file", path)
	}
	return os.ReadFile(path)
}

func cleanDescription(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "Unnamed repository;") {
		return ""
	}
	return s
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
