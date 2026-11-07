package identity

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"strings"
	"testing"

	"tailscale.com/client/tailscale/apitype"
	"tailscale.com/ipn/ipnstate"
	"tailscale.com/tailcfg"

	"github.com/kevindurb/tsgit/internal/authz"
)

type fakeWhoIs struct {
	resp *apitype.WhoIsResponse
	err  error
}

func (f fakeWhoIs) WhoIs(context.Context, string) (*apitype.WhoIsResponse, error) {
	return f.resp, f.err
}

func node() *tailcfg.Node {
	return &tailcfg.Node{
		StableID: "n123",
		Name:     "host.tailnet.ts.net.",
		Tags:     []string{"tag:ci"},
	}
}

func capMap(payloads ...string) tailcfg.PeerCapMap {
	values := make([]tailcfg.RawMessage, len(payloads))
	for i, payload := range payloads {
		values[i] = tailcfg.RawMessage(payload)
	}
	return tailcfg.PeerCapMap{authz.CapRepo: values}
}

func TestResolveKnownPeerWithCapabilities(t *testing.T) {
	client := fakeWhoIs{resp: &apitype.WhoIsResponse{
		Node:        node(),
		UserProfile: &tailcfg.UserProfile{LoginName: "alice@example.com"},
		CapMap:      capMap(`{"repos":["acme/*"],"access":"write"}`),
	}}
	id, err := NewLocal(client, slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil))).Resolve(context.Background(), "100.64.0.1:1234")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if id.Node != "n123" || id.Login != "alice@example.com" {
		t.Fatalf("identity = %+v", id)
	}
	if len(id.Tags) != 1 || id.Tags[0] != "tag:ci" {
		t.Fatalf("tags = %v", id.Tags)
	}
	if got := authz.Decide(id, "acme/widget", authz.OpWrite); !got.Allow {
		t.Fatal("resolved write capability did not authorize")
	}
}

func TestResolveWithoutCapability(t *testing.T) {
	client := fakeWhoIs{resp: &apitype.WhoIsResponse{
		Node:        node(),
		UserProfile: &tailcfg.UserProfile{LoginName: "alice@example.com"},
	}}
	id, err := NewLocal(client, nil).Resolve(context.Background(), "100.64.0.1:1234")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if len(id.Grants) != 0 {
		t.Fatalf("grants = %v, want empty", id.Grants)
	}
	if got := authz.Decide(id, "acme/widget", authz.OpRead); got.Allow {
		t.Fatal("identity without a capability was allowed")
	}
}

func TestResolveMalformedCapabilityGrantsNothing(t *testing.T) {
	var logs bytes.Buffer
	client := fakeWhoIs{resp: &apitype.WhoIsResponse{
		Node:   node(),
		CapMap: capMap(`{"repos":["acme/*"],"access":"write"}`, `not json`),
	}}
	id, err := NewLocal(client, slog.New(slog.NewTextHandler(&logs, nil))).Resolve(context.Background(), "100.64.0.1:1234")
	if err != nil {
		t.Fatalf("Resolve must not fail on a malformed capability: %v", err)
	}
	if len(id.Grants) != 0 {
		t.Fatalf("partial grants applied: %v", id.Grants)
	}
	if !bytes.Contains(logs.Bytes(), []byte("malformed repository capability")) {
		t.Fatalf("malformed capability was not logged: %s", logs.String())
	}
}

func TestResolveUnknownAccessGrantsNothing(t *testing.T) {
	client := fakeWhoIs{resp: &apitype.WhoIsResponse{
		Node:   node(),
		CapMap: capMap(`{"repos":["acme/*"],"access":"admin"}`),
	}}
	id, err := NewLocal(client, nil).Resolve(context.Background(), "100.64.0.1:1234")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if len(id.Grants) != 0 {
		t.Fatalf("unknown access produced grants: %v", id.Grants)
	}
}

func TestResolveTaggedNodeWithoutLogin(t *testing.T) {
	client := fakeWhoIs{resp: &apitype.WhoIsResponse{
		Node:   node(),
		CapMap: capMap(`{"repos":["acme/*"],"access":"read"}`),
	}}
	id, err := NewLocal(client, nil).Resolve(context.Background(), "100.64.0.1:1234")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if id.Login != "" {
		t.Fatalf("tagged node has login %q", id.Login)
	}
	if got := authz.Decide(id, "acme/widget", authz.OpRead); !got.Allow {
		t.Fatal("tagged node was not allowed to read")
	}
}

func TestResolveFailures(t *testing.T) {
	if _, err := NewLocal(fakeWhoIs{err: errors.New("unavailable")}, nil).Resolve(context.Background(), "x"); err == nil {
		t.Fatal("WhoIs failure must be returned")
	}
	if _, err := NewLocal(fakeWhoIs{resp: &apitype.WhoIsResponse{}}, nil).Resolve(context.Background(), "x"); err == nil {
		t.Fatal("missing node must be an error")
	}
}

func TestFixedResolver(t *testing.T) {
	id := authz.Identity{Node: "fixed"}
	got, err := (Fixed{Identity: id}).Resolve(context.Background(), "anything")
	if err != nil || got.Node != "fixed" {
		t.Fatalf("Fixed.Resolve = %+v, %v", got, err)
	}
}

func TestDevelopmentIdentityIsWriter(t *testing.T) {
	id := Development()
	if got := authz.Decide(id, "acme/widget", authz.OpWrite); !got.Allow || !got.CanCreate {
		t.Fatalf("development identity cannot write: %+v", got)
	}
}

type fakeStatus struct {
	status *ipnstate.Status
	err    error
}

func (f fakeStatus) StatusWithoutPeers(context.Context) (*ipnstate.Status, error) {
	return f.status, f.err
}

func funnelStatus(payloads ...string) *ipnstate.Status {
	values := make([]tailcfg.RawMessage, len(payloads))
	for i, payload := range payloads {
		values[i] = tailcfg.RawMessage(payload)
	}
	return &ipnstate.Status{Self: &ipnstate.PeerStatus{
		ID:     "n123",
		CapMap: tailcfg.NodeCapMap{authz.CapFunnel: values},
	}}
}

func TestFunnelResolveReadOnlyGrants(t *testing.T) {
	resolver := NewFunnel(fakeStatus{status: funnelStatus(`{"repos":["public/*","shared/docs"]}`)}, nil)
	id, err := resolver.Resolve(context.Background(), "203.0.113.9:5555")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if id.Node != authz.FunnelNode || id.Login != "" || len(id.Tags) != 0 {
		t.Fatalf("public identity = %+v", id)
	}
	if got := authz.Decide(id, "public/widget", authz.OpRead); !got.Allow {
		t.Fatal("public repository was not readable")
	}
	if got := authz.Decide(id, "public/widget", authz.OpWrite); got.Allow || got.CanCreate {
		t.Fatalf("public write allowed: %+v", got)
	}
	if got := authz.Decide(id, "public/widget", authz.OpCreate); got.Allow || got.CanCreate {
		t.Fatalf("public create allowed: %+v", got)
	}
	if got := authz.Decide(id, "other/secret", authz.OpRead); got.Allow {
		t.Fatal("repository outside the public patterns was readable")
	}
}

func TestFunnelResolveUsesCapabilityRegardlessOfAddress(t *testing.T) {
	// 203.0.113.9 is not a tailnet peer address. A Funnel request must still
	// resolve from the node capability, and the resolver has no WhoIs method.
	resolver := NewFunnel(fakeStatus{status: funnelStatus(`{"repos":["public/*"]}`)}, nil)
	for _, addr := range []string{"203.0.113.9:5555", "", "not-an-address"} {
		id, err := resolver.Resolve(context.Background(), addr)
		if err != nil {
			t.Fatalf("Resolve(%q): %v", addr, err)
		}
		if got := authz.Decide(id, "public/widget", authz.OpRead); !got.Allow {
			t.Fatalf("Resolve(%q) lost the public grants", addr)
		}
	}
}

func TestFunnelResolveMissingCapability(t *testing.T) {
	resolver := NewFunnel(fakeStatus{status: &ipnstate.Status{Self: &ipnstate.PeerStatus{ID: "n123"}}}, nil)
	id, err := resolver.Resolve(context.Background(), "203.0.113.9:5555")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if len(id.Grants) != 0 {
		t.Fatalf("missing capability produced grants: %v", id.Grants)
	}
	if got := authz.Decide(id, "public/widget", authz.OpRead); got.Allow {
		t.Fatal("missing capability still allowed a read")
	}
}

func TestFunnelResolveMalformedCapabilityGrantsNothing(t *testing.T) {
	var logs bytes.Buffer
	resolver := NewFunnel(
		fakeStatus{status: funnelStatus(`{"repos":["public/*"]}`, `not json`)},
		slog.New(slog.NewTextHandler(&logs, nil)),
	)
	id, err := resolver.Resolve(context.Background(), "203.0.113.9:5555")
	if err != nil {
		t.Fatalf("Resolve must not fail on a malformed capability: %v", err)
	}
	if len(id.Grants) != 0 {
		t.Fatalf("partial public grants applied: %v", id.Grants)
	}
	if !bytes.Contains(logs.Bytes(), []byte("malformed public funnel capability")) {
		t.Fatalf("malformed capability was not logged: %s", logs.String())
	}
}

func TestFunnelResolveRejectsAccessField(t *testing.T) {
	var logs bytes.Buffer
	resolver := NewFunnel(
		fakeStatus{status: funnelStatus(`{"repos":["public/*"],"access":"write"}`)},
		slog.New(slog.NewTextHandler(&logs, nil)),
	)
	id, err := resolver.Resolve(context.Background(), "203.0.113.9:5555")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if len(id.Grants) != 0 {
		t.Fatalf("a public payload with an access field became public: %v", id.Grants)
	}
	if !bytes.Contains(logs.Bytes(), []byte("malformed public funnel capability")) {
		t.Fatalf("rejected payload was not logged: %s", logs.String())
	}
}

func TestFunnelResolveStatusFailure(t *testing.T) {
	resolver := NewFunnel(fakeStatus{err: errors.New("localapi down")}, nil)
	if _, err := resolver.Resolve(context.Background(), "203.0.113.9:5555"); err == nil {
		t.Fatal("status failure must be returned")
	}
	if _, err := NewFunnel(fakeStatus{status: &ipnstate.Status{}}, nil).Resolve(context.Background(), "x"); err == nil {
		t.Fatal("status without a self node must be an error")
	}
}

// TestDocumentedFunnelFixture keeps docs/access-control.md honest: the
// documented cap/funnel payload must parse into real read-only grants.
func TestDocumentedFunnelFixture(t *testing.T) {
	const marker = "<!-- funnel-capability-fixture -->"
	data, err := os.ReadFile("../../docs/access-control.md")
	if err != nil {
		t.Fatalf("read access-control doc: %v", err)
	}
	text := string(data)
	idx := strings.Index(text, marker)
	if idx < 0 {
		t.Fatalf("access-control doc is missing %s", marker)
	}
	rest := text[idx+len(marker):]
	start := strings.Index(rest, "```json")
	if start < 0 {
		t.Fatal("funnel fixture is not a json code block")
	}
	rest = rest[start+len("```json"):]
	end := strings.Index(rest, "```")
	if end < 0 {
		t.Fatal("funnel fixture code block is not closed")
	}
	var entries []authz.FunnelGrant
	if err := json.Unmarshal([]byte(rest[:end]), &entries); err != nil {
		t.Fatalf("documented funnel fixture does not parse: %v", err)
	}
	if len(entries) != 1 || len(entries[0].Repos) != 2 {
		t.Fatalf("documented funnel fixture = %+v", entries)
	}
	id := authz.PublicIdentity(entries)
	if got := authz.Decide(id, "public/widget", authz.OpRead); !got.Allow {
		t.Fatal("documented funnel pattern does not allow public/widget")
	}
	if got := authz.Decide(id, "public/widget", authz.OpWrite); got.Allow {
		t.Fatal("documented funnel grant allowed a write")
	}
}
