# Spec Delta

## Purpose

Let a viewer read a repository's directory tree, file contents, refs, and README through a read-only web surface without cloning it.

## ADDED Requirements

### Requirement: Browsing uses the resolved identity and authorizes before git

The system MUST resolve the viewer of every browsing request from the same
tailnet identity source used for git and discovery requests before reading
repository content or starting a git process. A Funnel request MUST instead use
the public read-only principal defined by `funnel-access` and MUST NOT call
`WhoIs`. The system MUST NOT derive identity from a header, query parameter,
request body, or any other client-supplied value. The system MUST decide read
access for the repository before resolving a path or starting any git process.
When identity resolution fails, the system MUST refuse the request with a logged
error and MUST NOT fall back to a permissive default. A repository the viewer
may not read and a repository that does not exist MUST produce the same
not-found response.

#### Scenario: Known peer browses a readable repository

- **WHEN** a known tailnet peer requests a browsing page for a repository it may
  read
- **THEN** the system resolves the peer's identity and read access before reading
  any repository content

#### Scenario: Denied and missing are indistinguishable

- **WHEN** the viewer requests a browsing page for a repository it may not read
- **THEN** the response is identical to the response for a repository that does
  not exist

#### Scenario: Identity lookup failure is explicit

- **WHEN** the embedded node cannot answer the identity lookup for a browsing
  request
- **THEN** the request is refused with a server error, the failure is logged, and
  no repository content is rendered

#### Scenario: Public view uses the public principal

- **WHEN** a Funnel request opens a browsing page for a public repository
- **THEN** the page is rendered for the public read-only principal with no
  tailnet identity lookup

#### Scenario: Spoofed identity values are ignored

- **WHEN** a client sends a header or query value claiming to be a different user
- **THEN** the rendered page uses the identity resolved from the peer address and
  the claimed value has no effect

### Requirement: Browsing does not shadow smart HTTP

The browsing surface MUST claim only requests that the smart-HTTP handler does
not recognize. Existing clone, fetch, and push URLs, including a clone URL with
no `.git` suffix, MUST continue to reach the git handler unchanged. A malformed
or unsupported smart-HTTP request MUST NOT be served as a browsing page. A
`.git`-suffixed browsing URL MUST redirect to the canonical repository path while
preserving the browsing tail and query.

#### Scenario: Clone URLs are unchanged

- **WHEN** a client runs `git clone https://host/acme/widget.git` or
  `git clone https://host/acme/widget`
- **THEN** the request is served by the git handler as before

#### Scenario: Git-shaped request is not a browsing page

- **WHEN** a client sends an unsupported smart-HTTP method or service to a
  repository path
- **THEN** the response is a refusal, not a rendered tree, blob, or summary

#### Scenario: .git browsing URL redirects

- **WHEN** a client requests a browsing page under a repository path ending in
  `.git`
- **THEN** the response redirects to the same browsing page on the canonical
  repository path, preserving the ref query

### Requirement: Ref selection

Browsing requests MUST accept an optional `ref` query parameter and MUST treat
an absent value as `HEAD`. The system MUST accept a branch short name, a tag
short name, a full `refs/heads/...` or `refs/tags/...` name, or a full 40- or
64-hex commit ID. A full-length hexadecimal value MUST be resolved as a commit
first; if no such commit exists, it MUST fall back to short-name resolution.
Resolution MUST peel annotated tags to commits and MUST reject objects that are
not commits. A short name MUST prefer an exact branch over an exact tag; a full
tag ref MUST address a tag whose short name collides with a branch. Hexadecimal
input MUST be compared case-insensitively and generated links MUST use
lowercase. Generated browsing links MUST carry the selected ref when it is not
the default branch and MUST omit it when it is. The repository summary MUST
remain `HEAD`-only; a `ref` query on the summary MUST redirect to the tree.

#### Scenario: Default branch is browsed

- **WHEN** a browsing request omits the ref
- **THEN** the system browses the commit named by `HEAD`

#### Scenario: Branch is selected

- **WHEN** the request selects an existing branch short name
- **THEN** the listing and files come from that branch's tip

#### Scenario: Tag is selected

- **WHEN** the request selects an existing tag short name
- **THEN** the tag is peeled to its commit and the listing and files come from
  that commit

#### Scenario: Colliding tag is selected by full ref

- **WHEN** a branch and a tag share a short name and the request names the full
  `refs/tags/...` value
- **THEN** the tag is used rather than the branch

#### Scenario: Commit ID is browsed

- **WHEN** the request selects a full 40- or 64-hex commit ID that exists in the
  repository
- **THEN** the listing and files come from that commit even if no branch or tag
  reaches it

#### Scenario: Unknown ref is not found

- **WHEN** the request selects a value that resolves to no commit
- **THEN** the response is the repository's not-found response

#### Scenario: Non-commit object is refused

- **WHEN** the request selects a full hash of a tree or blob object
- **THEN** the response is the repository's not-found response and no content is
  rendered from that object

#### Scenario: Branch named like a commit ID

- **WHEN** a branch short name is a full-length hexadecimal string and no commit
  with that ID exists
- **THEN** the branch is selected by falling back to short-name resolution

#### Scenario: Detached HEAD is browsed

- **WHEN** the repository's `HEAD` is detached and the request omits the ref
- **THEN** the listing and files come from the detached commit

#### Scenario: Ref is preserved in links

- **WHEN** a browsing page is rendered for a non-default ref
- **THEN** its breadcrumbs, parent link, entry links, and README links carry that
  ref

#### Scenario: Summary redirects a ref query

- **WHEN** a request opens the repository summary with a `ref` query
- **THEN** the response redirects to the tree page with the same ref

### Requirement: Directory listing

For a directory the viewer may read, the system MUST render the entries of the
selected tree path at the resolved commit. The listing MUST be deterministic:
directories first, then files and other entries, each ordered by name. Each
entry MUST present its name, its type, its size when available, and a link to
the appropriate tree or blob page. Entry names MUST be escaped for display and
URL-encoded in links. The page MUST provide breadcrumbs and a link to the parent
directory, and those links MUST preserve the selected ref. A tree route that
names a file MUST redirect to the blob route, and a blob route that names a
directory MUST redirect to the tree route; redirect targets MUST be built from
validated path components. An empty listing MUST render an empty state rather
than an error.

#### Scenario: Root listing

- **WHEN** the viewer opens the tree page for a readable repository
- **THEN** the page lists the entries at the root of the selected commit

#### Scenario: Nested directory

- **WHEN** the viewer opens a tree path that names a directory
- **THEN** the page lists that directory's entries and shows a parent link

#### Scenario: Entry types and sizes

- **WHEN** a directory contains files, executables, symlinks, submodules, and
  subdirectories
- **THEN** each entry shows its name, type, size when available, and a link to
  the correct page

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
- **THEN** the page renders an empty state and does not fail

### Requirement: Paths are repository-tree paths

The system MUST treat the path after `/tree/` or `/blob/` as a git tree path,
not a filesystem path. It MUST reject empty, `.`, `..`, and control-character
path components. It MUST NOT join a request path onto the repository directory.
Path components beginning with a dot MUST be allowed so that directories such as
`.github` are browsable. The system MUST bound path depth and length.

#### Scenario: Traversal is rejected

- **WHEN** a browsing URL contains a `..` component or an empty component
- **THEN** the request is refused and no repository file outside the tree is read

#### Scenario: Dot-directory is allowed

- **WHEN** a repository contains a directory named `.github`
- **THEN** the viewer can open it and list its entries

#### Scenario: Deep path is bounded

- **WHEN** a browsing URL names a path deeper or longer than the configured bound
- **THEN** the request is refused without starting a git process

### Requirement: File contents

For a file the viewer may read, the system MUST render its contents at the
resolved commit. Text MUST be rendered as escaped text. A binary file MUST be
identified as binary and MUST NOT have its bytes rendered. A file above the
configured size cap MUST be reported as too large and MUST NOT be rendered. An
empty file MUST render as empty. A symlink MUST show its target as text and MUST
NOT be followed. A submodule MUST show its commit ID and MUST NOT be rendered as
file content. The system MUST NOT expose a raw or download endpoint for
repository bytes.

#### Scenario: Text file is rendered

- **WHEN** the viewer opens a readable text file
- **THEN** the page shows its contents as escaped text

#### Scenario: Binary file is identified

- **WHEN** the viewer opens a binary file
- **THEN** the page identifies it as binary and does not render its bytes

#### Scenario: Oversized file is not rendered

- **WHEN** the viewer opens a file above the configured size cap
- **THEN** the page reports that the file is too large and renders no contents

#### Scenario: Empty file is rendered

- **WHEN** the viewer opens an empty file
- **THEN** the page renders an empty file view and does not fail

#### Scenario: Symlink is not followed

- **WHEN** the viewer opens an entry that is a symlink
- **THEN** the page shows the target as text and does not read the target

#### Scenario: Submodule is not file content

- **WHEN** the viewer opens an entry that is a submodule
- **THEN** the page shows its commit ID and renders no file contents

#### Scenario: Markup is escaped

- **WHEN** a file contains HTML or script markup
- **THEN** the page displays it as literal text and does not execute it

### Requirement: README rendering

Wherever a README is rendered, including the repository summary at `HEAD` and
the tree root at the selected ref, the system MUST render it as Markdown in safe
mode with raw HTML disabled. README detection MUST be case-insensitive and MUST
prefer `README.md`, then `README.markdown`, then an extensionless `README`. A
README that is a symlink, submodule, binary, or above the configured size cap
MUST NOT be rendered. The renderer MUST allow only `http`, `https`, and `mailto`
destinations and in-repository relative links. Relative links MUST resolve
against the README's directory, MUST remain inside the repository, and MUST
carry the current ref. Absolute-path links, unsupported schemes, and links that
escape the repository MUST be rendered as text. Images MUST degrade to their alt
text. Generated markup MUST escape repository-controlled text. The repository
index MUST NOT render README content.

#### Scenario: README is rendered on the summary

- **WHEN** the viewer opens the summary of a repository whose default branch
  contains a README
- **THEN** the summary shows the rendered README

#### Scenario: README is rendered at a selected ref

- **WHEN** the viewer opens the tree root for a tag or commit that contains a
  README
- **THEN** the tree page shows that commit's rendered README

#### Scenario: Raw HTML is inert

- **WHEN** a README contains raw HTML such as a script or an event-handler
  attribute
- **THEN** that markup does not reach the page as active HTML

#### Scenario: Script link is text

- **WHEN** a README contains a Markdown link or image whose destination uses
  `javascript:`, `data:`, or another unsupported scheme
- **THEN** the destination is rendered as text and no active link is produced

#### Scenario: Relative link carries the ref

- **WHEN** a README at a non-default ref links to another file in the repository
- **THEN** the generated link points to the matching browsing route and carries
  the same ref

#### Scenario: Links that escape the repository are text

- **WHEN** a README link resolves to a path outside the repository
- **THEN** the destination is rendered as text

#### Scenario: Absolute-path links are text

- **WHEN** a README link starts with `/`
- **THEN** the destination is rendered as text rather than linked to a server
  route

#### Scenario: External image degrades to alt text

- **WHEN** a README contains a Markdown image
- **THEN** the rendered page shows its alt text and loads no external image

#### Scenario: Oversized README is not rendered

- **WHEN** a README exceeds the configured size cap
- **THEN** the page identifies it as too large and does not render it

#### Scenario: Symlink README is not rendered

- **WHEN** the preferred README entry is a symlink, submodule, or binary blob
- **THEN** it is not rendered as Markdown

#### Scenario: No README

- **WHEN** the selected tree root contains no recognized README
- **THEN** the page renders without a README section and does not fail

#### Scenario: Index does not render README

- **WHEN** the viewer opens the repository index
- **THEN** no README content is fetched or rendered for any repository

### Requirement: Browsing is read-only

The browsing surface MUST NOT create, modify, rename, or delete a repository,
ref, or object. It MUST refuse mutating methods. Viewing a missing path, ref, or
commit MUST create nothing. Repository creation and updates remain the exclusive
result of an authorized git push.

#### Scenario: Mutation is refused

- **WHEN** a client sends a mutating method to a browsing path
- **THEN** the request is refused and no repository, ref, or object is changed

#### Scenario: Viewing does not create

- **WHEN** the viewer requests a missing path, ref, or commit
- **THEN** no repository, ref, or object is created

### Requirement: Browsing pages are self-contained and theme-aware

Every browsing page MUST be legible in the viewer's light or dark color-scheme
preference, selected without JavaScript. Pages MUST load no external scripts,
stylesheets, fonts, or images, MUST render repository-controlled text as text
except where README Markdown rendering explicitly permits generated markup, and
MUST be served with a restrictive content security policy. README rendering MUST
NOT widen the policy to allow external images or scripts.

#### Scenario: Both color schemes render

- **WHEN** the viewer's browser prefers a light or a dark color scheme
- **THEN** the page renders with the corresponding color tokens without a script
  or a second request

#### Scenario: No external assets

- **WHEN** a browsing page is loaded
- **THEN** every stylesheet, image, and script comes from the tsgit origin, and
  no request leaves the tailnet

#### Scenario: README does not widen the policy

- **WHEN** a README contains an external image or script
- **THEN** the content security policy still forbids it
