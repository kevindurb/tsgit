# Spec Delta

## MODIFIED Requirements

### Requirement: Directory listing

For a directory the viewer may read, the system MUST render the entries of the
selected tree path at the resolved commit. The listing MUST be deterministic:
directories first, then files and other entries, each ordered by name. Each
entry MUST present an icon identifying its type, its name, its size when
available, and a link to the appropriate tree or blob page. Entry names MUST be
escaped for display and URL-encoded in links. Directory entry names MUST be
displayed with a trailing slash, display-only; the link target MUST NOT gain
the slash. The icon MUST be one of: directory, executable, symlink,
submodule, a recognized file type (document, code, image, archive, config,
pdf, markup/stylesheet, lockfile, dotfile), or a generic file. An entry's
kind MUST remain available to assistive technology; icons alone MUST NOT be
the only accessible type signal. The page MUST provide breadcrumbs. When the
selected path is not the repository root, the listing's first row MUST link to
the parent directory ("../") and carry the selected ref; it MUST NOT be
rendered as a separate paragraph outside the listing. A tree route that names
a file MUST redirect to the blob route, and a blob route that names a
directory MUST redirect to the tree route; redirect targets MUST be built from
validated path components. An empty listing MUST render the listing container
with the parent-directory row when one exists, rather than an error or a
missing way up.

#### Scenario: Root listing
- **WHEN** the viewer opens the tree page for a readable repository
- **THEN** the page lists the entries at the root of the selected commit

#### Scenario: Nested directory
- **WHEN** the viewer opens a tree path that names a directory
- **THEN** the page lists that directory's entries with a leading row linking
  to the parent directory, and that row carries the selected ref

#### Scenario: Root has no parent row
- **WHEN** the viewer opens the tree page for the repository root
- **THEN** the listing has no parent-directory row

#### Scenario: Entry types and sizes
- **WHEN** a directory contains files, executables, symlinks, submodules, and
  subdirectories
- **THEN** each entry shows an icon identifying its type, its name, its size
  when available, and a link to the correct page

#### Scenario: Directory names display with a trailing slash
- **WHEN** the listing renders a directory entry
- **THEN** the visible name ends with "/" and the link target does not

#### Scenario: Kind is available to assistive technology
- **WHEN** a screen reader reads an entry in the listing
- **THEN** the entry's kind (directory, file, executable, symlink, submodule)
  is available as text and is not conveyed by the icon alone

#### Scenario: Recognized file types get a distinct icon
- **WHEN** the listing renders files whose names match a recognized type group
  (document, code, image, archive, config, pdf, markup/stylesheet, lockfile,
  dotfile)
- **THEN** each shows the icon for its type group rather than the generic
  file icon

#### Scenario: Names with spaces and non-ASCII characters
- **WHEN** an entry name contains spaces, Unicode, or characters that require
  escaping
- **THEN** the page displays the name as text and the link addresses it without
  collision

#### Scenario: Tree route on a file redirects
- **WHEN** the request uses the tree route for a path that names a file
- **THEN** the response redirects to the blob route for that file

#### Scenario: Blob route on a directory redirects
- **WHEN** the request uses the blob route for a path that names a directory
- **THEN** the response redirects to the tree route for that directory

#### Scenario: Empty listing
- **WHEN** the selected path resolves to an empty tree
- **THEN** the page renders the listing with the parent-directory row when one
  exists and does not fail
