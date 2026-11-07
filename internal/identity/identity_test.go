package identity

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"testing"

	"tailscale.com/client/tailscale/apitype"
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
