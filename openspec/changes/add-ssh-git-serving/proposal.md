# Proposal

## Why

`tsgit` has scaffolding and a stub binary but serves nothing. The whole thesis —
that a forge on a tailnet can skip authentication because Tailscale has already
done it — is untested until a real client can `git clone` a real repository and
be authorized by its tailnet identity.

This change picks the narrowest slice that proves the thesis: SSH transport
only, one repository root, authorization from peer identity. If this works, the
HTTPS path and repository management are variations on a solved problem rather
than open questions.

## What Changes

- `internal/identity` resolves an SSH connection to a tailnet identity (node,
  user login, tags, app capabilities) via tailscaled's LocalAPI `WhoIs`.
- `internal/authz` decides read or write for an identity against a repository
  path, from the Tailscale app capabilities the connection was granted.
- `internal/transport/ssh` accepts SSH sessions, parses the git command, checks
  access, and execs `git upload-pack` or `git receive-pack`.
- `internal/repos` maps a requested repository name to a bare directory under
  the configured root, rejecting traversal.
- The daemon learns `serve` and a config file with listen address, repo root,
  and LocalAPI socket.

## Capabilities

### New Capabilities

- `tailnet-identity`: resolving an inbound connection to a tailnet identity
  (node name, user login, ACL tags, app capabilities) without trusting
  client-supplied values.
- `access-control`: deciding whether an identity may read or write a named
  repository from its Tailscale capabilities, and refusing everything else.
- `git-serving`: serving clone, fetch, and push over SSH by handing the session
  to git's own plumbing after authorization.

### Modified Capabilities

None. This is the first capability set.

## Impact

- New packages under `internal/`: `identity`, `authz`, `repos`, `transport/ssh`,
  `config`.
- `cmd/tsgit` gains a `serve` subcommand; the binary now binds a port.
- New runtime dependency: a reachable tailscaled LocalAPI socket, and `git` on
  the host. No new Go module: the LocalAPI client is stdlib HTTP over the unix
  socket.
- No database, no migration. State is the repo root; authorization lives in the
  tailnet policy file, which `tsgit` reads through LocalAPI and never stores.
