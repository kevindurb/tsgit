package transport

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kevindurb/tsgit/internal/repos"
)

func TestGitBinPassesArgumentsAndProtocol(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "fake-git")
	body := "#!/bin/sh\nprintf 'args=%s\\n' \"$*\"\nprintf 'protocol=%s\\n' \"$GIT_PROTOCOL\"\n"
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	backend := &GitBin{Git: script}
	err := backend.Serve(context.Background(), Request{
		RepoPath:    "/repos/acme/widget.git",
		Service:     ServiceUploadPack,
		GitProtocol: "version=2",
		Advertise:   true,
		Stateless:   true,
		Body:        strings.NewReader(""),
		Out:         &out,
	})
	if err != nil {
		t.Fatalf("Serve: %v", err)
	}
	got := out.String()
	for _, want := range []string{
		"args=upload-pack --stateless-rpc --advertise-refs /repos/acme/widget.git",
		"protocol=version=2",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("output %q does not contain %q", got, want)
		}
	}
}

func TestGitBinWithRealGit(t *testing.T) {
	store, err := repos.Open(t.TempDir(), "main")
	if err != nil {
		t.Fatal(err)
	}
	repoPath, err := store.Create(context.Background(), "acme/widget")
	if err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	backend := &GitBin{Git: store.GitPath()}
	if err := backend.Serve(context.Background(), Request{
		RepoPath:  repoPath,
		Service:   ServiceUploadPack,
		Advertise: true,
		Stateless: true,
		Body:      strings.NewReader(""),
		Out:       &out,
	}); err != nil {
		t.Fatalf("Serve: %v", err)
	}
	if out.Len() == 0 {
		t.Fatal("real git produced no advertisement")
	}
}
