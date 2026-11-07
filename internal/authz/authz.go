// Package authz decides whether a tailnet identity may read, write, or create a
// canonical repository. It works on plain values only: an identity, a
// repository name, and an operation. It never sees a connection, an HTTP
// request, or a Tailscale type, which keeps the decision testable without a
// tailnet.
package authz

import (
	"encoding/json"
	"fmt"
	"path"
)

// CapRepo is the Tailscale app capability name that carries repository grants.
// It is a public contract: changing it invalidates every tailnet policy that
// grants repository access.
const CapRepo = "github.com/kevindurb/tsgit/cap/repo"

// Access is the level a grant confers.
type Access int

const (
	AccessNone Access = iota
	AccessRead
	AccessWrite
)

func (a Access) String() string {
	switch a {
	case AccessRead:
		return "read"
	case AccessWrite:
		return "write"
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
)

// Decision is the result of an authorization check. CanCreate is true exactly
// when a write grant matches the canonical name.
type Decision struct {
	Allow     bool
	Access    Access
	CanCreate bool
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

	d := Decision{Access: best, CanCreate: best == AccessWrite}
	switch op {
	case OpRead:
		d.Allow = best >= AccessRead
	case OpWrite, OpCreate:
		d.Allow = best == AccessWrite
	default:
		d.Allow = false
	}

	if !d.Allow {
		d.Reason = fmt.Sprintf("no %s capability for %s", op, repo)
	}
	return d
}
