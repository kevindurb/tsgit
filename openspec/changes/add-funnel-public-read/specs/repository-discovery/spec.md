# Spec Delta

## MODIFIED Requirements

### Requirement: Discovery uses the resolved tailnet identity

The system MUST resolve the viewer of every discovery page from the same
tailnet identity source used for git requests, before deciding what to show. A
Funnel request MUST instead use the public read-only principal defined by
`funnel-access` and MUST NOT call `WhoIs`. The system MUST NOT derive identity
from a header, query parameter, request body, or any other client-supplied
value. If the identity lookup fails, the system MUST refuse the request with a
logged error and MUST NOT fall back to a permissive default or an assumed
identity.

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

#### Scenario: Public view uses the public principal

- **WHEN** a Funnel request opens a discovery page
- **THEN** the page is rendered for the public read-only principal, with no
  tailnet identity lookup

### Requirement: Index lists only readable repositories

The system MUST list exactly the repositories under the configured root for
which the resolved identity may read, and MUST omit every repository the
identity may not read. For a Funnel request, the list MUST be restricted to the
repositories the public principal may read, and it MUST NOT indicate that the
viewer may push. The list MUST be deterministic and MUST present each
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

#### Scenario: Public index hides non-public repositories

- **WHEN** the public principal may read only `public/*`
- **THEN** the index lists only matching repositories and names no other
  repository

#### Scenario: Public index has no push affordance

- **WHEN** the index is rendered for the public principal
- **THEN** every row is read-only and no writable indicator or push instruction
  appears

### Requirement: Repository summary

For a repository the viewer may read, the system MUST render a summary
containing the description, default branch, latest commit, and clone URL. When
the repository is empty, the summary MUST show how to push a first commit,
except for a Funnel request, which MUST NOT show first-push commands. The
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

#### Scenario: Public empty repository hides push instructions

- **WHEN** a Funnel request opens an empty public repository
- **THEN** the summary identifies it as empty but shows no first-push commands
