# Proposal

## Why

The file-browser tree table still reads like a plumbing readout: a separate
"Type" column of raw kind words, a "Parent directory" paragraph above the
table, and no visual distinction between entries. Small presentation changes
make the page faster to scan and match how other file browsers behave, without
changing any browsing semantics.

## What Changes

- The tree table gains a leading icon column. Icons are inline SVG (no new
  asset routes, CSP-compatible), one per entry kind — directory, file,
  executable, symlink, submodule — with extension-based icons for common file
  types (docs, code, images, archives, config, PDF, HTML/CSS, lockfiles and
  dotfiles) layered on top of the kind default.
- Directories are shown with a trailing slash (`docs/`); the href is unchanged.
- The "Type" column is removed. The kind remains available to assistive
  technology as visually-hidden text; the visible signal is the icon plus the
  trailing slash for directories.
- A `../` row is the first row of the table whenever the current path has a
  parent, replacing the separate "Parent directory" paragraph. Size and mode
  render as an em dash on that row. When a directory is empty, the table still
  renders with the `../` row instead of only the empty-state message.
- The blob page drops its "Parent directory" paragraph; its breadcrumbs
  already provide the way up.
- The `Mode` and `Size` columns are unchanged.
- The blob page's `.parent-link` markup and CSS are removed.

No change to browsing semantics: refs, paths, authorization, redirects, and
sorting are all untouched.

## Capabilities

### New Capabilities

<!-- none -->

### Modified Capabilities
- `code-browsing`: the Directory listing requirement changes — entries present
  an icon and directory-trailing-slash names instead of a visible Type column,
  and the way-up link moves from a standalone paragraph into a first row of the
  listing. The empty-listing behavior changes to keep the up-one-level row.

## Impact

- `internal/web/templates/tree.html`, `internal/web/templates/blob.html`,
  `internal/web/static/web.css`
- `internal/web/browse.go` (`entryView` gains an icon discriminator;
  `treeEntries` maps kind + extension to icon)
- `internal/web/*_test.go` assertions that reference the Type column
- No new dependencies, no new endpoints, no schema/storage changes
