package hooks

import (
	"context"
	"errors"
	"strings"
	"testing"

	"tailscale.com/ipn/ipnstate"
	"tailscale.com/tailcfg"
	"tailscale.com/tailcfg/nodecap"
)

func TestParseHookValid(t *testing.T) {
	h, err := parseHook(`{"repos": ["acme/*"], "url": "http://ci.local:8080/hook", "secretEnv": "TSC_HOOK_CI"}`)
	if err != nil {
		t.Fatalf("parseHook: %v", err)
	}
	if len(h.Repos) != 1 || h.Repos[0] != "acme/*" {
		t.Errorf("repos = %v", h.Repos)
	}
	if h.URL != "http://ci.local:8080/hook" {
		t.Errorf("url = %q", h.URL)
	}
	if h.SecretEnv != "TSC_HOOK_CI" {
		t.Errorf("secretEnv = %q", h.SecretEnv)
	}
}

func TestParseHookRejections(t *testing.T) {
	cases := []struct {
		name, raw string
	}{
		{"unknown field", `{"repos": ["*/*"], "url": "http://ci/", "access": "write"}`},
		{"empty repos", `{"repos": [], "url": "http://ci/"}`},
		{"empty pattern", `{"repos": ["  "], "url": "http://ci/"}`},
		{"bad pattern", `{"repos": ["[unclosed"], "url": "http://ci/"}`},
		{"missing url", `{"repos": ["*/*"]}`},
		{"relative url", `{"repos": ["*/*"], "url": "/hook"}`},
		{"unsupported scheme", `{"repos": ["*/*"], "url": "ftp://ci/hook"}`},
		{"userinfo", `{"repos": ["*/*"], "url": "http://user:pass@ci/hook"}`},
		{"secretEnv lowercase", `{"repos": ["*/*"], "url": "http://ci/", "secretEnv": "lowercase"}`},
		{"secretEnv with dash", `{"repos": ["*/*"], "url": "http://ci/", "secretEnv": "MY-HOOK"}`},
		{"secretEnv with leading digit", `{"repos": ["*/*"], "url": "http://ci/", "secretEnv": "1HOOK"}`},
		{"not an object", `["not", "an", "object"]`},
		{"trailing data", `{"repos": ["*/*"], "url": "http://ci/"} extra`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := parseHook(tc.raw); err == nil {
				t.Fatalf("parseHook(%s) succeeded, want error", tc.raw)
			}
		})
	}
}

func TestParseHookAllowsHTTPSAndQuery(t *testing.T) {
	h, err := parseHook(`{"repos": ["*/*"], "url": "https://ci.example.ts.net:8443/hook?token=abc"}`)
	if err != nil {
		t.Fatalf("parseHook: %v", err)
	}
	if !strings.Contains(h.URL, "token=abc") {
		t.Errorf("url = %q", h.URL)
	}
}

func TestHookMatches(t *testing.T) {
	h := Hook{Repos: []string{"acme/*", "public/docs"}}
	for _, repo := range []string{"acme/widget", "acme/gadget", "public/docs"} {
		if !h.Matches(repo) {
			t.Errorf("Matches(%q) = false, want true", repo)
		}
	}
	for _, repo := range []string{"other/widget", "acme/nested/widget", "public/doc"} {
		if h.Matches(repo) {
			t.Errorf("Matches(%q) = true, want false", repo)
		}
	}
}

func TestParseCapSkipsInvalidEntries(t *testing.T) {
	capMap := tailcfg.NodeCapMap{
		nodecap.Cap(CapHooks): {
			`{"repos": ["acme/*"], "url": "http://ci-one/hook"}`,
			`{"repos": ["acme/*"], "url": "http://ci-two/hook", "bogus": true}`,
			`not json`,
			`{"repos": ["other/*"], "url": "http://ci-three/hook", "secretEnv": "TSC_HOOK"}`,
		},
	}
	hooks := ParseCap(capMap, nil)
	if len(hooks) != 2 {
		t.Fatalf("got %d hooks, want 2: %+v", len(hooks), hooks)
	}
	if hooks[0].URL != "http://ci-one/hook" || hooks[1].URL != "http://ci-three/hook" {
		t.Errorf("hooks = %+v", hooks)
	}
}

func TestParseCapMissingCapability(t *testing.T) {
	if got := ParseCap(nil, nil); len(got) != 0 {
		t.Fatalf("nil cap map gave %d hooks, want 0", len(got))
	}
	if got := ParseCap(tailcfg.NodeCapMap{}, nil); len(got) != 0 {
		t.Fatalf("empty cap map gave %d hooks, want 0", len(got))
	}
}

type fakeStatus struct {
	status *ipnstate.Status
	err    error
}

func (f fakeStatus) StatusWithoutPeers(context.Context) (*ipnstate.Status, error) {
	return f.status, f.err
}

func TestStatusLoaderReadsNodeCaps(t *testing.T) {
	capMap := tailcfg.NodeCapMap{
		nodecap.Cap(CapHooks): {
			`{"repos": ["*/*"], "url": "http://ci/", "secretEnv": "TSC_HOOK"}`,
		},
	}
	st := &ipnstate.Status{Self: &ipnstate.PeerStatus{CapMap: capMap}}
	load := StatusLoader(fakeStatus{status: st}, nil)
	hooks, err := load(context.Background())
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(hooks) != 1 {
		t.Fatalf("got %d hooks, want 1", len(hooks))
	}
	if hooks[0].SecretEnv != "TSC_HOOK" {
		t.Errorf("secretEnv = %q", hooks[0].SecretEnv)
	}
}

func TestStatusLoaderErrors(t *testing.T) {
	if _, err := StatusLoader(fakeStatus{err: errors.New("localapi down")}, nil)(context.Background()); err == nil {
		t.Fatal("want error when status read fails")
	}
	if _, err := StatusLoader(fakeStatus{status: &ipnstate.Status{}}, nil)(context.Background()); err == nil {
		t.Fatal("want error when self is missing")
	}
}
