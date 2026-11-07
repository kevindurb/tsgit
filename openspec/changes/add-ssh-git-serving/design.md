# Design

## Context

The binary is a stub. There is no identity resolution, no authorization, no
transport. See `proposal.md` for why this slice. Constraints that shape it:

- tailscaled runs on the same host, and its LocalAPI is a unix socket at a
  known path. Querying it is a local operation, not a network round trip.
- `git` is installed on the host, and the design deliberately uses its plumbing
  (`docs/decisions/0001-git-transport-via-plumbing.md`).
- There is no database and none is being added.
- Repository permissions live in the tailnet policy file, not in `tsgit`.

## Goals / Non-Goals

**Goals:**
- Prove the tailnet-identity thesis with a working clone and push.
- Derive authorization from the tailnet policy alone, so `tsgit` stores no
  permission data of its own.
- Keep authorization a pure function of identity and repository.
- Refuse safely: unknown peer, unknown repo, missing or malformed capability
  all deny.

**Non-Goals:**
- HTTPS transport. It is a second front end over the same `identity` and
  `authz` seams, and follows this change.
- Repository creation, listing, and a web UI.
- Fine-grained ref-level permissions. Repository-level read/write is the unit.
- Group management. Tailscale expands groups, users, and tags before `tsgit`
  sees anything; `tsgit` evaluates the resulting capability only.
- A local override or fallback policy. A second authorization source would
  drift from the tailnet and defeat the point.

## Decisions

**Use tailscaled's LocalAPI `WhoIs` for identity and capabilities.**
`WhoIs` on the peer address returns the node, its tags, the user login, and the
capability map granted to that peer for this destination. One local call
supplies everything authorization needs. This is the same mechanism Tailscale
SSH and first-party apps (GoLink, TailSQL) use, so a client that works with
Tailscale SSH works here. Alternative considered: trust the SSH user name as
`git@` supplies it — rejected, because it is client-supplied and trivially
spoofed.

**Serve SSH with `gliderlabs/ssh` or Go's `x/crypto/ssh`, not by delegating to
`tailscaled`'s own SSH server.** Delegating would mean the forge only works
where Tailscale SSH is enabled and would give us no hook to name the repository
before git starts. Owning the listener lets us parse the requested command
first, which is the whole point of deciding before executing.

**Authorization comes from Tailscale app capabilities, not a local policy
file.** A grant in the tailnet policy file names the source (`user:`, `group:`,
or `tag:`) and the `tsgit` node as destination, then attaches a capability
describing repository access:

```json
"grants": [
  {
    "src": ["group:devs", "tag:ci"],
    "dst": ["tag:git"],
    "ip": ["tcp:22"],
    "app": {
      "github.com/kevindurb/tsgit/cap/repo": [
        {"repos": ["acme/*"], "access": "write"},
        {"repos": ["shared/docs"], "access": "read"}
      ]
    }
  }
]
```

The capability value is an array of objects; each grants `access` (`read` or
`write`) over a list of repository globs. Grants union: the highest level
matching any object wins, and no match denies. Repository names are matched in
canonical `<owner>/<repo>` form with `path.Match` semantics, so `*` does not
cross `/`. The capability name is a constant in the binary, published in the
docs, and must sit under a domain the project controls; `tailscale.com/*` is
reserved for Tailscale.

Alternative considered: a YAML policy file shipped next to the repos. Rejected
because it creates a second authorization system that must be kept in sync with
the tailnet, cannot express groups without inventing group storage, and cannot
answer "why can this peer read this repo?" from one place. The cost of the
chosen approach is that changing repository permissions now requires tailnet
admin access — the same access needed to add a user, and deliberate.

**`ip` and `app` are both required for SSH.** An app capability on its own does
not open a port. The docs must show the full grant, not just the `app` block,
or an operator will write a policy that authenticates nobody.

**Malformed capabilities deny.** Tailscale validates that a capability value is
an array of JSON objects, but not the project's schema inside them. An unknown
`access` value, a non-array payload, or an unparsable object is treated as no
grant for that object and logged. It never widens access.

**Repository name resolution is a containment check, not a sanitizer.** Resolve
the requested name against the root with `filepath.Clean` and verify the result
still has the root as a prefix after `filepath.EvalSymlinks`. Rejecting
`..`-containing strings by pattern is the weaker, bypassable approach.

## Risks / Trade-offs

- **Access decisions outlive the request.** A fetch authorized at start keeps
  running if the tailnet policy changes or the node is retagged. Acceptable for
  a first slice; a long-running clone is bounded in practice.
- **LocalAPI is a single point of failure.** If tailscaled restarts, sessions
  fail closed. That is the correct direction, but it means `tsgit` is unusable
  during a tailscaled restart.
- **No caching of identity or capabilities.** Every connection costs a LocalAPI
  query. At tailnet scale this is irrelevant; caching would introduce staleness
  in the one thing that must not be stale.
- **Capability payloads are not schema-validated upstream.** A typo in `repos`
  or `access` denies silently. The schema is small and documented; there is no
  way to validate a tailnet policy this process cannot see.
- **Repository permissions require tailnet admin.** There is no per-repo owner
  who can delegate access locally. Delegation could be a later change, but not
  in this slice.
- **The capability name is a public contract.** Changing it after users write
  policy breaks them, so it is fixed before implementation.
