# Design

## Context

The binary is a stub. There is no identity resolution, no authorization, no
transport. See `proposal.md` for why this slice. Constraints that shape it:

- tailscaled runs on the same host, and its LocalAPI is a unix socket at a
  known path. Querying it is a local operation, not a network round trip.
- `git` is installed on the host, and the design deliberately uses its plumbing
  (`docs/decisions/0001-git-transport-via-plumbing.md`).
- There is no database and none is being added.

## Goals / Non-Goals

**Goals:**
- Prove the tailnet-identity thesis with a working clone and push.
- Keep authorization a pure function of identity and repository.
- Refuse safely: unknown peer, unknown repo, unreadable policy all deny.

**Non-Goals:**
- HTTPS transport. It is a second front end over the same `identity` and
  `authz` seams, and follows this change.
- Repository creation, listing, and a web UI.
- Fine-grained ref-level permissions. Repository-level read/write is the unit.
- Group management. Rules name tailnet users and tags only.

## Decisions

**Use tailscaled's LocalAPI `WhoIs` rather than parsing `SSH_CONNECTION`.**
The TCP source address alone names a machine but not the human. `WhoIs` on the
peer address returns the node, its tags, and the user. This is the same
mechanism Tailscale SSH uses, so a client that works with Tailscale SSH works
here. Alternative considered: trust the SSH user name as `git@` supplies it —
rejected, because it is client-supplied and trivially spoofed.

**Serve SSH with `gliderlabs/ssh` or Go's `x/crypto/ssh`, not by delegating to
`tailscaled`'s own SSH server.** Delegating would mean the forge only works
where Tailscale SSH is enabled and would give us no hook to name the
repository before git starts. Owning the listener lets us parse the requested
command first, which is the whole point of deciding before executing.

**Policy as a small YAML file, evaluated per request.** Rules grant
`user:<login>` or `tag:<name>` a level (`read` or `write`) on a repository
glob. A file keeps it diffable and readable next to the repos; it matches the
"no database" constraint. Alternative considered: reuse the tailnet ACL policy
directly — rejected for now, because the tailnet ACL governs network reach, and
overloading it to also mean per-repository permission conflates two decisions a
user may want to differ. Worth revisiting once users ask.

**Repository name resolution is a containment check, not a sanitizer.** Resolve
the requested name against the root with `filepath.Clean` and verify the result
still has the root as a prefix after `filepath.EvalSymlinks`. Rejecting
`..`-containing strings by pattern is the weaker, bypassable approach.

## Risks / Trade-offs

- **Access decisions outlive the request.** A fetch authorized at start keeps
  running if the policy changes or the node is tagged differently. Acceptable
  for a first slice; a long-running clone is bounded in practice.
- **LocalAPI is a single point of failure.** If tailscaled restarts, sessions
  fail closed. That is the correct direction, but it means `tsgit` is unusable
  during a tailscaled restart.
- **No caching of identity.** Every connection costs a LocalAPI query. At
  tailnet scale this is irrelevant; caching it would introduce staleness in the
  one thing that must not be stale.
- **The policy file has no validation on startup yet.** A typo denies by
  default, which is safe but confusing. Startup validation is a follow-up.
