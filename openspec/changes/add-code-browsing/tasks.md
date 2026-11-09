# Tasks

## 1. Dependency setup

- [x] 1.1 Add `github.com/yuin/goldmark` at a pinned version to `go.mod` and
  `go.sum`; verify `go mod tidy` leaves the module graph clean and
  `go build ./...` succeeds.

## 2. Repository read layer

- [x] 2.1 Add tree-path parsing to `internal/repos` that rejects empty, `.`,
  `..`, and control-character components, allows leading dots, and enforces the
  depth and length bounds; verify unit tests for each rejected and accepted
  case.
- [x] 2.2 Add `ResolveRef` implementing the branch, tag, full-ref, and full-hash
  resolution table, including SHA-first fallback, tag peeling, non-commit
  rejection, case normalization, and detached `HEAD`; verify table-driven tests
  for every row and for a branch named like a full hash.
- [x] 2.3 Add commit metadata reading for the browsing header and for the
  summary's `HEAD`; verify tests with an attached branch, an annotated tag, and
  a detached commit, and verify `Info.Latest` is not used as the `HEAD` tip.
- [x] 2.4 Add `Tree` using `git cat-file -t` to distinguish a tree, then
  `git ls-tree -z -l` to list it; verify tests for a root listing, a nested
  directory, symlinks, submodules, and filenames containing spaces, tabs,
  newlines, and Unicode.
- [x] 2.5 Add `Blob` with a size check before reading, binary detection, UTF-8
  normalization, empty-file handling, symlink target display, and submodule
  rejection; verify a unit test for each case and assert no bytes are read for
  an oversized blob.
- [x] 2.6 Add `Readme` detection with case-insensitive preference for
  `README.md`, `README.markdown`, then extensionless `README`, skipping
  symlink, gitlink, binary, and oversized entries; verify unit tests for
  preference order, case variants, an absent README, and each skipped kind.
- [x] 2.7 Bound every new git read with a short context timeout and return a
  typed error the handler can map to not-found or a server error; verify a test
  with an already-canceled context returns promptly and does not panic.

## 3. Markdown rendering

- [x] 3.1 Add a safe-mode goldmark renderer with GFM, linkify, footnote, and
  auto heading IDs, without `html.WithUnsafe()`; verify tests that raw
  `<script>`, event-handler attributes, and `<style>` do not reach the output
  as markup.
- [x] 3.2 Add an AST transform enforcing the URL policy: allow `http`,
  `https`, and `mailto`; rewrite in-repository relative links with the current
  ref; render absolute-path, escaping, and unsupported-scheme links as text;
  and replace images with alt text; verify table tests for each destination
  category.
- [x] 3.3 Add README size capping and invalid-UTF-8 normalization before
  parsing, and escape generated text; verify tests for an oversized README, an
  invalid-sequence README, and repository text containing markup.

## 4. Web routes, templates, and summary

- [x] 4.1 Extend `internal/web` routing for `/tree` and `/blob`, keep the
  two-segment summary intact, redirect `.git`-suffixed browsing URLs with the
  tail and query, and redirect a tree/blob noun mismatch; verify handler tests
  for each route and redirect.
- [x] 4.2 Add the tree template with breadcrumbs, a script-free ref switcher,
  a deterministic entry table, type and size display, and preserved ref links;
  verify handler tests for a root listing, a nested directory, and names with
  spaces and non-ASCII characters.
- [x] 4.3 Add the blob template and file view; verify handler tests for text,
  binary, oversized, empty, symlink, and submodule entries, and assert that
  repository markup is escaped.
- [x] 4.4 Render the README on the summary at `HEAD` and on the tree root at
  the selected ref, and degrade to the metadata page when the README cannot be
  read or rendered; verify handler tests for both placements, a missing README,
  and a README failure.
- [x] 4.5 Suppress write badges, deletion actions, and first-push instructions
  on public browsing pages, and ensure README links cannot reach the deletion
  surface; verify tests with the public principal assert those affordances are
  absent.
- [x] 4.6 Enforce identity resolution before store access, identical 404s for
  denied and missing repositories, 405 for mutating methods, and a logged 503
  on identity failure for browsing routes; verify handler tests for each.
- [x] 4.7 Add CSS for the tree table, blob view, breadcrumbs, ref switcher, and
  Markdown elements without JavaScript or external assets; verify a handler
  test asserts no external stylesheet, font, image, or script reference and
  check light and dark rendering manually.

## 5. Public listener limits

- [x] 5.1 Classify public browsing GET and HEAD requests and acquire the same
  expensive-operation slot used by pack exchanges in `publicSurface`; verify
  unit tests for the classifier and a handler test that a full slot returns a
  retryable 503 for browsing.
- [x] 5.2 Broaden the `--funnel-max-concurrent` help text and
  `docs/configuration.md` from pack exchanges to public git operations; verify
  the documented flag matches the code and `go test ./...` still passes.
- [x] 5.3 Apply the browsing timeouts and blob/README size caps on the public
  path; verify a public handler test with an oversized README returns the
  "too large" notice rather than streaming content.

## 6. Documentation and scope

- [x] 6.1 Update `docs/project.md` so trees, blobs, ref selection, and safe
  README rendering are in scope while history, diffs, search, and editing stay
  out; verify a read-through does not contradict `proposal.md`.
- [x] 6.2 Update `docs/architecture.md` with the browsing routes, the repository
  read methods, and the revised public limit; verify the documented routes and
  limits match the handler tests.
- [x] 6.3 Update `README.md` status and feature list to name read-only code
  browsing and remove the statement that code browsing is unimplemented; verify
  the feature bullets match the implemented routes.
- [x] 6.4 Update the `openspec/config.yaml` context so the out-of-scope wording
  no longer forbids all code browsing; verify `openspec context --json` parses.
- [x] 6.5 Add `docs/decisions/0004-basic-code-browsing.md` recording the
  boundary change, the chosen limits, and the alternatives; verify it cites the
  affected specs and ADR 0001.
- [x] 6.6 Update the `repository-discovery` purpose line in
  `openspec/specs/repository-discovery/spec.md` to remove the blanket "no code
  browsing" claim; verify `just spec-validate` passes.

## 7. Integration verification

- [x] 7.1 Add an integration test with a temporary real git repository
  containing multiple branches, a tag, a detached commit, a README, a binary
  file, and a symlink; exercise the summary, tree, blob, tag, and commit routes
  and verify each response.
- [x] 7.2 Verify existing clone, fetch, push, and deletion routes are unchanged
  by the new dispatch; run the existing transport and deletion tests and an
  end-to-end clone against the test server.
- [x] 7.3 Run `just ci` and `just spec-validate`; verify both pass and fix any
  failure rather than weakening a check.
