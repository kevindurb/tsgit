# Spec Delta

## MODIFIED Requirements

### Requirement: Public surface is clone, fetch, and filtered discovery

The Funnel listener MUST serve `git-upload-pack` reference discovery and pack
exchanges for public repositories, the discovery pages filtered to those
repositories, and the read-only browsing pages for those repositories, including
directory listings, file contents, ref selection, and README rendering. It MUST
refuse everything else, including receive-pack and deletion, with the same
not-found response used for an unreadable or missing repository, before starting
a backend or creating a repository. The public view MUST NOT indicate that the
viewer may push, MUST NOT show first-push instructions for an empty repository,
and MUST NOT show a deletion action.

#### Scenario: Public clone succeeds

- **WHEN** a public repository exists and a Funnel client clones it
- **THEN** the client receives the repository contents over upload-pack

#### Scenario: Public receive-pack is refused

- **WHEN** a Funnel client requests receive-pack for a public repository
- **THEN** the request is refused as not found and no backend starts

#### Scenario: Public index is filtered

- **WHEN** a Funnel client opens the discovery index
- **THEN** only repositories the public principal may read are listed

#### Scenario: Public empty repository

- **WHEN** a Funnel client opens a public repository with no commits
- **THEN** the summary identifies it as empty but shows no push instructions

#### Scenario: Public browsing succeeds

- **WHEN** a Funnel client opens a tree, blob, or commit view for a public
  repository
- **THEN** the page is rendered for the public principal without starting a write
  backend

#### Scenario: Public browsing of a non-public repository is not found

- **WHEN** a Funnel client requests a browsing page for a repository the public
  principal may not read
- **THEN** the response is the same not-found response used for an unreadable or
  missing repository

#### Scenario: Public browsing hides write affordances

- **WHEN** a Funnel client opens a browsing page
- **THEN** the page shows no push indication, first-push commands, or deletion
  action

#### Scenario: Public README is rendered safely

- **WHEN** a Funnel client opens a public repository whose selected ref contains
  a README
- **THEN** the README is rendered in safe mode with the same URL and link rules
  as the tailnet view

#### Scenario: Public commit ID browsing succeeds

- **WHEN** a Funnel client browses a full commit ID that exists in a public
  repository
- **THEN** the page is rendered for the public principal

### Requirement: Public listener limits and observability

The system MUST bound public request bodies and request lifetimes, MUST cap the
number of concurrent public pack exchanges and git-backed browsing requests, and
MUST log the public client's source address. When a concurrency limit is reached,
the system MUST refuse the request with a retryable error rather than queueing it
without bound. Public browsing reads MUST be bounded by per-request timeouts and
response-size caps.

#### Scenario: Oversized public request body

- **WHEN** a Funnel client sends a request body larger than the configured
  public limit
- **THEN** the request is refused before a backend starts

#### Scenario: Public pack concurrency limit

- **WHEN** the configured number of public pack exchanges is already running
- **THEN** a further public pack exchange is refused with a retryable server
  error rather than queued indefinitely

#### Scenario: Public browsing concurrency limit

- **WHEN** the configured number of public browsing reads is already running
- **THEN** a further public browsing request is refused with a retryable server
  error rather than queued indefinitely

#### Scenario: Public browsing read is bounded

- **WHEN** a public browsing request reads a tree, file, or README that exceeds
  its size cap or time limit
- **THEN** the response reports the limit instead of streaming unbounded content

#### Scenario: Public access is logged

- **WHEN** a public request is served or refused
- **THEN** the log identifies the public principal and the public client's
  source address, not the Funnel relay's address
