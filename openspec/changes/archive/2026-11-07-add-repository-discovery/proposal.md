# Proposal

## Why

`tsgit` can serve, create, and authorize repositories, but it gives a person no
way to see what exists without already knowing a URL. Discovery is already in
the project's scope; only a code-browsing web UI is out of scope. A small
read-only surface closes that gap using the identity the tailnet already
provides, so it adds no accounts, no sessions, and no new data store.

Now is the moment because the smart-HTTP slice is nearly complete. The binary
serves git but returns 404 for `/`, which is the first thing a person tries.

## What Changes

- Add a read-only HTML front end with two pages: a repository index at `/` and
  a repository summary at `/<namespace>/<repo>`.
- The index lists only repositories the viewer may read, sorted by latest
  activity, with each repository's clone URL and whether the viewer may push.
- The repository summary shows the description, default branch, latest commit,
  branch and tag counts, and the clone URL. An empty repository shows the
  commands to push its first commit. There is no tree, blob, diff, search, or
  commit browsing.
- Extend `internal/repos` with listing and metadata reads: canonical names,
  containment through `Resolve`, git's `description` and `HEAD`, and one
  `git for-each-ref` per repository. Metadata failure degrades a single row; it
  never fails the page.
- Add an HTTP dispatcher: requests transport already recognizes as smart HTTP
  go to the git handler; every other path goes to the discovery handler. The
  four git endpoints and `.git`-less clone keep working unchanged.
- Present both light and dark themes with hand-written CSS and
  `prefers-color-scheme`. No CSS framework, no JavaScript, no external assets,
  no build step.
- Update `docs/project.md` and `docs/architecture.md` to describe the
  discovery front end and the boundary between discovery and code browsing.

Out of scope: code browsing (trees, blobs, diffs, README rendering), search,
issues, pull requests, repository creation or deletion from the UI, and any
change to how pushes authorize or create repositories.

## Capabilities

### New Capabilities

- `repository-discovery`: a read-only HTML surface that resolves the viewer
  from the tailnet, lists only repositories the viewer may read, renders a
  repository summary with its clone URL, and refuses denied or missing
  repositories identically.

### Modified Capabilities

- `git-serving`: the "unsupported request is refused" requirement is narrowed
  to unsupported **smart-HTTP** requests. A path outside the git endpoints may
  now be served by the discovery front end, which starts no git backend. The
  four git endpoints and their behavior are unchanged.

## Impact

- New package `internal/web` with embedded templates, CSS, and a favicon.
- `internal/repos` gains `List` and `Info`, plus metadata types. It remains
  identity-agnostic; filtering stays in the front end.
- `internal/transport` exports its route predicate so dispatch cannot drift
  from what the git handler accepts.
- `cmd/tsgit` composes the dispatcher and wires the same identity resolver and
  repository store into both front ends, in tailnet and development mode.
- No new Go dependencies: `html/template` and `go:embed` are stdlib.
- No configuration flags, no database, no on-disk metadata, and no change to
  the repository layout.
- Documentation changes are required; `add-http-git-serving` should archive
  before this change so both edit settled text and the `git-serving` delta has
  a main spec to modify.
