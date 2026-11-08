// Package identity turns an inbound connection into the tailnet identity behind
// it. The peer address comes from the listener; nothing the client sends is
// consulted.
package identity

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"tailscale.com/client/tailscale/apitype"
	"tailscale.com/ipn/ipnstate"
	"tailscale.com/tailcfg"

	"github.com/kevindurb/tsgit/internal/authz"
)

// Resolver resolves a peer address to an identity and its grants.
type Resolver interface {
	Resolve(ctx context.Context, remoteAddr string) (authz.Identity, error)
}

// WhoIser is the LocalAPI method the resolver needs. It is an interface so the
// resolver can be tested without a tailnet.
type WhoIser interface {
	WhoIs(ctx context.Context, remoteAddr string) (*apitype.WhoIsResponse, error)
}

// StatusReader is the LocalAPI method the public resolver needs. StatusWithoutPeers
// reports the node's own capabilities without the peer list.
type StatusReader interface {
	StatusWithoutPeers(ctx context.Context) (*ipnstate.Status, error)
}

// Local resolves identities through the embedded node's LocalAPI.
type Local struct {
	client WhoIser
	log    *slog.Logger
}

// NewLocal returns a resolver backed by client.
func NewLocal(client WhoIser, log *slog.Logger) *Local {
	if log == nil {
		log = slog.Default()
	}
	return &Local{client: client, log: log}
}

// Resolve calls WhoIs with the peer address and decodes the repository
// capability. A malformed capability payload grants nothing and is logged; it
// does not fail the lookup.
func (l *Local) Resolve(ctx context.Context, remoteAddr string) (authz.Identity, error) {
	who, err := l.client.WhoIs(ctx, remoteAddr)
	if err != nil {
		return authz.Identity{}, fmt.Errorf("whois %s: %w", remoteAddr, err)
	}
	if who == nil || who.Node == nil {
		return authz.Identity{}, errors.New("whois returned no node")
	}

	id := authz.Identity{Tags: append([]string(nil), who.Node.Tags...)}
	switch {
	case who.Node.StableID != "":
		id.Node = string(who.Node.StableID)
	default:
		id.Node = who.Node.Name
	}
	if who.UserProfile != nil {
		id.Login = who.UserProfile.LoginName
	}

	grants, err := tailcfg.UnmarshalCapJSON[authz.Grant](who.CapMap, authz.CapRepo)
	if err != nil {
		l.log.Warn("ignoring malformed repository capability",
			"node", id.Node,
			"capability", authz.CapRepo,
			"error", err,
		)
		return id, nil
	}
	id.Grants = grants
	return id, nil
}

// Funnel resolves public requests to a read-only principal built from the
// node's own cap/funnel attribute. It never calls WhoIs and ignores the peer
// address: a Funnel connection's remote address is Tailscale's relay, not the
// client, and public traffic has no tailnet identity.
type Funnel struct {
	status StatusReader
	log    *slog.Logger
}

// NewFunnel returns a resolver backed by client.
func NewFunnel(client StatusReader, log *slog.Logger) *Funnel {
	if log == nil {
		log = slog.Default()
	}
	return &Funnel{status: client, log: log}
}

// Resolve reads the node's capabilities on every request so a policy edit takes
// effect without a restart. A status failure is an error; a missing capability
// is an empty public set; a malformed capability grants nothing and is logged.
func (f *Funnel) Resolve(ctx context.Context, _ string) (authz.Identity, error) {
	st, err := f.status.StatusWithoutPeers(ctx)
	if err != nil {
		return authz.Identity{}, fmt.Errorf("read node status: %w", err)
	}
	if st == nil || st.Self == nil {
		return authz.Identity{}, errors.New("node status has no self node")
	}

	grants, err := tailcfg.UnmarshalNodeCapJSON[authz.FunnelGrant](st.Self.CapMap, authz.CapFunnel)
	if err != nil {
		f.log.Warn("ignoring malformed public funnel capability",
			"node", string(st.Self.ID),
			"capability", authz.CapFunnel,
			"error", err,
		)
		return authz.PublicIdentity(nil), nil
	}
	return authz.PublicIdentity(grants), nil
}

// Fixed returns the same identity for every address. Tests and development
// mode use it; production never does.
type Fixed struct {
	Identity authz.Identity
}

// Resolve returns the fixed identity.
func (f Fixed) Resolve(context.Context, string) (authz.Identity, error) {
	return f.Identity, nil
}

// Development is the identity used by --dev-listen: a local writer with access
// to every repository. It exists only so clone and push can be exercised
// without a tailnet.
func Development() authz.Identity {
	return authz.Identity{
		Node:  "development",
		Login: "development",
		Grants: []authz.Grant{
			{Repos: []string{"*/*"}, Access: authz.AccessAdmin},
		},
	}
}
