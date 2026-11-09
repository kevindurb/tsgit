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
- A read-only discovery web UI that lists the repositories a viewer may read
  and shows each repository's clone URL and latest activity.
- A read-only code browser for readable repositories: directory trees, file
  contents, branch/tag/commit selection, and safely rendered README Markdown.
- Deleting a repository from the discovery UI when the viewer has `admin`
  access, with a typed confirmation and no separate configuration.
- Running as a single binary on one tailnet node, with no host Tailscale
  daemon and no account system.
- Optional, read-only public clone and browsing of a curated subset of
  repositories over Tailscale Funnel, selected by tailnet policy.

SSH transport is deferred. Smart HTTP carries clone, fetch, and push, so it is
the first and only transport in the current slice.

Out of scope:

- Users outside the tailnet, except anonymous read-only clone and browsing of
  repositories explicitly designated by tailnet policy. Public write, public
  accounts, and control over who may read a public repository remain out of
  scope.
- Its own authentication, password, or key management.
- Commit history, diffs, blame, search, syntax highlighting, or editing files in
  the web UI. Basic read-only trees, blobs, ref selection, and README rendering
  are in scope; issues and pull requests are not.
- Creating or renaming repositories from the web UI. Creation remains the
  result of an authorized git push; renaming is a client-side mirror-push plus
  delete. Deleting a repository is an admin-gated web action.
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
