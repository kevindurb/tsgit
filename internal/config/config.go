// Package config parses tsgit's command-line flags and their environment
// fallbacks. Flags always win over the environment.
package config

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"tailscale.com/ipn"
)

// Config is the resolved runtime configuration.
type Config struct {
	Hostname      string
	Port          int
	HTTPPort      int
	StateDir      string
	RepoRoot      string
	AdvertiseTags string
	AuthKeyFile   string
	ControlURL    string
	HTTPS         bool
	DefaultBranch string
	DevListen     string
	Log           string
	Verbose       bool
	DebugTsnet    bool
	Version       bool
}

// AdvertiseTagList splits the comma-separated advertise-tags value.
func (c *Config) AdvertiseTagList() []string {
	var tags []string
	for _, tag := range strings.Split(c.AdvertiseTags, ",") {
		if tag = strings.TrimSpace(tag); tag != "" {
			tags = append(tags, tag)
		}
	}
	return tags
}

// Parse resolves args and the environment into a Config. getenv is injected so
// tests do not depend on the process environment.
func Parse(args []string, getenv func(string) string) (*Config, error) {
	cfg, err := defaults(getenv)
	if err != nil {
		return nil, err
	}

	fs := flag.NewFlagSet("tsgit serve", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.StringVar(&cfg.Hostname, "hostname", cfg.Hostname, "tailnet hostname to advertise")
	fs.IntVar(&cfg.Port, "port", cfg.Port, "tailnet HTTPS port")
	fs.IntVar(&cfg.HTTPPort, "http-port", cfg.HTTPPort, "tailnet HTTP port")
	fs.StringVar(&cfg.StateDir, "state-dir", cfg.StateDir, "tsnet state directory")
	fs.StringVar(&cfg.RepoRoot, "repo-root", cfg.RepoRoot, "root of the bare repository tree")
	fs.StringVar(&cfg.AdvertiseTags, "advertise-tags", cfg.AdvertiseTags, "comma-separated ACL tags to advertise")
	fs.StringVar(&cfg.AuthKeyFile, "authkey-file", cfg.AuthKeyFile, "file containing a Tailscale auth key")
	fs.StringVar(&cfg.ControlURL, "control-url", cfg.ControlURL, "coordination server URL")
	fs.BoolVar(&cfg.HTTPS, "https", cfg.HTTPS, "serve HTTPS when the tailnet permits it")
	fs.StringVar(&cfg.DefaultBranch, "default-branch", cfg.DefaultBranch, "initial branch of created repositories")
	fs.StringVar(&cfg.DevListen, "dev-listen", cfg.DevListen, "localhost address for development mode")
	fs.StringVar(&cfg.Log, "log", cfg.Log, "log level: debug, info, warn, error")
	fs.BoolVar(&cfg.Verbose, "verbose", cfg.Verbose, "verbose logging")
	fs.BoolVar(&cfg.DebugTsnet, "debug-tsnet", cfg.DebugTsnet, "log tsnet internals")
	fs.BoolVar(&cfg.Version, "version", cfg.Version, "print the version and exit")

	if err := fs.Parse(args); err != nil {
		return nil, err
	}
	if fs.NArg() > 0 {
		return nil, fmt.Errorf("unexpected argument %q", fs.Arg(0))
	}

	if err := cfg.validate(getenv); err != nil {
		return nil, err
	}
	return cfg, nil
}

func defaults(getenv func(string) string) (*Config, error) {
	stateDir, err := defaultStateDir(getenv)
	if err != nil {
		return nil, err
	}
	https, err := boolEnv(getenv, "TSGIT_HTTPS", true)
	if err != nil {
		return nil, err
	}
	port, err := intEnv(getenv, "TSGIT_PORT", 443)
	if err != nil {
		return nil, err
	}
	httpPort, err := intEnv(getenv, "TSGIT_HTTP_PORT", 80)
	if err != nil {
		return nil, err
	}
	return &Config{
		Hostname:      envOr(getenv, "tsgit", "TSGIT_HOSTNAME", "TS_HOSTNAME"),
		Port:          port,
		HTTPPort:      httpPort,
		StateDir:      stateDir,
		RepoRoot:      envOr(getenv, "", "TSGIT_REPO_ROOT"),
		AdvertiseTags: envOr(getenv, "", "TS_ADVERTISE_TAGS"),
		AuthKeyFile:   envOr(getenv, "", "TS_AUTHKEY_FILE"),
		ControlURL:    envOr(getenv, ipn.DefaultControlURL, "TS_CONTROL_URL"),
		HTTPS:         https,
		DefaultBranch: envOr(getenv, "main", "TSGIT_DEFAULT_BRANCH"),
		DevListen:     envOr(getenv, "", "TSGIT_DEV_LISTEN"),
		Log:           envOr(getenv, "info", "TSGIT_LOG"),
	}, nil
}

func (c *Config) validate(getenv func(string) string) error {
	if c.Version {
		return nil
	}
	if c.Port < 1 || c.Port > 65535 {
		return fmt.Errorf("--port must be between 1 and 65535, got %d", c.Port)
	}
	if c.HTTPPort < 1 || c.HTTPPort > 65535 {
		return fmt.Errorf("--http-port must be between 1 and 65535, got %d", c.HTTPPort)
	}
	if c.Hostname == "" {
		return errors.New("--hostname must not be empty")
	}
	if c.StateDir == "" {
		return errors.New("--state-dir must not be empty")
	}
	if c.DefaultBranch == "" {
		return errors.New("--default-branch must not be empty")
	}
	switch strings.ToLower(c.Log) {
	case "debug", "info", "warn", "error":
	default:
		return fmt.Errorf("--log must be debug, info, warn, or error, got %q", c.Log)
	}
	if c.AuthKeyFile != "" && getenv("TS_AUTHKEY") != "" {
		return errors.New("--authkey-file and TS_AUTHKEY are mutually exclusive")
	}
	if c.DevListen == "" && c.RepoRoot == "" {
		return errors.New("--repo-root is required unless --dev-listen is set")
	}
	return nil
}

// DefaultBranch returns the resolved branch name. It is a small convenience
// for callers that prefer not to reach into the field.
func (c *Config) DefaultBranchName() string {
	if c.DefaultBranch == "" {
		return "main"
	}
	return c.DefaultBranch
}

// DevDir is the temporary repository root used by --dev-listen.
type DevDir struct {
	RepoRoot string
}

// Cleanup removes the temporary root.
func (d *DevDir) Cleanup() {
	if d != nil && d.RepoRoot != "" {
		_ = os.RemoveAll(d.RepoRoot)
	}
}

// PrepareDev creates the temporary repository root used by development mode.
// The caller owns the returned directory and must clean it up.
func PrepareDev() (*DevDir, error) {
	dir, err := os.MkdirTemp("", "tsgit-dev-repos-*")
	if err != nil {
		return nil, fmt.Errorf("create development repository root: %w", err)
	}
	if err := os.Chmod(dir, 0o750); err != nil {
		_ = os.RemoveAll(dir)
		return nil, fmt.Errorf("chmod development repository root: %w", err)
	}
	return &DevDir{RepoRoot: dir}, nil
}

func envOr(getenv func(string) string, fallback string, names ...string) string {
	for _, name := range names {
		if v := getenv(name); v != "" {
			return v
		}
	}
	return fallback
}

func intEnv(getenv func(string) string, name string, fallback int) (int, error) {
	v := getenv(name)
	if v == "" {
		return fallback, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return 0, fmt.Errorf("%s must be an integer: %w", name, err)
	}
	return n, nil
}

func boolEnv(getenv func(string) string, name string, fallback bool) (bool, error) {
	v := getenv(name)
	if v == "" {
		return fallback, nil
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return false, fmt.Errorf("%s must be a boolean: %w", name, err)
	}
	return b, nil
}

func defaultStateDir(getenv func(string) string) (string, error) {
	if v := envOr(getenv, "", "TSGIT_STATE_DIR", "TS_STATE_DIR"); v != "" {
		return v, nil
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("resolve default state directory: %w", err)
	}
	return filepath.Join(dir, "tsnet-tsgit"), nil
}
