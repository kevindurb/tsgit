# Project

## Purpose

`tsgit` is a git forge built for tailnets. It serves repositories to peers that
Tailscale has already authenticated and connected, and it derives authorization
from tailnet identity — user logins and ACL tags — rather than from an account
system it maintains itself.

## Users

Someone running a tailnet with a handful to a few dozen machines who wants
private git hosting without standing up a forge's account model, TLS
termination, or user directory. Typically a homelab, a small team, or a person
who already uses Tailscale SSH and would rather `git clone` the same way.

## Scope

In scope:

- Serving git over smart HTTP to tailnet peers from an embedded `tsnet` node.
- Authorization from tailnet identity, including tag-based machine access.
- Repository creation, listing, and discovery scoped to the tailnet.
- Running as a single binary on one tailnet node, with no host Tailscale
  daemon and no account system.

SSH transport is deferred. Smart HTTP carries clone, fetch, and push, so it is
the first and only transport in the current slice.

Out of scope:

- Users outside the tailnet, and public anonymous clone.
- Its own authentication, password, or key management.
- A web UI for browsing code, issues, or pull requests.
- CI/CD. `tsgit` hosts code; it does not test it.
- Federation or mirroring between tailnets.

## Non-goals worth naming

**Not a GitHub replacement.** The value is in *not* having GitHub's account and
hosting model, not in reimplementing its feature surface. Features that only
make sense for public multi-tenant hosting are not wanted here.

**Not a wrapper.** Shelling out to `git http-backend` or `git-receive-pack` for
the transfer itself is expected and fine. Reimplementing the pack protocol is
not. Being tailnet-native is about identity and reachability, not about
avoiding `git`'s own plumbing.
