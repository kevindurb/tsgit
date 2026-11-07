package transport

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"strings"
)

// GitBin is the first Backend: it execs git's own upload-pack or receive-pack.
// ADR 0001 governs this choice; the interface is what keeps it replaceable.
type GitBin struct {
	Git string
	Log *slog.Logger
}

// Serve runs the service against RepoPath and streams the exchange.
func (g *GitBin) Serve(ctx context.Context, req Request) error {
	gitPath := g.Git
	if gitPath == "" {
		gitPath = "git"
	}
	// Service names are the HTTP names (git-upload-pack); the git command is
	// `git upload-pack`.
	subcommand := strings.TrimPrefix(req.Service, "git-")
	args := []string{subcommand}
	if req.Stateless {
		args = append(args, "--stateless-rpc")
	}
	if req.Advertise {
		args = append(args, "--advertise-refs")
	}
	args = append(args, req.RepoPath)

	cmd := exec.CommandContext(ctx, gitPath, args...)
	cmd.Stdin = req.Body
	cmd.Stdout = req.Out
	stderr := &cappedBuffer{limit: 32 * 1024}
	cmd.Stderr = stderr
	if req.GitProtocol != "" {
		cmd.Env = append(os.Environ(), "GIT_PROTOCOL="+req.GitProtocol)
	}

	err := cmd.Run()
	if stderr.Len() > 0 {
		log := g.Log
		if log == nil {
			log = slog.Default()
		}
		log.Warn("git wrote to stderr",
			"service", req.Service,
			"repo", req.RepoPath,
			"stderr", stderr.String(),
		)
	}
	if err != nil {
		return fmt.Errorf("%s %s: %w", req.Service, req.RepoPath, err)
	}
	return nil
}

// cappedBuffer keeps the first limit bytes while draining the rest, so a
// verbose git process cannot block on a full stderr pipe.
type cappedBuffer struct {
	buf   bytes.Buffer
	limit int
}

func (c *cappedBuffer) Write(p []byte) (int, error) {
	if remaining := c.limit - c.buf.Len(); remaining > 0 {
		if len(p) < remaining {
			remaining = len(p)
		}
		_, _ = c.buf.Write(p[:remaining])
	}
	return len(p), nil
}

func (c *cappedBuffer) Len() int       { return c.buf.Len() }
func (c *cappedBuffer) String() string { return c.buf.String() }
