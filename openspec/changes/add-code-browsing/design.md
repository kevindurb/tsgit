# Design

## Context

The current web surface is deliberately metadata-only. `internal/web` parses
exactly two path segments and renders the index and repository summary
(`internal/web/web.go:197-212`). `internal/repos` already shells out to git for
`for-each-ref` (`internal/repos/repos.go:457-494`), and `internal/transport`
owns smart-HTTP routing; `cmd/tsgit` dispatches with `transport.Matches` first
(`cmd/tsgit/serve.go:93-109`). The public composition reuses the same `web`
handler with `Public: true` and mounts no deletion handler
(`cmd/tsgit/tailnet.go:217-228`). `publicSurface` currently bounds request
bodies and caps concurrent pack POSTs only (`cmd/tsgit/public.go:73-108`).

Constraints that shape the approach:

- Identity still comes from the resolved peer, or the public principal on
  Funnel; authorization is a value-level `authz.Decide` before any git process.
- Repositories are bare directories; there is no database or index.
- The UI is server-rendered, JavaScript-free, with a restrictive CSP and no
  external assets (`internal/web/web.go:277-284`).
- The project is stdlib-first, but a Markdown parser is a justified dependency.
- Clone URLs, smart-HTTP behavior, and the deletion surface must not change.

## Goals / Non-Goals

**Goals:**

- Add read-only tree, blob, ref, and README views without moving the identity
  or authorization boundary.
- Keep the existing summary and index behavior, with the README added to the
  summary.
- Serve the same browsing surface on the Funnel listener under a bounded
  resource budget.
- Keep repository reads as git plumbing calls, consistent with ADR 0001.

**Non-Goals:**

- Commit history, per-file history, diffs, blame, search, or syntax
  highlighting.
- Editing files, creating branches, or any write or download endpoint.
- Per-directory README rendering, per-file "last commit" metadata, or a file
  finder. These are deferred without changing the route shape.

## Decisions

### Routes and dispatch

```
/<ns>/<repo>                          summary (HEAD, plus README)
/<ns>/<repo>/tree[?ref=<ref>]        directory listing
/<ns>/<repo>/tree/<path...>?ref=     directory listing
/<ns>/<repo>/blob/<path...>?ref=     file view
```

`transport.Matches` remains the first dispatch decision, then the deletion
surface, then `web`. Inside `web`, the first two path segments are still parsed
with `repos.ParseName`; anything after them selects tree or blob. The ref lives
in the query so `feature/login` is never ambiguous with a nested directory.
Generated links omit `ref` at the default branch and carry it otherwise.

A `.git`-suffixed browsing URL redirects to the canonical path, preserving the
tail and query. A tree route naming a file, or a blob route naming a directory,
redirects to the correct noun using validated path components rather than the
raw URL. Alternatives considered: a single `/src/` route that infers file vs
directory (fewer routes, but redirects and explicit intent are cheap); ref in
the path (familiar, but Go's decoded `r.URL.Path` turns `%2F` into a separator).

### Ref resolution

```
value                                   resolution
--------------------------------------  ------------------------------------------
absent                                  HEAD
refs/heads/<name>, refs/tags/<name>     exact ref
40- or 64-hex, resolves to a commit     that commit
40- or 64-hex, no such object           fall back to short branch/tag name
anything else                           exact branch, then exact tag
none of the above                       404
```

Only `refs/heads/` and `refs/tags/` are accepted as full refs. A full-length
hex value is tried as a commit first, so permalinks are reliable; a pathological
branch named with 40 hex characters remains reachable as `refs/heads/...`.
Resolution uses `git rev-parse --verify --end-of-options <value>^{commit}`,
which peels annotated tags and rejects trees and blobs. Short names are matched
against the refs already enumerated for the page, preferring an exact branch and
using the full tag ref for a colliding tag. Hex input is compared
case-insensitively and generated links are lowercase.

The summary stays HEAD-only. A `?ref=` on the summary returns a 302 to the tree
with the same ref; this avoids a second versioned summary surface.

### Resolve the ref to a commit once

Every browsing request resolves the selected ref to a commit ID before reading
the tree or a blob, then uses that commit for all reads. This makes the listing,
file, and README come from one snapshot even if a push moves the ref mid-request.
It also means a SHA view and a branch view of the same commit produce identical
content.

`repos.Info.Latest` must not be reused for this. It is the newest commit across
all branches and tags, not the `HEAD` tip. The summary README resolves `HEAD`
explicitly (or `refs/heads/<branch>` when attached, `HEAD` when detached). The
browsing page header reads the selected commit's short hash, subject, and date
with one `git show -s`.

A deletion that races a browsing request makes the git read fail; the handler
logs it and returns not-found or a server error, never a partial page.

### Repository reads

Add focused read methods to `internal/repos`:

- `ResolveRef(ctx, name, ref) (Commit, error)` — validate, resolve, peel.
- `Tree(ctx, name, commit, path) ([]Entry, error)` — list a directory.
- `Blob(ctx, name, commit, path) (Blob, error)` — metadata and capped contents.
- `Commit(ctx, name, commit) (Commit, error)` — header metadata.
- `Readme(ctx, name, commit) (Blob, bool, error)` — the root README, if any.

`Info` stays unchanged. README reading is deliberately separate: `List` calls
`Info` for every repository on the index, and the index must not fetch or parse
README content. All git calls use the store's git path, a `context.WithTimeout`
(two seconds for refs and commit metadata, five for tree and blob reads), and
the existing containment resolution before any process starts.

Tree type is resolved with `git cat-file -t <commit>:<path>`; a tree is listed
with `git ls-tree -z -l <commit>:<path>`, and the root with `<commit>^{tree}`.
Using `<commit>:<path>` as the tree-ish avoids pathspec magic and trailing-slash
rules. Records are NUL-delimited and parsed as mode, type, object ID, optional
size, then the path after the separator; never with `strings.Fields`, because a
filename may contain spaces, tabs, or newlines. Blobs are sized with
`git cat-file -s` before any content is read.

Alternative considered: one `git cat-file --batch` process per request. It is a
reasonable optimization later, but one process per request is simpler and fine
for a tailnet-sized forge.

### Path handling

The path after `/tree/` or `/blob/` is a git tree path, never a filesystem path.
Components are split on `/`; empty, `.`, `..`, and control-character components
are rejected. Leading dots are allowed, so `.github` is browsable. Depth and
total length are capped (32 components and 4 KiB). The request path is never
joined onto the repository directory; it is passed to git as part of
`<commit>:<path>`.

### Blob rendering

The size cap is applied before reading (`cat-file -s`). Above the cap the page
reports "too large" and renders nothing. Text is decoded as UTF-8 with invalid
sequences replaced, exactly as the existing description and ref rendering do,
and is escaped by `html/template`. A NUL byte in the first 8 KiB marks the blob
binary; a binary is identified, not rendered. An empty blob renders an empty
view. A symlink (`120000`) shows its target text and is not followed. A submodule
(gitlink, `160000`) shows its commit ID and no content.

There is no raw or download endpoint. Serving repository bytes from the tsgit
origin with an attacker-influenced content type is the classic stored-XSS path
(notably SVG or HTML); cloning is already the way to obtain the bytes. This is a
deliberate non-goal, not an omission.

### README detection and safe Markdown

Detection is limited to the selected root tree. Names are matched
case-insensitively in preference order `README.md`, `README.markdown`, then an
extensionless `README`. A preferred entry that is a symlink, gitlink, binary, or
above the cap is not rendered.

Rendering uses `github.com/yuin/goldmark` with GFM, linkify, and footnote
extensions and auto heading IDs. `html.WithUnsafe()` is not enabled, so raw HTML
never reaches the page as markup. The rendered HTML is passed to the template as
`template.HTML`; the trust boundary is therefore the parser, the renderer, and
the link policy below, not arbitrary repository HTML.

An AST transform enforces the link and image policy before rendering:

- Allow `http`, `https`, and `mailto` destinations.
- Resolve a relative destination against the README's directory with
  `path.Clean`; reject it if it escapes the repository.
- Rewrite an in-repository destination to the matching `/blob` or `/tree` route
  with the current ref; preserve a fragment.
- Replace an absolute-path link, an unsupported scheme, or a link that escapes
  the repository with its text content.
- Replace an image with its alt text. This keeps external images out of the page
  and leaves `img-src 'self'` untouched.

The CSP is not widened. A post-render allowlist sanitizer is possible defense in
depth, but with raw HTML disabled and destinations filtered, it would duplicate
the renderer's guarantees; it can be added later without changing the spec.

Alternatives considered: `blackfriday` or `gomarkdown` (older maintenance and
weaker extension story than goldmark); rendering Markdown but allowing raw HTML
(rejected: reintroduces the trust bypass); a Markdown parser plus a general HTML
sanitizer (reasonable but a larger dependency and allowlist to maintain).

### Summary integration

`servePath` already resolves identity, authorizes, and reads `Info`. After that,
the summary handler resolves `HEAD`, calls `Readme`, renders it with the same
pipeline, and includes it in the page below the metadata. A README read or
render failure logs and leaves the metadata page intact. The summary's default
branch row links to `/tree`; the index is untouched and never renders a README.

### Public composition and limits

`composePublic` already reuses `web.New` with `Public: true` and no delete
handler, so the new routes are served on Funnel without a second handler. The
public page must reuse the same write/delete suppression and the same link
policy; a README cannot link to the deletion surface because absolute paths
become text and the route is structurally absent on the public listener.

`publicSurface` gains a second classification: a GET or HEAD whose path is a
tree or blob route acquires the same concurrency slot used by pack exchanges.
A shared `--funnel-max-concurrent` budget is the simplest single bound and
satisfies the requirement to cap both pack exchanges and browsing reads. The
trade-off is that a few long clones can temporarily starve browsing; the error
is retryable and browsing reads are short. A separate browsing budget can be
added later without changing the spec.

Browsing reads also get the `repos` timeouts and the blob/README size caps.
The existing public request logging already records the principal and the
client source address.

## Risks / Trade-offs

- **Markdown remains a trust boundary** → raw HTML off, explicit URL policy,
  generated markup escaped, payload tests; a sanitizer can be layered on later.
- **One git process per browsing request** → bounded by timeouts and caps; large
  clone and browsing concurrency share one public budget.
- **`ls-tree` parsing is easy to get wrong** → `-z`, split the prefix rather than
  whitespace, and test names with spaces, tabs, newlines, and Unicode.
- **README on the summary adds work to a page** → only the per-repository summary,
  never the index; a README failure degrades to the metadata page.
- **Ref ambiguity** → explicit resolution table; full refs escape collisions;
  full-length hex is trusted as a commit first.
- **Unreachable commit IDs can be pruned** → a SHA is a stable name, not a
  permanent guarantee; a pruned commit returns not-found.
- **Anonymous browsing load** → shared public semaphore, short timeouts, and
  size caps; no raw endpoint to amplify responses.
- **New dependency** → goldmark is pure Go, maintained, CommonMark-compliant, and
  pinned; the alternative is reimplementing CommonMark, which is not justified.
- **Documentation drift** → tasks include updating the project scope, README,
  architecture, configuration docs, and the capability purpose line.

## Migration Plan

The change is additive: no repository data, storage format, or configuration
migration. Browsing creates nothing, and reverting the binary removes the routes
without affecting existing repositories. Public browsing is enabled by the
existing Funnel opt-in; deployments that do not enable Funnel are unaffected.
`--funnel-max-concurrent` keeps its name but its documentation broadens from
"pack exchanges" to "public git operations."

## Open Questions

None that affect the specs, approach, or task breakdown.
