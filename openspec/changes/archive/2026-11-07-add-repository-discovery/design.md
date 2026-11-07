# Design

## Context

See `proposal.md` for motivation and `specs/repository-discovery/spec.md` for
the behavior contract.

Current state that shapes the approach:

- `internal/transport.Handler` owns the whole HTTP path space. It resolves
  identity first, then parses a route; anything that is not one of the four
  smart-HTTP endpoints is refused with 404. Its route parser already accepts a
  repository URL with or without a single `.git` suffix.
- `internal/authz` is a pure decision over an identity, a canonical
  `<namespace>/<repo>` name, and an operation. `internal/repos` owns the root,
  containment, and atomic creation, and is deliberately identity-agnostic.
- `internal/identity` turns a peer address into an `authz.Identity`;
  `identity.Fixed` already exists for tests and development mode.
- There is no database, no template or asset pipeline, and no Node toolchain.
  The Go version is 1.27 and new dependencies need justification.
- The repository tree is plain bare git directories. Git's own files are the
  metadata.

## Goals / Non-Goals

**Goals:**

- Add a second HTTP front end without changing git serving, identity
  resolution, or authorization.
- Keep repository metadata reads contained, identity-agnostic, and cheap at
  tailnet scale.
- Ship a self-contained visual surface: embedded templates and CSS, no
  JavaScript, no external assets, light and dark from the OS preference.
- Keep the diff small enough that reverting it restores the four-endpoint
  server exactly.

**Non-Goals:**

- Code browsing, README rendering, search, issues, pull requests, or any
  mutating action.
- A theme toggle, a settings page, or any cookie. Auto-selection is the whole
  theme feature in this slice.
- Caching repository metadata across requests.
- Any change to push authorization, push-to-create, or the capability
  contract.

## Decisions

### A separate `internal/web` front end, not a transport mode

`transport` is the git smart-HTTP handler; adding templates, asset routing,
and HTML rendering there would make one package own two unrelated protocols.
`web` becomes a sibling that depends on `identity`, `authz`, and `repos`, and
knows nothing about pack backends.

```text
cmd/tsgit
  |
  +-- dispatcher (path predicate)
        |
        +--> internal/transport --> backend, repos
        |
        +--> internal/web ------> identity, authz, repos
```

`transport` stays git-only. The dependency arrows keep pointing one way, and
the layer table in `docs/architecture.md` grows a row instead of collapsing two
responsibilities.

Alternative: a `UI http.Handler` fallback injected into `transport.Handler`.
It would share identity resolution for free, but it couples the git package to
the HTML package and makes `transport` stop meaning what its name says.
Rejected.

### Dispatch by the git handler's own parser

The dispatcher asks transport whether it recognizes the request:

```text
transport.Matches(r)
  true  -> transport.Handler
  false -> web.Handler
```

`Matches` is a thin exported wrapper over the existing `parseRoute`, so the
dispatcher cannot drift from what the git handler accepts.

Alternatives considered:

- **Suffix matching** (`/info/refs`, `/git-upload-pack`, `/git-receive-pack`):
  simple, but shadows valid repository summary names such as
  `info/refs` and `a/git-upload-pack`, and sends malformed git requests to the
  UI instead of refusing them.
- **"Try transport, fall back to web on failure":** a denied push would fall
  through to a styled summary page and leak existence. Rejected outright.
- **Reserve `/ui/` for discovery and leave everything else to transport:** no
  ambiguity, but the index no longer lives at `/` and the natural
  `/<namespace>/<repo>` summary URL is gone. The reserved prefix is still used
  for assets.

The predicate is parse-based, so a malformed smart-HTTP request is refused as
a git request, and a non-git path never starts a backend.

### URL space

| Path | Served by |
|---|---|
| `/` | index |
| `/<namespace>/<repo>` | summary |
| `/<namespace>/<repo>.git` | redirect to the summary |
| `/.tsgit/web.css`, `/.tsgit/favicon.svg` | embedded assets |
| `GET/POST .../info/refs`, `.../git-upload-pack`, `.../git-receive-pack` | git handler |

`/.tsgit/` is collision-free by construction: `repos.validateSegment` refuses a
segment that starts with `.`, so no namespace can ever be named `.tsgit`.
Assets use explicit handlers rather than `http.FileServer`, which avoids
directory listings and redirects.

The `.git`-less clone URL (`/acme/widget/info/refs`) is still recognized by
`parseRoute` and reaches the git handler unchanged. The summary page claims
only the bare two-segment path.

### Metadata belongs to `repos`, filtering belongs to `web`

`repos` owns the root, containment, and the git binary, and it still never
learns who asked. Add:

```go
type Info struct {
    Name        string
    Description string
    Branch      string      // symbolic HEAD branch, when known
    Detached    bool
    Empty       bool        // HEAD branch has no commit
    Latest      Commit      // zero when there is no commit
    Branches    int
    Tags        int
}

type Commit struct {
    Hash    string
    Subject string
    When    time.Time
}

func (s *Store) List(ctx context.Context) ([]Info, error)
func (s *Store) Info(ctx context.Context, name string) (Info, error)
```

`List` walks the root with `os.ReadDir`, skips `.tsgit-tmp` and any entry whose
segments fail `ParseName`, and calls `Resolve` for each candidate. `Resolve`,
not a raw walk, is what enforces containment through existing symlinks.
`Info` returns `ErrNotExist` for a missing repository.

Metadata comes from git's own files plus one read-only plumbing call:

- `description`: the file, with git's default `Unnamed repository; edit this
  file ...` boilerplate treated as empty.
- `HEAD`: symbolic branch or detached hash; a missing target means unborn.
- refs: one `git --git-dir <path> for-each-ref
  --sort=-committerdate --format=...` gives the latest commit, branch count,
  and tag count. No hooks, no pager, no network.

`List` degrades per repository: a metadata failure logs the repository name and
yields a name-only entry rather than failing the page. Each call is bounded by
a short `context.WithTimeout` so one pathological repository cannot hang the
index.

The index makes one `authz.Decide(id, name, OpRead)` call per repository. The
returned `Decision` both filters the list and supplies the push badge:
`CanCreate` is true exactly when the viewer holds write, so the page needs no
second decision per row. The summary page repeats the same call for its single
repository.

Alternatives considered:

- **Read refs and objects directly in Go:** reimplements git's ref resolution
  and packed-refs handling for no user-visible gain. Rejected.
- **One `git log` per branch:** more subprocesses for the same answer. Rejected.
- **Cache metadata in memory or on disk:** freshness and invalidation cost more
  than the subprocess at a few dozen repositories. Deferred; see Open
  Questions.

### Read-only, with no mutation surface

The UI never creates, renames, or deletes anything. Repository creation remains
the exclusive result of an authorized receive-pack, which is already specified.
This is also the security decision: the only state a request can change is the
response body, so there is no CSRF token, no Origin check, and no form action
to get wrong. A future create/delete UI would need that analysis explicitly;
it is not smuggled in here.

### Identity resolution in `web`, not middleware

`web.Handler` holds an `identity.Resolver` and resolves `r.RemoteAddr` itself,
mirroring `transport`. It logs the failure and returns 503. This duplicates
about five lines, but keeps both front ends independently testable with
`identity.Fixed` and leaves `transport` untouched. A shared middleware would be
the right move at a third front end, not a second.

### Repository text is escaped and pages are self-contained

`html/template` with `go:embed` for four templates (`base`, `index`, `repo`,
`error`), one stylesheet, and one SVG mark. Page templates are parsed into
separate template sets so their `content` blocks do not collide.

Render rules:

- No JavaScript. Theme selection is CSS only.
- `description`, commit subjects, and names are escaped by `html/template`;
  invalid UTF-8 is normalized before it reaches the template.
- Repository links escape each path segment with `url.PathEscape`. `ParseName`
  permits spaces and non-ASCII, so `path.Join` alone would produce broken or
  ambiguous links.
- The clone URL uses the request's scheme (`r.TLS`) and host (`r.Host`); no
  forwarded header is consulted.
- Response headers: `Content-Security-Policy: default-src 'none';
  style-src 'self'; img-src 'self'; base-uri 'none'; form-action 'self';
  frame-ancestors 'none'`, plus `X-Content-Type-Options: nosniff` and
  `Referrer-Policy: no-referrer`.
- `404` for denied and missing; `503` for identity failure; `500` only when
  template execution fails after headers are still writable.

### Hand-written CSS with semantic tokens

One `web.css`, roughly 200-250 lines: a small reset, tokens, page chrome,
table, badges, mono block, empty state, definition list, and one narrow-screen
breakpoint. No framework, no utility classes, no inline `style` attributes
(the CSP forbids them anyway).

| Token | Light | Dark |
|---|---|---|
| `--bg` | `#FFFFFF` | `#1F1E1E` |
| `--surface` | `#F7F5F4` | `#232222` |
| `--border` | `#EEEBEA` | `#2E2D2D` |
| `--border-strong` | `#DAD6D5` | `#3D3B3B` |
| `--text` | `#2E2D2D` | `#EEEBEA` |
| `--text-muted` | `#706E6D` | `#AFACAB` |
| `--accent-text` | `#3F5DB3` | `#60A5FA` |
| `--accent-fill` | `#4B70CC` | `#4B70CC` |
| `--code-bg` | `#F7F5F4` | `#2A2929` |

The light column is golink's palette; the dark column is tsidp's. One
correction is deliberate: tsidp's `#706E6D` muted text on `#1F1E1E` is about
3.5:1, below AA, so dark muted text uses gray-400 `#AFACAB` (about 7.4:1).
Dark links use `#60A5FA` rather than `#3B82F6` for the same reason.

Selection is CSS only:

```css
:root { color-scheme: light dark; /* light tokens */ }
@media (prefers-color-scheme: dark) {
  :root { /* dark tokens */ }
}
```

`color-scheme` makes native scrollbars, form controls, and default UI match.
`<meta name="color-scheme" content="light dark">` and matching `theme-color`
metas cover browser chrome. No transition is applied on theme change.

Alternatives considered:

- **Tailwind:** would add a Node toolchain and a generated artifact for a
  two-page UI. The project's stdlib-first stance makes hand-written CSS the
  lower-cost choice.
- **A manual toggle with a cookie or `localStorage`:** explicitly deferred. It
  would add either server state or JavaScript, and the token structure leaves
  room for it later.
- **An external font (Inter):** the tailnet may be offline; use a system font
  stack and a mono stack for identifiers.

### Static assets and caching

`web.css` and `favicon.svg` are embedded and served by explicit handlers with
the right content type and an `ETag` derived from the embedded bytes.
`Cache-Control: no-cache` plus the ETag keeps an upgraded binary from pairing
new HTML with stale CSS without needing a content-hashed URL. A hashed asset
URL and immutable caching are a later optimization, not a first-slice need.

### Wiring

`cmd/tsgit` builds one store, one resolver, and one logger, then:

```go
git := transport.NewHandler(resolver, store, backend, logger)
ui := web.New(web.Config{Resolver: resolver, Store: store, Log: logger, Version: version})
root := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
    if transport.Matches(r) {
        git.ServeHTTP(w, r)
        return
    }
    ui.ServeHTTP(w, r)
})
```

The same composition is used by `--dev-listen`, so the temporary root and
development identity exercise the UI with no tailnet. The version string moves
from a package variable in `main` into the web config for the footer.

## Risks / Trade-offs

- **One git subprocess per repository per index load** -> a few dozen
  repositories stay in the low tens of milliseconds. Every call is timed out,
  and a failure degrades one row. A metadata cache is the measured next step,
  not a default.
- **A very large repository root makes the index linear in repositories** ->
  acceptable at the project's stated tailnet scale; a cap or cache is a later
  change.
- **Repository descriptions and commit subjects are attacker-controlled** ->
  `html/template` escaping, UTF-8 normalization, and a no-script CSP. The
  description is never rendered as markup.
- **A malformed git request could be mistaken for a summary page** -> dispatch
  uses the git handler's own parser, and a test asserts that a git-shaped
  request is refused rather than rendered.
- **`git for-each-ref` on a hostile bare repository** -> read-only plumbing,
  no hooks or pager, bounded by a timeout.
- **Default branch mismatch leaves `HEAD` unborn while `master` exists** -> the
  summary reports the existing refs instead of claiming the repository was
  never pushed.
- **The UI could drift from the capability model** -> it calls the same
  `authz.Decide(OpRead)` as git, and tests assert that an unreadable repository
  is absent from the index and 404 on its summary page.
- **Copying the reference apps too literally** -> borrow the warm palette,
  spacing, and dot-grid geometry, but use a tsgit mark and no third-party
  attribution. The Tailscale logo and "From the nerds at Tailscale" wording
  belong to another project and are not part of this design.
- **`git-serving` delta depends on another change** -> `openspec archive`
  refuses a MODIFIED requirement when the target spec does not exist.
  `add-http-git-serving` must archive first; `openspec validate --strict`
  reports this as informational until then.

## Migration Plan

No data migration and no configuration change. This is a binary-only addition:
the repository tree, state directory, capability contract, and git endpoints
are untouched.

Deployment and rollback:

1. Build and replace the binary. The index appears at `/`; git URLs behave
   exactly as before.
2. Roll back by reverting the binary. The repository tree needs no conversion,
   and an open clone or push is unaffected.
3. Update `docs/project.md` (discovery UI in scope, code browsing still out),
   `docs/architecture.md` (front-end layer and HTTP surface), and
   `openspec/config.yaml`'s out-of-scope wording if it still reads as if no UI
   at all is permitted.
4. Archive `add-http-git-serving` before this change so the `git-serving` delta
   has a main spec to modify. Then archive this change and sync
   `repository-discovery` into `openspec/specs/`.

## Open Questions

- Whether a manual light/dark toggle is worth a cookie or a small script. The
  tokens and media query are structured so either can be added without changing
  the pages.
- Whether repository metadata needs a short-lived cache once the index is slow
  enough to notice.
- Whether a plain-text README panel or real code browsing belongs in a later
  change. Both are outside this capability by intent, not by accident.
