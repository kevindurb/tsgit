# Proposal

## Why

`tsgit` has scaffolding, a documented thesis, and no working forge. The claim —
that a git host on a tailnet can skip its own authentication because Tailscale
already identified the peer — is untested until a real client can clone and push
a real repository.

The earlier SSH-first change was removed in favour of an HTTP-first design. An
embedded `tsnet` node is a smaller surface than a host daemon plus an SSH
listener: it needs no root, no system Tailscale, and no separate TLS
termination, and it matches how Tailscale-native apps such as golink and tsidp
are built. HTTP is also the only transport needed to prove the thesis: smart
HTTP carries clone, fetch, and push, and `git` already speaks it.

## What Changes

- `cmd/tsgit` gains a `serve` subcommand that starts an embedded `tsnet` node,
  a tailnet HTTPS listener (falling back to HTTP when HTTPS is not enabled), and
  a smart-HTTP git server.
- `internal/identity` resolves the request's peer address through the embedded
  node's LocalClient `WhoIs` into a node identity, an optional user login, ACL
  tags, and the parsed `cap/repo` app capability. Client-supplied headers and
  query parameters are never used as identity.
- `internal/authz` decides read or write for an identity against a canonical
  `<namespace>/<repo>` name from the capability rules. It denies by default;
  `write` implies both `read` and permission to create the repository.
- `internal/repos` maps a requested name to a bare repository under a single
  root with containment checks, and creates missing repositories atomically
  when a writer's capability matches.
- `internal/transport` serves the smart-HTTP endpoints (`info/refs`,
  `git-upload-pack`, `git-receive-pack`) and delegates the pack exchange to a
  `Backend` interface. The first backend execs `git upload-pack` and
  `git receive-pack --stateless-rpc`; a pure-Go backend can be added later
  without changing identity, authorization, repository resolution, or HTTP
  routing.
- Configuration follows the conventions of golink and tsidp: `--hostname`,
  `--port`, `--state-dir`, `--repo-root`, `--advertise-tags`,
  `--authkey-file`, `--control-url`, `--https`, `--dev-listen`, `--log`, and
  `--version`, with app-prefixed environment fallbacks.
- `--dev-listen` runs a plain localhost server with a temporary repository root
  and a development identity, so clone, push, and push-to-create are usable
  without a tailnet.
- Documentation is updated to describe the HTTP-first, tsnet-embedded shape.
  `docs/decisions/0001-git-transport-via-plumbing.md` still governs the
  transport: the backend shells out to git plumbing, and the seam makes that
  choice replaceable rather than permanent.

## Capabilities

### New Capabilities

- `tailnet-identity`: resolving an inbound HTTP request to the tailnet identity
  and app capabilities behind it, via the embedded node's LocalAPI, without
  trusting client-supplied values.
- `access-control`: deciding read or write for an identity on a canonical
  repository name from Tailscale app capabilities, with deny-by-default,
  `write` implying `read`, and `write` authorizing creation.
- `git-serving`: serving clone, fetch, and push over smart HTTP, including
  push-to-create, by handing an authorized request to a replaceable git
  backend.

### Modified Capabilities

None. This is the first capability set; the removed SSH change left no specs.

## Impact

- New packages under `internal/`: `identity`, `authz`, `repos`,
  `transport` (HTTP handler and backend), and `config`.
- New Go dependency: `tailscale.com` for `tsnet`, `tailcfg`, `client/local`,
  and `envknob`. This is the node and the identity source; it is the
  justification for departing from the stdlib-only default.
- New runtime dependency: `git` on the host, unchanged from ADR 0001. The
  backend seam keeps a pure-Go implementation possible.
- New data layout: a tsnet state directory (node key, not backed up with
  repositories) and a repository root of bare git directories.
- `cmd/tsgit` binds a listener inside the tailnet; no host port is exposed.
- No database and no migration. State is the repository tree plus the tsnet
  state directory.
- Out of scope for this change: SSH transport, Tailscale Funnel, Tailscale
  Service mode, running against a host tailscaled, repository listing and
  browsing, and a go-git backend.
