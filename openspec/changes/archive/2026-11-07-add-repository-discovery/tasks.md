# Tasks

## 1. Repository metadata

- [x] 1.1 Add `Info` and `Commit` to `internal/repos`, plus `List` and `Info`
  methods that walk the root with `os.ReadDir`, skip `.tsgit-tmp` and names
  `ParseName` rejects, and verify each candidate through `Resolve`; verify unit
  tests cover valid names, invalid names, and a symlinked namespace that
  escapes the root.
- [x] 1.2 Read the `description` file, treating git's "Unnamed repository"
  boilerplate as empty, and read `HEAD` as a symbolic branch or detached hash;
  verify unit tests for a described repository, an undescribed one, and a
  detached `HEAD`.
- [x] 1.3 Collect the latest commit, branch count, and tag count with one
  read-only `git for-each-ref` per repository; verify unit tests against a
  repository with commits, tags, and multiple branches.
- [x] 1.4 Represent an empty repository as an unborn `HEAD`, and when `HEAD`
  names a branch with no commits while another branch exists, report the
  existing refs rather than an empty repository; verify both cases with unit
  tests.
- [x] 1.5 Bound each metadata call with a short context timeout and degrade a
  single repository to a name-only entry on failure; verify a test with a
  non-git directory named `*.git` still lists the repository and logs the
  failure without failing the page.

## 2. Discovery pages

- [x] 2.1 Create `internal/web` with an embedded `base`, `index`, `repo`, and
  `error` template set and a `Config` carrying the resolver, store, logger, and
  version; verify a handler test renders the index with an
  `identity.Fixed` and a temporary store.
- [x] 2.2 Filter the index through `authz.Decide(id, name, OpRead)`, sort by
  latest activity with empty repositories last and a name tiebreak, and show
  each repository's canonical name, clone URL, and whether the viewer may push;
  verify tests for mixed access, a read-only repository, and an empty index.
- [x] 2.3 Render the repository summary with description, default branch,
  latest commit, branch and tag counts, and clone URL, show first-push commands
  for an empty repository, and redirect a `.git`-suffixed summary URL to its
  canonical page; verify tests for a populated repository, an empty one, and
  the redirect.
- [x] 2.4 Return the same 404 for a denied and a missing repository summary and
  create nothing on either path; verify tests assert identical status and that
  no directory appears.
- [x] 2.5 Refuse mutating methods on discovery paths with no repository or ref
  change; verify a test sends `POST` and `DELETE` to a summary path and checks
  both the status and the untouched tree.
- [x] 2.6 Resolve the viewer through the `identity.Resolver`, return a logged
  503 when resolution fails, and ignore a spoofed login header or query value;
  verify tests with a failing resolver and with a `Fixed` identity plus a
  conflicting `Tailscale-User-Login` header.
- [x] 2.7 Escape descriptions, commit subjects, and names, normalize invalid
  UTF-8, and escape each link segment with `url.PathEscape`; verify tests with a
  hostile description and with a repository name containing a space or
  non-ASCII characters.

## 3. Dispatch and headers

- [x] 3.1 Export `transport.Matches` over the existing `parseRoute` and add a
  test table for a git endpoint, a `.git`-less clone URL, the index, a summary
  URL, and a malformed smart-HTTP request.
- [x] 3.2 Add the root dispatcher to `cmd/tsgit` and wire it into both tailnet
  mode and `--dev-listen`, sharing the resolver, store, backend, and version;
  verify `just run serve --dev-listen 127.0.0.1:8080` serves the index, and a
  push followed by a reload shows the new repository.
- [x] 3.3 Set the CSP, `nosniff`, and `Referrer-Policy` headers on HTML
  responses, and serve the embedded CSS and SVG with explicit handlers and an
  `ETag`; verify handler tests assert the headers and asset content types, and
  that no page references an external origin.

## 4. Visual layer

- [x] 4.1 Write the hand-written `web.css` with semantic tokens, page chrome,
  the repository table, badges, mono blocks, empty state, summary definition
  list, focus styles, and one narrow-screen breakpoint; verify it defines both
  light and dark values under `color-scheme` and
  `prefers-color-scheme: dark`.
- [x] 4.2 Add the tsgit SVG mark and `theme-color`/`color-scheme` metadata;
  verify the favicon is served from `/.tsgit/favicon.svg` and the page contains
  no external stylesheet, font, image, or script reference.
- [x] 4.3 Check the rendered index and summary in both color schemes at desktop
  and narrow widths, including keyboard focus and a table with a long
  description; verify manually in a browser and record the result.

## 5. Documentation and integration

- [x] 5.1 Update `docs/project.md` so repository listing and the read-only
  discovery UI are clearly in scope and code browsing remains out; verify a
  read-through does not contradict `proposal.md`.
- [x] 5.2 Update `docs/architecture.md` with the `internal/web` layer row, the
  discovery routes, and the parse-based dispatch rule; verify the documented
  routes match the dispatcher tests.
- [x] 5.3 Update the out-of-scope wording in `openspec/config.yaml` if it reads
  as forbidding any web UI, and confirm `openspec context --json` still parses.
- [x] 5.4 Run `just ci` and `just spec-validate`; verify both pass.
- [ ] 5.5 Confirm `add-http-git-serving` is archived so
  `openspec/specs/git-serving/spec.md` exists, then archive this change and sync
  `repository-discovery` into `openspec/specs/`; verify `openspec validate` no
  longer reports a missing target spec and both main specs exist.
