# tsgit

A git forge that treats Tailscale as its network, not an afterthought.

Existing forges assume a public listener, an account system, and a PKI of their
own. `tsgit` assumes none of that: every human and machine that reaches it is
already identified by the tailnet, already authenticated by WireGuard, and
already reachable by MagicDNS. Identity, transport, and reachability are three
things a forge normally has to build. On a tailnet, they are given.

Status: early. The scaffolding, the contract, and the specs are in place; the
forge itself is being built.

## What it does and does not do

**Does** serve git repositories over smart HTTP to tailnet peers from an
embedded tsnet node, authorize reads and writes from tailnet identity and ACL
tags, let repos be created and discovered without a separate account database,
and show a read-only repository index with clone URLs in the browser. SSH is
deferred; smart HTTP already covers clone, fetch, and push.

**Does not** aim to be a GitHub replacement for public open source, run its own
authentication, serve users outside the tailnet, or become a CI system.

## Commands

Everything runs through `just`. See `AGENTS.md` for the full contract.

```
just ci        # fmt, lint, build, test — run before saying anything is done
just run       # go run ./cmd/tsgit
```

## Container image

Images are published to GitHub Container Registry on `main` and on `vX.Y.Z`
tags:

```sh
podman pull ghcr.io/kevindurb/tsgit:latest
```

See [`docs/deployment.md`](docs/deployment.md) for the runtime contract, volume
and auth-key setup, and the publishing tag rules.

## Layout

| Path | Holds |
|---|---|
| `cmd/tsgit/` | The binary's entry point |
| `internal/` | Implementation, package per concern |
| `docs/` | Project and architecture documentation |
| `openspec/` | Specs and change artifacts |
| `.agents/skills/` | Durable agent conventions |

## Documentation

- [`docs/project.md`](docs/project.md) — purpose, users, scope, non-goals
- [`docs/architecture.md`](docs/architecture.md) — how the pieces fit
- [`docs/configuration.md`](docs/configuration.md) — flags and data layout
- [`docs/deployment.md`](docs/deployment.md) — container image and publishing
- [`docs/access-control.md`](docs/access-control.md) — the capability contract
- [`openspec/`](openspec/) — capability specs and in-flight changes

## Getting started

```
git clone git@github.com:kevindurb/tsgit.git && cd tsgit
just install-hooks   # once per clone
just ci
```
