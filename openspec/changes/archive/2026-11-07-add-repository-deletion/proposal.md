# Proposal

## Why

`tsgit` can create a repository with an authorized push, but it has no way to
remove one. A repository created by a typo, an abandoned experiment, or a
completed project is stuck in the index until an operator edits the repository
root by hand. The discovery UI is now the natural place for the one destructive
lifecycle action, because it already resolves the viewer's tailnet identity and
can ask for confirmation before anything happens.

This closes the lifecycle loop without adding accounts, sessions, a database, or
a second authorization store. Rename stays a client-side mirror-push plus
delete; archive stays out of scope.

## What Changes

- Add `admin` as the highest `cap/repo` access level. `admin` implies `write`
  and `read`, and it is the only level that authorizes deletion.
  **BREAKING** for older binaries: an older `tsgit` cannot decode an `admin`
  value and fails the whole capability payload closed, so a peer granted
  `admin` loses all repository access after a downgrade. Existing `read` and
  `write` payloads keep working unchanged.
- Add an `OpDelete` operation and a `CanDelete` decision to `internal/authz`.
  Correct `CanCreate` from `== AccessWrite` to `>= AccessWrite` so admins keep
  push-to-create.
- Add a delete action to the tailnet web UI:
  - the repository summary shows a Delete link when the viewer has `admin`;
  - a confirmation page shows the repository's description, default branch,
    latest commit, and clone URL, and requires typing the canonical name;
  - a `POST` performs the deletion and redirects to the index.
  The route is always mounted on the tailnet listener. It is not mounted on the
  Funnel listener, so the public surface cannot reach it.
- Add `repos.Store.Delete`: take the same mutex used by creation, atomically
  rename the bare directory into `.tsgit-tmp`, then remove it. The repository
  disappears atomically and an in-flight clone finishes on its open inode.
- Protect the `POST` with a signed per-request token plus `Origin` and
  `Sec-Fetch-Site` checks. No cookie, no session, no token store.
- Document the `admin` level, the delete flow, and the client-side rename
  recipe in `docs/access-control.md`, `docs/project.md`,
  `docs/architecture.md`, `README.md`, and `docs/deployment.md`.

No new dependency, no database, no configuration flag, and no change to the git
protocol, the repository layout, or the container.

## Capabilities

### New Capabilities

- `repository-deletion`: an admin-gated web action that confirms, authorizes,
  and permanently removes one repository from the tailnet listener, with
  CSRF protection, atomic disappearance, and an audit log.

### Modified Capabilities

- `access-control`: add the ordered `read < write < admin` levels; `admin`
  implies write and create, and only `admin` authorizes deletion.
- `repository-discovery`: the summary offers a Delete action to an identity with
  `admin`; the discovery pages remain GET-only and perform no mutation
  themselves.
- `funnel-access`: the public read-only principal is explicitly forbidden from
  deletion, and the public surface does not serve the deletion route.

## Impact

- `internal/authz`: `AccessAdmin`, `OpDelete`, `Decision.CanDelete`, and the
  `CanCreate` correction.
- `internal/identity`: the development identity becomes `AccessAdmin`; the
  capability decoder is otherwise unchanged.
- `internal/repos`: `Delete`, a `sync.Mutex` shared with `Create`, and the
  rename-to-trash deletion path.
- `internal/web`: the Delete link, confirmation page, delete handler, and an
  `admin` access badge.
- `cmd/tsgit`: mount the delete handler on the tailnet and development
  compositions, and omit it from the Funnel composition.
- Tests for the access hierarchy, the delete handler, CSRF rejection, typed
  confirmation, the store deletion, and public refusal.
- Documentation and specs. No change to `internal/transport`,
  `internal/config`, the git endpoints, or the container image.
- Operational: the feature is inert after upgrade until the tailnet policy
  grants `admin`; a policy edit takes effect on the next request.
