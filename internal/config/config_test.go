package config

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

func env(values map[string]string) func(string) string {
	return func(key string) string { return values[key] }
}

func TestDefaults(t *testing.T) {
	cfg, err := Parse([]string{"--repo-root", "/srv/repos"}, env(nil))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if cfg.Hostname != "tsgit" {
		t.Errorf("Hostname = %q", cfg.Hostname)
	}
	if cfg.Port != 443 || cfg.HTTPPort != 80 {
		t.Errorf("ports = %d/%d", cfg.Port, cfg.HTTPPort)
	}
	if !cfg.HTTPS {
		t.Error("HTTPS should default to true")
	}
	if cfg.DefaultBranch != "main" || cfg.Log != "info" {
		t.Errorf("branch/log = %q/%q", cfg.DefaultBranch, cfg.Log)
	}
	if cfg.ControlURL == "" {
		t.Error("ControlURL must have a default")
	}
	if cfg.Funnel {
		t.Error("Funnel must default to off")
	}
	if cfg.FunnelMaxConcurrent != 4 {
		t.Errorf("FunnelMaxConcurrent = %d, want 4", cfg.FunnelMaxConcurrent)
	}
	if !strings.Contains(cfg.StateDir, "tsnet-tsgit") {
		t.Errorf("StateDir = %q", cfg.StateDir)
	}
	if cfg.RepoRoot != "/srv/repos" {
		t.Errorf("RepoRoot = %q", cfg.RepoRoot)
	}
}

func TestEnvironmentFallbacks(t *testing.T) {
	cfg, err := Parse(nil, env(map[string]string{
		"TSGIT_HOSTNAME":   "app-host",
		"TS_HOSTNAME":      "generic-host",
		"TSGIT_PORT":       "8443",
		"TSGIT_HTTP_PORT":  "8080",
		"TSGIT_REPO_ROOT":  "/env/repos",
		"TSGIT_HTTPS":      "false",
		"TSGIT_LOG":        "debug",
		"TSGIT_DEV_LISTEN": "",
	}))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if cfg.Hostname != "app-host" {
		t.Errorf("app-prefixed environment should win, got %q", cfg.Hostname)
	}
	if cfg.Port != 8443 || cfg.HTTPPort != 8080 {
		t.Errorf("ports = %d/%d", cfg.Port, cfg.HTTPPort)
	}
	if cfg.HTTPS {
		t.Error("TSGIT_HTTPS=false was ignored")
	}
	if cfg.Log != "debug" {
		t.Errorf("Log = %q", cfg.Log)
	}

	cfg, err = Parse(nil, env(map[string]string{
		"TS_HOSTNAME":     "generic-host",
		"TSGIT_REPO_ROOT": "/env/repos",
	}))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if cfg.Hostname != "generic-host" {
		t.Errorf("TS_HOSTNAME fallback = %q", cfg.Hostname)
	}
}

func TestFlagsWinOverEnvironment(t *testing.T) {
	cfg, err := Parse([]string{
		"--hostname", "flag-host",
		"--port", "9443",
		"--repo-root", "/flag/repos",
	}, env(map[string]string{
		"TSGIT_HOSTNAME":  "env-host",
		"TSGIT_PORT":      "8443",
		"TSGIT_REPO_ROOT": "/env/repos",
	}))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if cfg.Hostname != "flag-host" || cfg.Port != 9443 || cfg.RepoRoot != "/flag/repos" {
		t.Fatalf("flags did not win: %+v", cfg)
	}
}

func TestFunnelConfiguration(t *testing.T) {
	cfg, err := Parse([]string{"--funnel=false", "--funnel-max-concurrent", "1"}, env(map[string]string{
		"TSGIT_FUNNEL":                "true",
		"TSGIT_FUNNEL_MAX_CONCURRENT": "2",
		"TSGIT_REPO_ROOT":             "/env/repos",
	}))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if cfg.Funnel {
		t.Error("--funnel=false did not win over TSGIT_FUNNEL=true")
	}
	if cfg.FunnelMaxConcurrent != 1 {
		t.Errorf("FunnelMaxConcurrent = %d, want the flag value 1", cfg.FunnelMaxConcurrent)
	}

	cfg, err = Parse(nil, env(map[string]string{
		"TSGIT_FUNNEL":                "true",
		"TSGIT_FUNNEL_MAX_CONCURRENT": "3",
		"TSGIT_REPO_ROOT":             "/env/repos",
	}))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if !cfg.Funnel || cfg.FunnelMaxConcurrent != 3 {
		t.Fatalf("environment funnel settings = %v/%d", cfg.Funnel, cfg.FunnelMaxConcurrent)
	}
}

func TestInvalidFunnelConcurrency(t *testing.T) {
	for _, args := range [][]string{
		{"--repo-root", "/srv/repos", "--funnel-max-concurrent", "0"},
		{"--repo-root", "/srv/repos", "--funnel-max-concurrent", "-1"},
	} {
		if _, err := Parse(args, env(nil)); err == nil || !strings.Contains(err.Error(), "--funnel-max-concurrent") {
			t.Errorf("Parse(%v) error = %v, want a positive-value error", args, err)
		}
	}
	if _, err := Parse(nil, env(map[string]string{
		"TSGIT_REPO_ROOT":             "/srv/repos",
		"TSGIT_FUNNEL_MAX_CONCURRENT": "0",
	})); err == nil || !strings.Contains(err.Error(), "--funnel-max-concurrent") {
		t.Errorf("environment zero = %v, want a positive-value error", err)
	}
}

func TestRepositoryRootRequired(t *testing.T) {
	_, err := Parse(nil, env(nil))
	if err == nil || !strings.Contains(err.Error(), "--repo-root") {
		t.Fatalf("expected required --repo-root error, got %v", err)
	}
}

func TestDevListenMakesRootOptional(t *testing.T) {
	cfg, err := Parse([]string{"--dev-listen", "127.0.0.1:0"}, env(nil))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if cfg.DevListen != "127.0.0.1:0" {
		t.Fatalf("DevListen = %q", cfg.DevListen)
	}
}

func TestAuthKeyFileConflictsWithEnvironmentKey(t *testing.T) {
	_, err := Parse([]string{
		"--repo-root", "/srv/repos",
		"--authkey-file", "/run/tskey",
	}, env(map[string]string{"TS_AUTHKEY": "tskey-secret"}))
	if err == nil || !strings.Contains(err.Error(), "mutually exclusive") {
		t.Fatalf("expected mutual exclusion error, got %v", err)
	}
}

func TestInvalidEnvironmentValues(t *testing.T) {
	for _, tc := range []map[string]string{
		{"TSGIT_PORT": "not-a-number", "TSGIT_REPO_ROOT": "/srv/repos"},
		{"TSGIT_HTTPS": "not-a-bool", "TSGIT_REPO_ROOT": "/srv/repos"},
	} {
		if _, err := Parse(nil, env(tc)); err == nil {
			t.Fatalf("expected error for %v", tc)
		}
	}
}

func TestUnknownFlagAndArgument(t *testing.T) {
	if _, err := Parse([]string{"--repo-root", "/srv/repos", "--nope"}, env(nil)); err == nil {
		t.Fatal("unknown flag must fail")
	}
	if _, err := Parse([]string{"--repo-root", "/srv/repos", "extra"}, env(nil)); err == nil {
		t.Fatal("extra argument must fail")
	}
}

func TestVersionFlag(t *testing.T) {
	cfg, err := Parse([]string{"--version"}, env(nil))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if !cfg.Version {
		t.Fatal("--version did not set Version")
	}
}

func TestPrepareDevCreatesRepositoryRoot(t *testing.T) {
	dev, err := PrepareDev()
	if err != nil {
		t.Fatalf("PrepareDev: %v", err)
	}
	info, err := os.Stat(dev.RepoRoot)
	if err != nil {
		t.Fatalf("temporary repository root missing: %v", err)
	}
	if !info.IsDir() {
		t.Fatal("temporary repository root is not a directory")
	}
	root := dev.RepoRoot
	dev.Cleanup()
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Fatalf("Cleanup left %s behind: %v", root, err)
	}
}

// TestDocumentedFlagsAreAccepted reads docs/configuration.md and parses every
// flag it documents, so the table cannot drift from the parser.
func TestDocumentedFlagsAreAccepted(t *testing.T) {
	data, err := os.ReadFile("../../docs/configuration.md")
	if err != nil {
		t.Fatalf("read configuration doc: %v", err)
	}
	flagRE := regexp.MustCompile(`--([a-z][a-z-]*)`)
	seen := map[string]bool{}
	values := map[string]string{
		"port":                  "8443",
		"http-port":             "8080",
		"https":                 "true",
		"funnel-max-concurrent": "2",
		"log":                   "info",
		"state-dir":             "/tmp/tsgit-state",
		"repo-root":             "/tmp/tsgit-repos",
		"dev-listen":            "127.0.0.1:0",
		"default-branch":        "main",
	}
	boolean := map[string]bool{
		"https":       true,
		"funnel":      true,
		"verbose":     true,
		"debug-tsnet": true,
		"version":     true,
	}
	for _, match := range flagRE.FindAllStringSubmatch(string(data), -1) {
		seen[match[1]] = true
	}
	if len(seen) == 0 {
		t.Fatal("configuration doc documents no flags")
	}
	for name := range seen {
		args := []string{"--repo-root", "/tmp/tsgit-repos", "--state-dir", "/tmp/tsgit-state"}
		if boolean[name] {
			args = append(args, "--"+name)
		} else {
			value, ok := values[name]
			if !ok {
				value = "example"
			}
			args = append(args, "--"+name, value)
		}
		if _, err := Parse(args, env(nil)); err != nil {
			t.Errorf("documented flag --%s is not accepted: %v", name, err)
		}
	}
}
