---
name: tailnet-app-capabilities
description: How tsgit authorizes repositories — Tailscale app capabilities, not a local policy store.
---

# Tailscale app capabilities are the authorization source

**Directive:** Repository access comes from app capabilities in the tailnet
policy file, resolved from the same LocalAPI `WhoIs` call that yields node,
user, and tags. Never add a local policy file, permission table, or group
store.

**Why:** A second authorization source drifts from the tailnet, cannot express
groups, and splits the answer to "why can this peer read this repo?" across two
places. The tailnet policy is already versioned and audited by Tailscale.

## Contract

- Capability name: `github.com/kevindurb/tsgit/cap/repo` — a constant, fixed
  before implementation; changing it breaks written policy.
- Payload: an array of objects, `{"repos": ["acme/*"], "access": "read"}` where
  `access` is `read` or `write`.
- Matches union; the highest level wins; `write` implies `read`; no match
  denies.
- Globs use `path.Match` on the canonical `<owner>/<repo>` name; `*` does not
  cross `/`.

## Two traps

1. **A grant needs `ip` as well as `app`.** An `app`-only grant does not open a
   port, so the client never connects and the capability is never read. Always
   show both:

   ```json
   {
     "src": ["group:devs"],
     "dst": ["tag:git"],
     "ip": ["tcp:22"],
     "app": {
       "github.com/kevindurb/tsgit/cap/repo": [
         {"repos": ["acme/*"], "access": "write"}
       ]
     }
   }
   ```

2. **Tailscale validates only that an app value is an array of objects**, not
   the project schema inside them. Treat an unknown `access`, a wrong type, or
   an undecodable object as granting nothing, and log it. Never fail open.

The capability map arrives on `WhoIs`; decode it in `internal/identity` so
`authz.Decide` stays a pure function of `(Identity, Repo, Op)`.
