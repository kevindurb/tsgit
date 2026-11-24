// Package hooks delivers push notifications to operator-configured HTTP
// endpoints (CI systems) declared in the tailnet policy. Configuration lives
// in the node's app-capability map as github.com/kevindurb/tsgit/cap/hooks;
// delivery goes out over the tailnet itself, never the host network. Secrets
// never appear in the policy: a hook may name an environment variable whose
// value is read at delivery time.
package hooks

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/url"
	"path"
	"strings"

	"tailscale.com/tailcfg"
	"tailscale.com/tailcfg/nodecap"
)

// CapHooks is the app capability that carries webhook configuration on the
// tsgit node's nodeAttrs. It is a public contract: changing it invalidates
// every policy that configures hooks.
const CapHooks = "github.com/kevindurb/tsgit/cap/hooks"

// Hook is one validated cap/hooks entry.
type Hook struct {
	// Repos lists path.Match patterns matched against the canonical
	// <namespace>/<repo> name.
	Repos []string
	// URL is the delivery endpoint; http or https only.
	URL string
	// SecretEnv, when non-empty, names the environment variable holding the
	// HMAC-SHA256 key used to sign deliveries. The value is never read from
	// the policy and never logged.
	SecretEnv string
}

// Matches reports whether the hook applies to the canonical repository name.
func (h Hook) Matches(repo string) bool {
	for _, pattern := range h.Repos {
		if ok, err := path.Match(pattern, repo); err == nil && ok {
			return true
		}
	}
	return false
}

type hookJSON struct {
	Repos     []string `json:"repos"`
	URL       string   `json:"url"`
	SecretEnv string   `json:"secretEnv"`
}

func parseHook(raw string) (Hook, error) {
	var h hookJSON
	dec := json.NewDecoder(strings.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&h); err != nil {
		return Hook{}, fmt.Errorf("cap/hooks entry: %w", err)
	}
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		return Hook{}, errors.New("cap/hooks entry: trailing data after JSON object")
	}
	if len(h.Repos) == 0 {
		return Hook{}, errors.New("cap/hooks entry: repos must list at least one pattern")
	}
	for _, pattern := range h.Repos {
		if strings.TrimSpace(pattern) == "" {
			return Hook{}, errors.New("cap/hooks entry: repos pattern is empty")
		}
		if _, err := path.Match(pattern, "probe/name"); err != nil {
			return Hook{}, fmt.Errorf("cap/hooks entry: repos pattern %q: %w", pattern, err)
		}
	}
	u, err := url.Parse(h.URL)
	if err != nil {
		return Hook{}, fmt.Errorf("cap/hooks entry: url: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return Hook{}, fmt.Errorf("cap/hooks entry: unsupported url scheme %q (want http or https)", u.Scheme)
	}
	if u.Host == "" {
		return Hook{}, errors.New("cap/hooks entry: url has no host")
	}
	if u.User != nil {
		return Hook{}, errors.New("cap/hooks entry: url must not embed userinfo")
	}
	if h.SecretEnv != "" {
		if !isEnvName(h.SecretEnv) {
			return Hook{}, fmt.Errorf("cap/hooks entry: secretEnv %q is not a valid environment variable name", h.SecretEnv)
		}
	}
	return Hook{Repos: h.Repos, URL: h.URL, SecretEnv: h.SecretEnv}, nil
}

func isEnvName(s string) bool {
	for i, r := range s {
		isFirst := i == 0
		isUpper := r >= 'A' && r <= 'Z'
		isDigit := r >= '0' && r <= '9'
		if isFirst {
			if !isUpper && r != '_' {
				return false
			}
			continue
		}
		if !isUpper && !isDigit && r != '_' {
			return false
		}
	}
	return len(s) > 0
}

// ParseCap decodes the cap/hooks node capability. Invalid entries are logged
// and skipped; the remaining entries still take effect. A missing capability
// yields nil hooks and no error.
func ParseCap(capMap tailcfg.NodeCapMap, log *slog.Logger) []Hook {
	if log == nil {
		log = slog.Default()
	}
	raw := capMap[nodecap.Cap(CapHooks)]
	hooks := make([]Hook, 0, len(raw))
	for i, entry := range raw {
		hook, err := parseHook(string(entry))
		if err != nil {
			log.Warn("ignoring invalid cap/hooks entry", "index", i, "error", err)
			continue
		}
		hooks = append(hooks, hook)
	}
	return hooks
}
