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
			{Repos: []string{"*/*"}, Access: authz.AccessWrite},
		},
	}
}
