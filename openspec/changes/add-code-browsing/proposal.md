# Proposal

## Why

The discovery UI tells a tailnet user which repositories exist and how to clone
them, but not what is inside one. Reading a README or checking a single file
means cloning the whole repository. A small, read-only browser removes that
round trip without adding accounts, a database, or a GitHub-sized feature
surface.

This deliberately revisits the project's "no code browsing" non-goal. The
boundary being drawn is narrow: view the default branch, browse a directory,
read a file, select a branch/tag/commit, and render the README. History, diffs,
blame, search, syntax highlighting, issues, and editing remain out of scope.
The project stays a tailnet forge rather than becoming a GitHub replacement.

## What Changes

- Add read-only browsing routes:
  - `GET /<ns>/<repo>/tree[?ref=<ref>]` and
    `GET /<ns>/<repo>/tree/<path...>?ref=<ref>` for directory listings.
  - `GET /<ns>/<repo>/blob/<path...>?ref=<ref>` for file contents.
- Resolve `ref` as a branch short name, a tag short name, a full
  `refs/heads/...` or `refs/tags/...` name, or a full 40- or 64-hex commit ID.
  An absent ref means `HEAD`. Invalid refs and non-commit objects return the
  repository's not-found response.
- Show directory entries with type, size, and mode-derived details. Render text
  blobs with size, binary, encoding, and empty-file handling. Do not follow
  symlinks or submodule entries.
- Render the README on the repository summary at `HEAD` and on the tree root at
  the selected ref. Render Markdown in safe mode with raw HTML disabled, an
  explicit URL-scheme allowlist, and relative links rewritten to browsing routes
  with the current ref. Images degrade to alt text.
- Keep the summary HEAD-only; a `?ref=` on the summary redirects to the tree.
- Serve browsing and README rendering on the Funnel listener for public
  repositories, with bounded concurrency, timeouts, and response-size caps.
- Add a Markdown parser dependency (goldmark) with the raw-HTML renderer
  disabled.
- Amend the `repository-discovery` summary requirement to allow the README and
  expand the `funnel-access` public surface and limit requirements.
- Update project scope documentation and record the boundary change in an ADR.

## Capabilities

### New Capabilities

- `code-browsing`: Read-only tree, blob, ref selection, and safe README
  rendering for repositories the viewer may read.

### Modified Capabilities

- `repository-discovery`: The repository summary now renders the default
  branch's README in addition to the existing metadata, while still forbidding
  other trees, blobs, diffs, and commit lists.
- `funnel-access`: The public surface now includes read-only browsing and README
  rendering for public repositories, and the public limits cover git-backed
  browsing requests in addition to pack exchanges.

## Impact

- `internal/web`: new browsing routes, templates, styles, README renderer, link
  rewriting, and shared not-found handling.
- `internal/repos`: ref resolution, tree listing, blob reads, and commit
  metadata. README reading stays a separate call so the repository index does
  not fetch or parse README content for every repository.
- `cmd/tsgit`: wire browsing into the existing `web` handler; extend the public
  surface guard to bound git-backed browsing requests.
- `go.mod`: add a Markdown parser (`github.com/yuin/goldmark`).
- Specs and docs: `repository-discovery`, `funnel-access`, `docs/project.md`,
  `docs/architecture.md`, `README.md`, the OpenSpec project context, and a new
  ADR recording the scope change.
- No new identity path, authorization rule, database, or storage format. The
  browser shells out to git plumbing for reads, consistent with ADR 0001, and
  decides authorization before any subprocess starts.
