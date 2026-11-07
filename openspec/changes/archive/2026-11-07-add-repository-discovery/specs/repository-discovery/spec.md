# Spec Delta

## Purpose

Give a tailnet user a read-only way to discover which repositories they can
reach and how to clone them, without adding accounts, sessions, or code
browsing.

## ADDED Requirements

### Requirement: Discovery uses the resolved tailnet identity

The system MUST resolve the viewer of every discovery page from the same
tailnet identity source used for git requests, before deciding what to show.
It MUST NOT derive identity from a header, query parameter, request body, or
any other client-supplied value. If the identity lookup fails, the system MUST
refuse the request with a logged error and MUST NOT fall back to a permissive
default or an assumed identity.

#### Scenario: Known peer sees its own view

- **WHEN** a request arrives from a known tailnet peer with a repository
  capability
- **THEN** the system resolves that node, optional login, tags, and capability
  before rendering any repository data

#### Scenario: Identity lookup failure is explicit

- **WHEN** the embedded node cannot answer the identity lookup for a discovery
  request
- **THEN** the request is refused with a server error, the failure is logged,
  and no repository data is rendered

#### Scenario: Spoofed identity values are ignored

- **WHEN** a client sends a header or query value claiming to be a different
  user
- **THEN** the rendered index and summary use the identity resolved from the
  peer address and the claimed value has no effect

### Requirement: Index lists only readable repositories

The system MUST list exactly the repositories under the configured root for
which the resolved identity may read, and MUST omit every repository the
identity may not read. The list MUST be deterministic and MUST present each
repository's canonical `<namespace>/<repo>` name, a clone URL that works with a
stock git client, and whether the viewer may also push. An identity with no
readable repositories MUST see an empty state rather than an error.

#### Scenario: Mixed access

- **WHEN** the root contains `acme/widget` and `other/secret`, and the viewer
  may read only `acme/widget`
- **THEN** the index lists `acme/widget` and does not mention `other/secret`

#### Scenario: Empty index

- **WHEN** the viewer may read no repositories
- **THEN** the index renders an empty state and does not fail

#### Scenario: Clone URL

- **WHEN** the index lists a repository
- **THEN** it shows a clone URL whose host and scheme match the request and
  whose path addresses that repository

#### Scenario: Push access is visible

- **WHEN** the viewer may read and may also push to a listed repository
- **THEN** the index indicates that the repository is writable, and a
  read-only repository is marked as such

### Requirement: Repository summary

For a repository the viewer may read, the system MUST render a summary
containing the description, default branch, latest commit, and clone URL. When
the repository is empty, the summary MUST show how to push a first commit. The
summary MUST NOT render file trees, file contents, diffs, commit lists, or
README content.

#### Scenario: Populated repository

- **WHEN** the viewer opens a non-empty repository they may read
- **THEN** the summary shows its description, default branch, latest commit,
  and clone URL

#### Scenario: Empty repository

- **WHEN** the viewer opens a repository whose default branch has no commits
- **THEN** the summary identifies it as empty and shows the commands to push a
  first commit

#### Scenario: Default branch is unborn but other refs exist

- **WHEN** the repository's `HEAD` names a branch with no commits while another
  branch exists
- **THEN** the summary does not present the repository as never-pushed, and it
  names the existing refs

#### Scenario: Metadata is unavailable

- **WHEN** one repository's metadata cannot be read
- **THEN** the index still renders that repository and the other repositories,
  and the failure is logged

### Requirement: Denied and missing are indistinguishable

A repository the viewer may not read and a repository that does not exist MUST
produce the same not-found response, so the summary page does not confirm
whether an unreadable repository exists.

#### Scenario: No existence oracle

- **WHEN** the viewer requests a repository they may not read
- **THEN** the response is identical to the response for a repository that does
  not exist

#### Scenario: Missing repository

- **WHEN** the viewer requests a well-formed repository name that has never
  been created
- **THEN** the response is the same not-found response and no repository is
  created

### Requirement: Discovery is read-only

The discovery surface MUST NOT create, modify, rename, or delete a repository
or any part of one. It MUST refuse any mutating request. Repository creation
and updates remain the exclusive result of an authorized git push.

#### Scenario: Mutation is refused

- **WHEN** a client sends a mutating method to a discovery path
- **THEN** the request is refused and no repository or ref is changed

#### Scenario: Viewing does not create

- **WHEN** the viewer requests a repository page for a name that does not exist
- **THEN** no directory is created

### Requirement: Discovery does not shadow smart HTTP

The discovery surface MUST claim only requests that the smart-HTTP handler does
not recognize. Existing clone, fetch, and push URLs, including a clone URL with
no `.git` suffix, MUST continue to reach the git handler unchanged. A malformed
or unsupported smart-HTTP request MUST NOT be served as a repository summary.

#### Scenario: Clone URLs are unchanged

- **WHEN** a client runs `git clone https://host/acme/widget.git` or
  `git clone https://host/acme/widget`
- **THEN** the request is served by the git handler as before

#### Scenario: Git-shaped request is not a summary page

- **WHEN** a client sends an unsupported smart-HTTP method or service to a
  repository path
- **THEN** the response is a refusal, not a rendered repository summary

### Requirement: Self-contained, theme-aware rendering

Every discovery page MUST be legible in the viewer's light or dark color-scheme
preference, selected without JavaScript. Pages MUST load no external scripts,
stylesheets, fonts, or images, MUST render repository-controlled text as text
rather than markup, and MUST be served with a restrictive content security
policy.

#### Scenario: Both color schemes render

- **WHEN** the viewer's browser prefers a light or a dark color scheme
- **THEN** the page renders with the corresponding color tokens without
  requiring a script or a second request

#### Scenario: Repository text is escaped

- **WHEN** a repository description or commit subject contains HTML or script
  markup
- **THEN** the page displays it as literal text and does not execute it

#### Scenario: No external assets

- **WHEN** a discovery page is loaded
- **THEN** every stylesheet, image, and script comes from the tsgit origin, and
  no request leaves the tailnet
