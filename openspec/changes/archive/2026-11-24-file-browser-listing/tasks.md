# Tasks

## 1. Entry icon mapping in the web layer

- [x] 1.1 Add an `Icon string` field to `entryView` in `internal/web/browse.go` and an `iconFor` helper implementing the kind-first, then extension, then basename (dotfiles/lockfiles) mapping from design D2; unit-test `iconFor` for every kind, each extension group, case-insensitivity, and unknown fallback
- [x] 1.2 Populate `Icon` in `treeEntries` and confirm existing tree/blob handler tests still pass (`go test ./internal/web/`)

## 2. Tree template restructure

- [x] 2.1 Rework `tree.html`: inline SVG symbol sprite (16px, stroke `currentColor`, ids `icon-folder`, `icon-file`, `icon-executable`, `icon-symlink`, `icon-submodule`, `icon-doc`, `icon-code`, `icon-image`, `icon-archive`, `icon-config`, `icon-pdf`, `icon-markup`, `icon-lockfile`, `icon-dotfile`), leftmost icon column with a visually-hidden "Type" header, `aria-hidden` icons, `.visually-hidden` kind text in each name cell, directory names rendered with a trailing slash, `../` first row (folder icon, em dashes for Size/Mode) when `.Parent` is set, Type column removed; verify by running the web tests and inspecting rendered HTML in the test output
- [x] 2.2 Keep the empty-state behavior per design D4: render the table with only the `../` row for a non-root empty tree; keep "This directory is empty." only for the empty root; cover both in handler tests
- [x] 2.3 Update `web.css`: `.visually-hidden` utility, tree icon column sizing, remove `.parent-link`, extend the mobile grid so the icon cell takes its own narrow column and `data-label` cells keep their existing behavior; verified via headless Chromium at 1280px and 400px viewports (desktop table and stacked mobile layout both render correctly)

## 3. Blob page parent-link removal

- [x] 3.1 Remove the `Parent` paragraph from `blob.html` and confirm nothing else references `.parent-link` (grep); blob page tests and `go test ./internal/web/` pass

## 4. Tests for the new listing behavior

- [x] 4.1 Extend `browse_test.go`: trailing slash on directory links (href unchanged), `../` row present on nested pages and absent at root, absent Type column, icon sprite present with `use` references, empty non-root tree renders the table with the `../` row, kind still present as visually-hidden text
- [x] 4.2 Run `just ci` and confirm the whole suite is green; run `just spec-validate`

## Workflow follow-up

- Archive the change after the project's review requirements are satisfied.
- Sync the delta spec into `openspec/specs/code-browsing/spec.md` at archive time.
