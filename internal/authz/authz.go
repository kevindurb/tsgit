// Package authz decides whether a tailnet identity may read, write, create, or
// delete a canonical repository. It works on plain values only: an identity, a
// repository name, and an operation. It never sees a connection, an HTTP
// request, or a Tailscale type, which keeps the decision testable without a
// tailnet.
package authz

import (
	"bytes"
	"encoding/json"
	"fmt"
	"path"
)

// CapRepo is the Tailscale app capability name that carries repository grants.
// It is a public contract: changing it invalidates every tailnet policy that
// grants repository access.
const CapRepo = "github.com/kevindurb/tsgit/cap/repo"

// CapFunnel is the node attribute (nodeAttrs.app) that lists repositories
// exposed read-only to the public internet. It is read from the node's own
// capability map, never from a peer's grants.
const CapFunnel = "github.com/kevindurb/tsgit/cap/funnel"

// FunnelNode is the synthetic principal used for public Funnel requests.
const FunnelNode = "funnel"

// Access is the level a grant confers.
type Access int

const (
	AccessNone Access = iota
	AccessRead
	AccessWrite
	AccessAdmin
)

func (a Access) String() string {
	switch a {
	case AccessRead:
		return "read"
	case AccessWrite:
		return "write"
	case AccessAdmin:
		return "admin"
	default:
		return "none"
	}
}

// UnmarshalJSON accepts only the two documented level names. An unknown value
// fails the whole payload so no partial grant can be applied.
func (a *Access) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return fmt.Errorf("access must be a string: %w", err)
	}
	switch s {
	case "read":
		*a = AccessRead
	case "write":
		*a = AccessWrite
	case "admin":
		*a = AccessAdmin
	default:
		return fmt.Errorf("unknown access %q", s)
	}
	return nil
}

// Grant is one capability entry: an access level over repository patterns.
// Patterns use path.Match, so * does not cross a slash.
type Grant struct {
	Repos  []string `json:"repos"`
	Access Access   `json:"access"`
}

// FunnelGrant is one cap/funnel entry. It has no access field: public access is
// read-only by construction, so a payload cannot express a public write. An
// unknown field, including an "access" copied from a cap/repo grant, makes the
// entry invalid and therefore grants nothing.
type FunnelGrant struct {
	Repos []string `json:"repos"`
}

// UnmarshalJSON rejects fields outside the public grant shape.
func (g *FunnelGrant) UnmarshalJSON(b []byte) error {
	type publicGrant struct {
		Repos []string `json:"repos"`
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	var v publicGrant
	if err := dec.Decode(&v); err != nil {
		return fmt.Errorf("cap/funnel entry: %w", err)
	}
	g.Repos = v.Repos
	return nil
}

// PublicIdentity builds the synthetic read-only principal from public grants.
// Every grant is AccessRead regardless of the payload, so Decide reuses the
// tailnet path and CanCreate stays false.
func PublicIdentity(grants []FunnelGrant) Identity {
	id := Identity{Node: FunnelNode}
	for _, g := range grants {
		id.Grants = append(id.Grants, Grant{Repos: g.Repos, Access: AccessRead})
	}
	return id
}

// Identity is who made a request, according to the tailnet. Login is optional
// because a tagged node may not have a user behind it.
type Identity struct {
	Node   string
	Login  string
	Tags   []string
	Grants []Grant
}

// Op is the action an identity wants to take.
type Op string

const (
	OpRead   Op = "read"
	OpWrite  Op = "write"
	OpCreate Op = "create"
	OpDelete Op = "delete"
)

// Decision is the result of an authorization check. CanCreate is true when a
// write or admin grant matches the canonical name; CanDelete is true only for
// an admin grant.
type Decision struct {
	Allow     bool
	Access    Access
	CanCreate bool
	CanDelete bool
	Reason    string
}

// Decide returns the decision for identity id taking op on the canonical
// repository name repo. Grants union, the highest level wins, write implies
// read, and no matching pattern denies.
func Decide(id Identity, repo string, op Op) Decision {
	var best Access
	for _, g := range id.Grants {
		for _, pattern := range g.Repos {
			ok, err := path.Match(pattern, repo)
			if err != nil {
				// A malformed pattern grants nothing; the rest of the
				// payload is not trusted to compensate for it.
				continue
			}
			if ok && g.Access > best {
				best = g.Access
			}
		}
	}

	d := Decision{
		Access:    best,
		CanCreate: best >= AccessWrite,
		CanDelete: best >= AccessAdmin,
	}
	switch op {
	case OpRead:
		d.Allow = best >= AccessRead
	case OpWrite, OpCreate:
		d.Allow = best >= AccessWrite
	case OpDelete:
		d.Allow = best >= AccessAdmin
	default:
		d.Allow = false
	}

	if !d.Allow {
		d.Reason = fmt.Sprintf("no %s capability for %s", op, repo)
	}
	return d
}
