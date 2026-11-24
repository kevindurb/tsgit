# Design

## Context

See proposal.md for motivation. The tree listing is rendered from
`browsePage.Entries` (`[]entryView`) by `tree.html`; `describeEntry` in
`internal/web/browse.go` already classifies each entry as
directory/submodule/symlink/executable/file and yields a display size.
Verified constraints:

- CSP is `default-src 'none'; style-src 'self'; img-src 'self'` — no external
  assets, no `data:` URIs, no fonts; inline SVG markup in HTML is unaffected.
- `Mobile CSS` collapses `.tree` into a grid with `data-label` pseudo-labels;
  `thead` is hidden there.
- `browse.go` already computes `Parent` (used by both `tree.html` and
  `blob.html`); `blob.html` is losing its use.
- Conventions noted as assumptions during review (not explicitly confirmed,
  kept minimal): `h`/`hpp` treated as code, `xz` as archive.

## Goals / Non-Goals

**Goals:**
- One at-a-glance type signal per row (icon), directory affordance via
  trailing slash, a way-up row inside the table.
- Zero new dependencies, assets, requests, or JS; single inlined SVG sprite.
- Keep the accessible name of each entry meaningful.

**Non-Goals:**
- Relative "last updated" times (needs a history walk; deferred to its own
  change — see proposal scope).
- Per-extension color theming or linguist-scale icon coverage.
- Changing sort order, routes, ref handling, redirects, or the Mode/Size
  columns.
- Touching the blob page beyond removing its parent-link paragraph.

## Decisions

### D1: Inline SVG sprite in `tree.html`, one `<symbol>` per icon
Each `<symbol id="icon-...">` is declared once in the template; rows reference
`<svg class="icon" aria-hidden="true"><use href="#icon-code"></use></svg>`.
Icons are 16px, `fill="none" stroke="currentColor"`, 1.5px stroke, round caps,
matching the favicon's stroke style; color inherits from the row's muted text
tokens.
- Why: CSP-safe (markup, not a resource load), no extra requests, no data
  URIs, trivially cacheable as part of the page.
- Alternatives rejected: per-row inline copies (markup bloat), CSS `mask-image`
  with data URIs (blocked by `img-src 'self'`), an icon font (blocked by CSP,
  extra asset), an external sprite file (extra request, extra asset to serve
  and test).

### D2: Icon selection happens in `treeEntries`, not in the template
`entryView` gains `Icon string` (the symbol suffix). Selection order:
1. `Dir` → `folder`; `Submodule` → `submodule`; `Symlink` → `symlink`;
   mode `100755` → `executable`.
2. Otherwise map the lowercased file extension (after the last dot) to a
   group: doc (`md`, `markdown`, `txt`, `adoc`), code (`go`, `rs`, `py`, `js`,
   `ts`, `jsx`, `tsx`, `c`, `h`, `cc`, `cpp`, `hpp`, `sh`), image (`png`,
   `jpg`, `jpeg`, `gif`, `svg`, `webp`), archive (`zip`, `tar`, `gz`, `tgz`,
   `xz`), config (`json`, `yaml`, `yml`, `toml`, `ini`), pdf (`pdf`),
   markup/style (`html`, `css`).
3. Dotfiles and lockfiles match on the full basename (case-insensitive):
   `.gitignore`, `.dockerignore`, `.editorconfig`, `.env`, `.gitattributes` →
   dotfile; `package-lock.json`, `yarn.lock`, `pnpm-lock.yaml`, `go.sum`,
   `cargo.lock` → lockfile.
4. Otherwise → `file`.

Templates never inspect extensions; they only render `Icon`.

### D3: Directory affordance is display-only
Directories keep their existing `Href` (tree route); the name cell renders
`name/`. Kind text (`directory`, …) moves into a visually-hidden span inside
the link so removing the Type column costs no accessible information. A new
`.visually-hidden` utility class is added; no external CSS changes beyond the
tree table rules and removal of `.parent-link`.

### D4: The way-up row is a normal table row, first in tbody
Rendered only when `browsePage.Parent != ""`: folder icon, `../` as the link
text, em dashes in Size and Mode. The empty-state block is kept only for the
root-at-empty-tree case; a non-root empty directory renders the table with
just the `../` row. `Parent` remains on `browsePage` for the tree template;
`blob.html` drops its use.

### D5: Mode and Size columns stay; icon column is leftmost and unlabeled
The icon `<th>` carries a `.visually-hidden` "Type" for column semantics.
Size/Mode and their `data-label`s are unchanged. Mobile grid gains a leading
auto-width column for the icon; size/mode keep their existing treatment.

### D6: No new JavaScript, no new dependencies, no spec for the icon set itself
Icon membership is a presentation detail; the spec pins the behavior
(type group visible, kind available to AT, `../` row) without enumerating
symbols.

## Risks / Trade-offs

- **Icon drift** (hand-maintained symbols fall out of style) → 16px grid and
  stroke rules fixed in the template; review catches drift; no runtime cost.
- **Empty-directory behavior change** (no separate "This directory is empty"
  message when a parent exists) → intentional; table with a single `../` row
  is self-explanatory. Root-empty keeps the message.
- **Visual density**: one more column on narrow screens → mobile grid keeps
  the icon in its own narrow column; it does not wrap.
- **String-assertion tests** on the tree page churn → tests keep passing
  against sr-only kind text; new assertions cover the `../` row and icon
  symbols; `.parent-link` CSS removal is asserted by its absence.
- **Extension map maintenance** → small and additive; misclassification only
  affects the icon, never correctness or security.

## Migration Plan

No data or state to migrate. Revert = revert the commit(s). No flags.

## Open Questions

None. Assumptions (h/hpp as code, xz as archive) are recorded above and are
safe to adjust during implementation without affecting specs.
