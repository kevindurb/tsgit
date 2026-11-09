# 4. Add basic read-only code browsing

## Status

Accepted.

## Context

The project deliberately kept code browsing out of scope: `docs/project.md`
listed trees, blobs, diffs, and README rendering alongside issues and pull
requests as things tsgit is not trying to be. The discovery UI could show which
repositories exist and how to clone them, but reading a README or checking one
file meant cloning the repository.

The narrow gap is the README and a single file or directory. A read-only browser
removes that round trip without adding accounts, a database, or GitHub-sized
feature surface. The change is specified by:

- `openspec/changes/add-code-browsing/specs/code-browsing/spec.md`
- the modified `repository-discovery` and `funnel-access` requirements in the
  same change.

## Decision

1. **Browsing is read-only and narrow.** The surface is a summary, a directory
   tree, and a file view. It supports a branch, tag, full ref, or full commit ID,
   and renders the root README. Commit history, diffs, blame, search, syntax
   highlighting, and editing remain out of scope. There is no raw/download
   endpoint: serving attacker-influenced bytes from the tsgit origin is a
   stored-XSS path, and cloning already obtains the bytes.
2. **Identity and authorization are unchanged.** The handler resolves the same
   tailnet peer (or the Funnel read-only principal), calls
   `authz.Decide(..., OpRead)` before resolving a path or starting a process, and
   returns the same not-found response for denied and missing repositories.
   `internal/authz` still takes values, not connections.
3. **Repository reads stay git plumbing.** ADR 0001 governs. New methods on
   `internal/repos` resolve a ref once to a commit, list trees with
   `git ls-tree -z -l`, size blobs before reading them, and read README content
   separately so the index never fetches it. Paths are git tree paths, never
   filesystem paths.
4. **Markdown is untrusted.** `github.com/yuin/goldmark` renders with raw HTML
   disabled. An AST transform allows `http`, `https`, `mailto`, and
   in-repository relative links; relative links are rewritten to browsing routes
   with the current ref; absolute, escaping, unsupported-scheme, and image
   destinations degrade to text. The CSP and `img-src 'self'` are not widened.
5. **Chosen limits.** Tree and blob reads get a five-second timeout, ref and
   commit reads two seconds. Blobs are capped at 1 MiB and READMEs at 512 KiB;
   oversized content is identified, not streamed. Public browsing shares the
   existing `--funnel-max-concurrent` budget with pack exchanges, so one bound
   covers every expensive public git operation. Browsing reads are short, and a
   saturated budget returns a retryable `503`.
6. **The default branch is the only summary ref.** The summary renders the
   README at `HEAD`; a `ref` query on the summary redirects to the tree. A tree
   root renders the README at its selected ref. The index never renders README
   content.

## Consequences

- **One dependency.** Goldmark is pure Go, maintained, and CommonMark-compliant.
  The alternative is reimplementing CommonMark or accepting a weaker parser.
- **One git process per browsing read.** The design is intentionally simple for
  a tailnet-sized forge. Timeouts, size caps, and the shared public concurrency
  budget bound the cost; a batched `cat-file` backend can be added later without
  changing routes.
- **A few long clones can briefly starve public browsing.** They share one
  semaphore. The failure is a retryable `503`, and a separate browsing budget is
  possible without a spec change.
- **Markdown remains a trust boundary.** Raw HTML is off and destinations are
  filtered. A post-render allowlist sanitizer can be layered on later if the
  parser's guarantees are not considered sufficient.
- **The public surface grows.** Public repositories now expose READMEs, trees,
  and blobs. The payload still cannot express write access, and the public view
  never shows push or deletion affordances.
- **Scope documentation changed.** `docs/project.md`, `README.md`,
  `docs/architecture.md`, and the OpenSpec project context now describe basic
  read-only browsing as in scope while keeping history, diffs, search, and
  editing out.

## Alternatives considered

- **Keep cloning for READMEs.** Rejected: the round trip is the problem this
  change removes.
- **A single `/src/` route that infers file versus directory.** Fewer routes, but
  explicit tree/blob routes make redirects and intent cheap.
- **Put the ref in the path.** Familiar, but Go's decoded `r.URL.Path` turns
  `%2F` into a separator and makes `feature/login` ambiguous with a directory.
- **Render Markdown with raw HTML enabled.** Rejected: it reintroduces a trust
  bypass through repository-controlled markup.
- **A general HTML sanitizer.** Reasonable, but a larger dependency and
  allowlist to maintain on top of a parser that already disables raw HTML.
- **A separate public browsing semaphore.** Cleaner isolation, but a second knob
  for the same resource and not needed for the current scale.
