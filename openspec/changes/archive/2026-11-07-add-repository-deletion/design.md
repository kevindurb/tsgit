# Design

## Context

See `proposal.md` - Why. Current state that shapes the approach:

- `internal/authz` has `none < read < write`. `Decide(id, repo, op)` returns
  `Access`, `Allow`, and `CanCreate`, and `CanCreate` is currently
  `best == AccessWrite`. Adding a level above write makes that comparison wrong.
- Existing tests deliberately encode the old contract: `TestAccessRejectsUnknownValue`
  and `TestResolveUnknownAccessGrantsNothing` use `"admin"` as the unknown value,
  and one `TestDecide` case uses `Op("delete")` as an unknown operation. They
  must change in this change, not after it.
- `internal/repos.Store` owns the root, containment, and atomic creation.
  `Create` stages under `.tsgit-tmp` and renames into place. There is no lock;
  concurrent creates rely on the final rename and the `dirNonEmpty` check.
  `List` and `Info` tolerate a changing tree.
- `internal/web` is a read-only discovery front end. It resolves identity once,
  filters with `authz.Decide(OpRead)`, renders embedded templates, and refuses
  anything that is not `GET` or `HEAD` with `405`. It sets no cookie, keeps no
  session, and serves a restrictive CSP that already allows
  `form-action 'self'`.
- `cmd/tsgit` dispatches `transport.Matches` to git and every other path to
  discovery. The Funnel composition sets `transport.Handler.ReadOnly` and uses
  the public principal. The repository root is a plain bare-directory tree.
- There is no database, no configuration flag for the UI, and no session store.
  `crypto/rand` and `crypto/hmac` are stdlib.

## Goals / Non-Goals

**Goals:**

- One destructive web action, gated by `admin`, with a deliberate confirmation
  and cross-site protection that needs no JavaScript.
- Keep the discovery handler GET-only and cookie-free.
- Make the repository disappear atomically and leave in-flight transfers safe.
- Add no dependency, database, session, cookie, flag, or storage format.

**Non-Goals:**

- Archive, restore, rename, bulk deletion, or namespace deletion.
- A trash retention window or undo. The rename into staging is an atomicity
  mechanism, not a user-visible trash.
- A separate admin capability or a second policy vocabulary.
- Changes to push-to-create, the git endpoints, the repository layout, or the
  public Funnel surface.
- An `admin` badge on the index. The summary's Delete link is the affordance.

## Decisions

### `admin` is a new level in `cap/repo`, not a new capability

`AccessNone < AccessRead < AccessWrite < AccessAdmin`. `UnmarshalJSON` accepts
`"admin"`, `String` returns `"admin"`, and `Decide` handles `OpDelete` with
`best >= AccessAdmin`. `Decision` computes `CanCreate: best >= AccessWrite` and
`CanDelete: best >= AccessAdmin`.

Why: the tailnet policy keeps one vocabulary, and `admin` naturally implies
write and read. The user accepted the downgrade failure: an older binary sees
an unknown value and fails the whole payload closed. A separate capability
would preserve rollback but adds a second payload, a second decode, and a
second contract for a single operation. Rejected.

The non-obvious correction is `CanCreate`. With the current
`best == AccessWrite`, an admin would lose push-to-create and the summary's
write badge would be wrong. The change must be `>= AccessWrite`, and the tests
must assert it.

`identity.Development()` becomes `AccessAdmin` so development mode exercises
the surface. `PublicIdentity` stays `AccessRead`, so the public principal can
never delete.

### The delete surface lives under `/.tsgit/delete/`, outside the discovery handler

```
GET   /.tsgit/delete/<ns>/<repo>    confirmation page
POST  /.tsgit/delete/<ns>/<repo>    delete, 303 to /
HEAD  /.tsgit/delete/<ns>/<repo>    same headers as GET
other                               405
```

`web.DeleteHandler` is a separate type in the `web` package, not part of
`web.Handler`. It shares the base template, render helper, security headers,
`repoPath`, and `cloneURL`, but it owns the mutation and the token key. The
discovery handler stays read-only.

`cmd/tsgit.front` gains a third branch:

```
transport.Matches(r)       -> git
DeleteHandler.Matches(r)   -> delete
otherwise                  -> discovery
```

The Funnel composition passes `nil`, so the route is structurally absent on the
public listener. The prefix is collision-free because `ParseName` refuses a
segment that starts with `.`, so no repository can claim `/.tsgit/delete/...`.

Alternatives:

- **Inline forms on the summary.** The summary is a GET page; embedding a token
  and a mutation target would put the admin secret and CSRF handling into the
  read-only handler, and it would make the discovery page a mutation surface.
  The link is enough.
- **`POST /<ns>/<repo>`.** It collides conceptually with the summary and makes
  the dispatcher decide between a malformed page and an action. Rejected.
- **A separate `internal/webdelete` package.** It would duplicate the page
  chrome or require extracting shared UI first. One page does not justify that;
  a separate handler type in `web` keeps the behavior split without the churn.

### Authorization is a value decision checked before any filesystem work

The handler resolves identity through `identity.Resolver`, parses the canonical
name with `repos.ParseName`, and calls `authz.Decide(id, name, OpDelete)` before
rendering the confirmation or touching the store. A denied viewer and a missing
repository both receive the same `404`; the real reason is logged. The handler
re-checks on `POST` and never relies on the link having been absent.

The Delete link is rendered from `decision.CanDelete`, computed by the same
`Decide` call the summary already makes for `OpRead`. No second authorization
call is needed on the summary.

Why: it follows the project's convention that `authz` takes values and that
authorization precedes work. It also keeps the existence oracle closed.

### Confirmation is a typed canonical name plus metadata

The confirmation page shows the canonical name, description, default branch,
latest commit, clone URL, and a permanence warning. It calls `Store.Info`; a
metadata failure still renders the name and warning, because deleting a broken
repository is a valid case.

The `POST` requires `confirm == canonical name` exactly. A mismatch returns
`400` with the form and changes nothing. A valid `POST` deletes and redirects.

Why: deletion is irreversible and has no undo. Typing the name is a deliberate
signal that a checkbox does not provide, needs no JavaScript, and follows the
established forge pattern.

### CSRF protection is a signed token, not a cookie

The delete handler receives a 32-byte key generated once at startup from
`crypto/rand`. The token binds the requester's node, the canonical name, the
delete action, and a one-hour expiry:

```
expiry + "." + base64url(HMAC-SHA256(key, node "\x00" repo "\x00" "delete" "\x00" expiry))
```

On `POST` the handler verifies the token in constant time, then checks
`Origin` when present against the request's own scheme and host, and requires
`Sec-Fetch-Site` to be `same-origin` when present. A failure is `403` and the
repository is untouched.

No cookie, no session, no server-side token store. A restart invalidates open
forms; they receive `403` with a "reload the page" message.

Alternatives:

- **Cookie double-submit.** Standard, but it introduces the first cookie into a
  system whose discovery pages are deliberately cookie-free.
- **Custom header.** Requires JavaScript, which the UI does not use.
- **Origin check alone.** Cheap, but older browsers can omit both headers; the
  signed token is the primary defense and the headers are depth.

The token is bound to the node rather than the login. An admin token leaked from
one node could be replayed from another admin node, but reaching the page
already requires `admin`, the referral policy is `no-referrer`, and the expiry
is short. Acceptable for this surface.

The `POST` body is bounded with `http.MaxBytesReader` (64 KiB is ample for a
small form).

### `Store.Delete` renames into `.tsgit-tmp`, then removes

`Store` gains a `sync.Mutex`. `Create` holds it from the existence check
through the final rename. `Delete` holds it through the rename, releases it,
then removes the trash.

```
ParseName -> Resolve -> lock -> Lstat -> MkdirTemp -> Rename -> unlock -> RemoveAll
```

Details:

- `ErrNotExist` for a missing repository; `ErrInvalidName` and `ErrOutsideRoot`
  keep their current meaning.
- Refuse a symlinked repository directory with a clear error. `Resolve` follows
  internal symlinks, and deleting or renaming the link instead of the data is
  surprising.
- Rename the directory into a unique `.tsgit-tmp/delete-*` directory. On
  success the repository is gone from the served tree immediately, and an
  in-flight `upload-pack` or `receive-pack` keeps its open inode and finishes.
- Remove the trash after releasing the lock. If removal fails, return a wrapped
  `ErrCleanup`; the handler logs a warning and still redirects, because the
  repository is already gone. A rename failure is a normal error and becomes a
  `500`.
- Best-effort `os.Remove` of the namespace directory; ignore `ENOTEMPTY` and
  `EBUSY`.
- `EXDEV` if a namespace is a separate mount: return an explicit error. The
  project already assumes one filesystem for atomic creation; a non-atomic
  fallback would weaken the delete guarantee.

Why the rename: a direct `RemoveAll` can fail partway and leave a half-live
repository, and a clone that opens a new pack after removal starts can fail
mid-transfer. The rename makes the disappearance atomic and defers the
irreversible work to a path no request can reach.

Why the lock: without it, `Create` and `Delete` can interleave between the
existence check and the final rename. With it, either create wins and the delete
removes the new repository, or delete wins and the next push creates a new one.
Both are coherent. The lock is never held for a transfer, so a long clone does
not block an admin.

### The public listener omits the delete handler

`composePublic` calls `front(git, ui, nil)`. The `front` dispatcher falls
through to the discovery handler when `del` is nil, and discovery 404s the
path. The public principal is read-only anyway; omitting the handler is
structural defense in depth, not a grant check.

### Wiring and failure handling

- `cmd/tsgit` generates the CSRF key once per server start and passes it to
  `web.NewDeleteHandler`. A `crypto/rand` failure fails startup rather than
  silently disabling deletion.
- `serveDev` and `composeTailnet` build the delete handler with the same
  resolver, store, logger, and version. `composePublic` does not.
- `front` takes the optional delete handler. The existing `front(git, ui)` test
  call is updated to pass `nil` or a handler.
- The summary gains a `Deletable` flag on `repoView` and a danger-styled link to
  `/.tsgit/delete/` plus `repoPath(name)`. The delete page is parsed into its
  own template set, like the existing pages.
- Status codes: `404` for denied or missing, `403` for token or origin failure,
  `400` for a typed-name mismatch, `405` for an unsupported method, `503` for
  identity failure, `500` for a store failure, `303` on success.
- On success, log `repo`, `node`, `login`, `tags`, `latest` (from the pre-delete
  `Info`), and `remote`. Redirect to `/?deleted=<urlencoded name>`; the index
  renders a notice from the query value as escaped text. No flash store.

## Risks / Trade-offs

- **Irreversible action** -> typed confirmation, metadata, a permanence
  warning, and a README note to back up the repository volume. No retention
  window by design.
- **`admin` is broad** -> it also implies write and create. Policy must scope it
  narrowly; the docs show a group-scoped example.
- **Downgrade is fail-closed** -> an older binary grants nothing for a payload
  containing `admin`. Accepted because the project is new; documented in
  `access-control.md` and the proposal.
- **Token invalidation on restart** -> a form open across a restart gets `403`
  and a reload instruction. Acceptable for a short-lived confirmation.
- **In-flight push during delete** -> the push can write into the trash inode
  and is discarded when the trash is removed. That is the correct delete
  outcome; the spec states it. A lock over the whole transfer would block the
  admin for minutes and is rejected.
- **Name reuse after delete** -> a later push can create a new repository at the
  name, and an old clone URL will then point at it. Inherent to delete plus
  push-to-create; stated as a scenario, not prevented with a tombstone.
- **Synchronous removal can be slow for a very large repository** -> the
  repository already disappeared at the rename; the request waits only for
  space reclamation. A background sweeper is a later change if this is noticed.
- **Symlinked repository directories are refused** -> the operator resolves the
  link manually. Safer than deleting through a link.
- **Empty namespace cleanup is best-effort** -> a leftover directory is
  harmless; `List` ignores it.
- **Existing tests encode the old contract** -> `TestAccessRejectsUnknownValue`,
  `TestResolveUnknownAccessGrantsNothing`, and the `Op("delete")` case in
  `TestDecide` must be rewritten. `TestDocumentedCapabilityFixture` ties the
  docs example to the parser, so the fixture and test move together.

## Migration Plan

1. Land the authz level, the delete handler, the store method, the wiring, and
   the test updates as one change. `just ci` and `just spec-validate` must pass.
2. Deploy the binary. No data migration, no configuration, and no container
   change. The route exists but is inert: no existing grant is `admin`, so no
   viewer sees the Delete link.
3. Add `admin` to the intended `cap/repo` grants. The policy is re-read on the
   next request, so no restart is needed.
4. Roll back by reverting the binary. Remove `admin` grants before a rollback if
   the old binary must keep honoring those peers; otherwise the whole payload
   grants nothing.
5. Update `docs/access-control.md` (level table, fixture, downgrade note),
   `docs/project.md` (scope), `docs/architecture.md` (route, store deletion,
   admin level), `README.md` (admin grant, delete flow, rename recipe), and
   `docs/deployment.md` (policy fragment). `docs/configuration.md` needs no
   change.

## Open Questions

None that affect the specs or the task breakdown. Whether to add a retention
window, namespace deletion, or an admin badge on the index are later feature
decisions, not blockers.
