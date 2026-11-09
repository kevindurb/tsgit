package repos

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
)

// Browsing bounds. They keep one repository read from holding a handler or a
// public connection without limit.
const (
	// MaxTreeDepth is the largest number of components in a browsing path.
	MaxTreeDepth = 32
	// MaxTreePathBytes is the largest decoded browsing path accepted.
	MaxTreePathBytes = 4096
	// MaxBlobBytes is the largest file rendered as text.
	MaxBlobBytes int64 = 1 << 20
	// MaxReadmeBytes is the largest README rendered as Markdown.
	MaxReadmeBytes int64 = 512 << 10

	refTimeout    = 2 * time.Second
	browseTimeout = 5 * time.Second
	binarySniff   = 8 << 10
)

var (
	// ErrInvalidPath means a browsing path is not a valid git tree path.
	ErrInvalidPath = errors.New("invalid tree path")
	// ErrNotTree means the requested object is not a directory.
	ErrNotTree = errors.New("object is not a tree")
	// ErrNotBlob means the requested object is not a file.
	ErrNotBlob = errors.New("object is not a blob")
)

// Entry is one directory entry in a repository tree.
type Entry struct {
	Name       string
	Mode       string
	Type       string
	Hash       string
	Size       int64 // -1 when git does not report a size
	Dir        bool
	Symlink    bool
	Submodule  bool
	Executable bool
}

// Blob is one file at a resolved commit. Data is normalized UTF-8 text and is
// only set when the blob is renderable. The flag fields identify why a blob is
// not rendered.
type Blob struct {
	Path       string
	Hash       string
	Mode       string
	Size       int64
	Data       []byte
	Empty      bool
	Binary     bool
	TooLarge   bool
	Symlink    bool
	LinkTarget string
	Submodule  bool
}

// ParseTreePath validates the components after /tree/ or /blob/. An empty slice
// is the repository root. Empty, ".", "..", and control-character components
// are rejected; leading dots are allowed so .github is browsable.
func ParseTreePath(parts []string) (string, error) {
	if len(parts) > MaxTreeDepth {
		return "", ErrInvalidPath
	}
	total := 0
	for i, part := range parts {
		if part == "" || part == "." || part == ".." {
			return "", ErrInvalidPath
		}
		for _, r := range part {
			if unicode.IsControl(r) {
				return "", ErrInvalidPath
			}
		}
		total += len(part)
		if i > 0 {
			total++
		}
	}
	if total > MaxTreePathBytes {
		return "", ErrInvalidPath
	}
	return strings.Join(parts, "/"), nil
}

// ResolveRef resolves a browsing ref to a commit. An empty ref means HEAD.
// Full refs are limited to refs/heads and refs/tags. A full-length hex value is
// tried as a commit first and then as a short branch or tag name.
func (s *Store) ResolveRef(ctx context.Context, name, ref string) (Commit, error) {
	target, err := s.repositoryDir(name)
	if err != nil {
		return Commit{}, err
	}

	if ref == "" {
		commit, err := s.commitSpec(ctx, target, "HEAD")
		if err != nil {
			return Commit{}, refError(ctx, err)
		}
		commit.Ref = "HEAD"
		return commit, nil
	}

	lower := strings.ToLower(ref)
	if isFullHash(lower) {
		if commit, err := s.commitSpec(ctx, target, lower); err == nil {
			commit.Ref = lower
			return commit, nil
		}
	}

	if isFullRef(ref) {
		commit, err := s.commitSpec(ctx, target, ref)
		if err != nil {
			return Commit{}, refError(ctx, err)
		}
		commit.Ref = ref
		return commit, nil
	}

	for _, prefix := range []string{"refs/heads/", "refs/tags/"} {
		full := prefix + ref
		commit, err := s.commitSpec(ctx, target, full)
		if err == nil {
			commit.Ref = full
			return commit, nil
		}
		if ctx.Err() != nil {
			return Commit{}, ctx.Err()
		}
	}
	return Commit{}, ErrNotExist
}

// Commit reads the header metadata for an already-resolved commit.
func (s *Store) Commit(ctx context.Context, name, commit string) (Commit, error) {
	target, err := s.repositoryDir(name)
	if err != nil {
		return Commit{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, browseTimeout)
	defer cancel()
	c, err := s.commitMetadata(ctx, target, commit)
	if err != nil {
		return Commit{}, refError(ctx, err)
	}
	c.Ref = commit
	return c, nil
}

// Tree lists the entries of path at commit. An empty path is the repository
// root. A path that names a file returns ErrNotTree so the caller can redirect.
func (s *Store) Tree(ctx context.Context, name, commit, path string) ([]Entry, error) {
	if path != "" {
		if _, err := ParseTreePath(strings.Split(path, "/")); err != nil {
			return nil, ErrInvalidPath
		}
	}
	target, err := s.repositoryDir(name)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, browseTimeout)
	defer cancel()

	spec := commit + "^{tree}"
	if path != "" {
		spec = commit + ":" + path
	}
	out, err := s.git(ctx, target, "cat-file", "-t", "--", spec)
	if err != nil {
		return nil, refError(ctx, err)
	}
	kind := strings.TrimSpace(string(out))
	if kind != "tree" {
		return nil, ErrNotTree
	}
	out, err = s.git(ctx, target, "ls-tree", "-z", "-l", spec)
	if err != nil {
		return nil, refError(ctx, err)
	}
	return parseTree(out)
}

func parseTree(out []byte) ([]Entry, error) {
	var entries []Entry
	for _, record := range strings.Split(string(out), "\x00") {
		if record == "" {
			continue
		}
		meta, file, ok := strings.Cut(record, "\t")
		if !ok {
			return nil, fmt.Errorf("malformed ls-tree record")
		}
		fields := strings.Fields(meta)
		if len(fields) != 4 {
			return nil, fmt.Errorf("malformed ls-tree record")
		}
		entry := Entry{
			Name: strings.ToValidUTF8(file, "\uFFFD"),
			Mode: fields[0],
			Type: fields[1],
			Hash: fields[2],
			Size: -1,
		}
		if fields[3] != "-" {
			if n, err := strconv.ParseInt(fields[3], 10, 64); err == nil {
				entry.Size = n
			}
		}
		switch entry.Type {
		case "tree":
			entry.Dir = true
		case "commit":
			entry.Submodule = true
		case "blob":
			entry.Symlink = entry.Mode == "120000"
			entry.Executable = entry.Mode == "100755"
		}
		entries = append(entries, entry)
	}
	sort.SliceStable(entries, func(i, j int) bool {
		if entries[i].Dir != entries[j].Dir {
			return entries[i].Dir
		}
		return entries[i].Name < entries[j].Name
	})
	return entries, nil
}

// Blob reads a file at commit. The path names a single entry and must not be
// empty. A directory returns ErrNotBlob.
func (s *Store) Blob(ctx context.Context, name, commit, path string) (Blob, error) {
	return s.blob(ctx, name, commit, path, MaxBlobBytes)
}

func (s *Store) blob(ctx context.Context, name, commit, path string, limit int64) (Blob, error) {
	if path == "" {
		return Blob{}, ErrNotBlob
	}
	components := strings.Split(path, "/")
	if _, err := ParseTreePath(components); err != nil {
		return Blob{}, ErrInvalidPath
	}
	target, err := s.repositoryDir(name)
	if err != nil {
		return Blob{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, browseTimeout)
	defer cancel()

	parent := strings.Join(components[:len(components)-1], "/")
	base := components[len(components)-1]
	entries, err := s.Tree(ctx, name, commit, parent)
	if errors.Is(err, ErrNotTree) {
		// A component of the path names a file, so no such entry exists.
		return Blob{}, ErrNotExist
	}
	if err != nil {
		return Blob{}, err
	}
	var entry *Entry
	for i := range entries {
		if entries[i].Name == base {
			entry = &entries[i]
			break
		}
	}
	if entry == nil || entry.Type == "tree" {
		if entry == nil {
			return Blob{}, ErrNotExist
		}
		return Blob{}, ErrNotBlob
	}

	blob := Blob{Path: path, Hash: entry.Hash, Mode: entry.Mode, Size: entry.Size}
	if entry.Type == "commit" {
		blob.Submodule = true
		return blob, nil
	}
	if entry.Type != "blob" {
		return Blob{}, ErrNotExist
	}

	spec := commit + ":" + path
	out, err := s.git(ctx, target, "cat-file", "-s", spec)
	if err != nil {
		return Blob{}, refError(ctx, err)
	}
	size, err := strconv.ParseInt(strings.TrimSpace(string(out)), 10, 64)
	if err != nil {
		return Blob{}, fmt.Errorf("read size of %s: %w", path, err)
	}
	blob.Size = size
	if size > limit {
		blob.TooLarge = true
		return blob, nil
	}
	data, err := s.git(ctx, target, "cat-file", "blob", spec)
	if err != nil {
		return Blob{}, refError(ctx, err)
	}
	if entry.Mode == "120000" {
		blob.Symlink = true
		blob.LinkTarget = strings.ToValidUTF8(string(data), "\uFFFD")
		return blob, nil
	}
	sniff := data
	if len(sniff) > binarySniff {
		sniff = sniff[:binarySniff]
	}
	if bytes.IndexByte(sniff, 0) >= 0 {
		blob.Binary = true
		return blob, nil
	}
	text := strings.ToValidUTF8(string(data), "\uFFFD")
	blob.Data = []byte(text)
	blob.Empty = len(text) == 0
	return blob, nil
}

// Readme finds the root README at commit. Names are matched case-insensitively
// in preference order README.md, README.markdown, README. A preferred entry that
// cannot be rendered is skipped; if every matching entry is unrenderable, the
// first one is returned with its skip reason set.
func (s *Store) Readme(ctx context.Context, name, commit string) (Blob, bool, error) {
	entries, err := s.Tree(ctx, name, commit, "")
	if err != nil {
		return Blob{}, false, err
	}
	candidates := []string{"README.md", "README.markdown", "README"}
	var skipped *Blob
	for _, candidate := range candidates {
		for _, entry := range entries {
			if !strings.EqualFold(entry.Name, candidate) {
				continue
			}
			switch entry.Type {
			case "commit":
				if skipped == nil {
					skipped = &Blob{
						Path:      entry.Name,
						Hash:      entry.Hash,
						Mode:      entry.Mode,
						Size:      entry.Size,
						Submodule: true,
					}
				}
				continue
			case "blob":
			default:
				continue
			}
			blob, err := s.blob(ctx, name, commit, entry.Name, MaxReadmeBytes)
			if err != nil {
				if ctx.Err() != nil {
					return Blob{}, false, ctx.Err()
				}
				continue
			}
			if blob.Symlink || blob.Submodule || blob.Binary || blob.TooLarge {
				if skipped == nil {
					b := blob
					skipped = &b
				}
				continue
			}
			return blob, true, nil
		}
	}
	if skipped != nil {
		return *skipped, true, nil
	}
	return Blob{}, false, nil
}

func (s *Store) repositoryDir(name string) (string, error) {
	canonical, err := ParseName(name)
	if err != nil {
		return "", err
	}
	target, err := s.Resolve(canonical)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(target)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", ErrNotExist
		}
		return "", fmt.Errorf("stat repository %s: %w", canonical, err)
	}
	if !info.IsDir() {
		return "", ErrNotExist
	}
	return target, nil
}

func (s *Store) commitSpec(ctx context.Context, target, spec string) (Commit, error) {
	ctx, cancel := context.WithTimeout(ctx, refTimeout)
	defer cancel()
	out, err := s.git(ctx, target, "rev-parse", "--verify", "--end-of-options", spec+"^{commit}")
	if err != nil {
		return Commit{}, err
	}
	return s.commitMetadata(ctx, target, strings.TrimSpace(string(out)))
}

func (s *Store) commitMetadata(ctx context.Context, target, hash string) (Commit, error) {
	out, err := s.git(ctx, target, "show", "-s", "--format=%H%x00%ct%x00%s", "--end-of-options", hash)
	if err != nil {
		return Commit{}, err
	}
	parts := strings.Split(strings.TrimSuffix(string(out), "\n"), "\x00")
	if len(parts) != 3 {
		return Commit{}, fmt.Errorf("malformed commit metadata")
	}
	when, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return Commit{}, fmt.Errorf("parse commit time: %w", err)
	}
	return Commit{
		Hash:    parts[0],
		When:    time.Unix(when, 0).UTC(),
		Subject: strings.ToValidUTF8(parts[2], "\uFFFD"),
	}, nil
}

func (s *Store) git(ctx context.Context, target string, args ...string) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	cmd := exec.CommandContext(ctx, s.gitPath, append([]string{"--git-dir", target}, args...)...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return out, nil
}

func refError(ctx context.Context, err error) error {
	if err != nil && ctx.Err() != nil {
		return ctx.Err()
	}
	return ErrNotExist
}

func isFullHash(value string) bool {
	if len(value) != 40 && len(value) != 64 {
		return false
	}
	for _, r := range value {
		switch {
		case r >= '0' && r <= '9':
		case r >= 'a' && r <= 'f':
		default:
			return false
		}
	}
	return true
}

func isFullRef(value string) bool {
	if rest, ok := strings.CutPrefix(value, "refs/heads/"); ok {
		return rest != ""
	}
	if rest, ok := strings.CutPrefix(value, "refs/tags/"); ok {
		return rest != ""
	}
	return false
}
