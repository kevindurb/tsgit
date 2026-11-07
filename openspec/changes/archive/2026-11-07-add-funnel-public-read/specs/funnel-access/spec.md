# Spec Delta

## Purpose

Publish a policy-selected, read-only subset of repositories to the public
internet over Tailscale Funnel, without adding accounts and without ever
mistaking a public client for a tailnet peer.

## ADDED Requirements

### Requirement: Funnel is opt-in and separate from the tailnet listener

The system MUST open a Funnel listener only when the deployment enables Funnel,
the node carries the tailnet `funnel` attribute, and HTTPS certificate domains
are available. The Funnel listener MUST serve public traffic only and MUST NOT
be reachable by tailnet peers. When Funnel is requested but unavailable, the
system MUST keep serving the tailnet, MUST NOT open the public listener, and
MUST log a prominent error.

#### Scenario: Funnel is not requested

- **WHEN** the deployment does not enable Funnel
- **THEN** no public listener is opened and the tailnet listeners behave as
  before

#### Scenario: Funnel is requested but unavailable

- **WHEN** the deployment enables Funnel but the node lacks the `funnel`
  attribute, HTTPS, or a certificate domain
- **THEN** the tailnet keeps serving, no public listener is opened, and an error
  naming the missing prerequisite is logged

#### Scenario: Public listener does not serve tailnet peers

- **WHEN** a tailnet peer reaches the node
- **THEN** it is served by the tailnet handler and its tailnet identity, not by
  the public principal

### Requirement: Public allowlist comes from the node capability

The system MUST read `github.com/kevindurb/tsgit/cap/funnel` from the node's own
`nodeAttrs.app` payload on each public request. The payload MUST be an array of
objects containing repository patterns in `repos`, matched against the canonical
`<namespace>/<repo>` name. The payload MUST NOT carry an access level: public
access is always read-only. A capability placed in `grants[].app` MUST NOT make
a repository public. A missing capability means nothing is public. A malformed
capability grants nothing and is logged.

#### Scenario: Node capability exposes repositories

- **WHEN** the node carries a `cap/funnel` payload with `repos` patterns
- **THEN** a public request may read exactly the repositories those patterns
  match

#### Scenario: Grant capability does not expose repositories

- **WHEN** a `cap/funnel` payload appears in a grant instead of `nodeAttrs.app`
- **THEN** no repository becomes public

#### Scenario: Missing capability

- **WHEN** the node has no `cap/funnel` capability
- **THEN** no repository is public and public requests are refused

#### Scenario: Malformed capability

- **WHEN** a `cap/funnel` payload cannot be decoded or has the wrong shape
- **THEN** no repository is public, the error is logged, and the tailnet keeps
  serving

#### Scenario: Policy change takes effect on the next request

- **WHEN** the tailnet policy adds or removes a public pattern
- **THEN** the next public request reflects the change without a restart or any
  on-disk change

### Requirement: Public requests use a read-only principal

A Funnel request MUST resolve to a synthetic public principal whose grants are
read-only and derived only from the node capability. The system MUST NOT call
`WhoIs` for a Funnel request and MUST NOT derive the principal from the remote
address, an HTTP header, a query parameter, or a request body. The public
principal MUST NOT have write or create access under any payload.

#### Scenario: Public clone uses the public principal

- **WHEN** a Funnel request asks to read a public repository
- **THEN** authorization uses the public principal and the clone proceeds

#### Scenario: Funnel address is not identity

- **WHEN** a Funnel request arrives with an address that would not resolve to a
  tailnet peer
- **THEN** the system does not attempt a tailnet identity lookup and still uses
  only the public principal

#### Scenario: Public write is denied

- **WHEN** a Funnel request asks for receive-pack or any write operation
- **THEN** the request is refused before any backend starts

#### Scenario: Public creation is denied

- **WHEN** a Funnel request names a repository that does not exist
- **THEN** no repository is created and the response is the same not-found
  result as for an unreadable repository

### Requirement: Public surface is clone, fetch, and filtered discovery

The Funnel listener MUST serve `git-upload-pack` reference discovery and pack
exchanges for public repositories, and the discovery pages filtered to those
repositories. It MUST refuse everything else, including receive-pack, with the
same not-found response used for an unreadable or missing repository, before
starting a backend or creating a repository. The public view MUST NOT indicate
that the viewer may push, and MUST NOT show first-push instructions for an empty
repository.

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

### Requirement: Tailnet behavior is unchanged

Enabling Funnel MUST NOT change tailnet identity resolution, authorization,
push, discovery, or repository creation. A tailnet peer retains its own
capabilities whether or not a repository is public.

#### Scenario: Tailnet writer pushes a non-public repository

- **WHEN** Funnel is enabled and a tailnet identity with write access pushes to
  a repository that is not public
- **THEN** the push succeeds exactly as it would without Funnel

#### Scenario: Tailnet and public views differ

- **WHEN** the same node serves a tailnet peer and a public client
- **THEN** the tailnet peer sees its full authorized view and the public client
  sees only the public subset

### Requirement: Public listener limits and observability

The system MUST bound public request bodies and request lifetimes, MUST cap the
number of concurrent public pack exchanges, and MUST log the public client's
source address. When the concurrency limit is reached, the system MUST refuse
the request with a retryable error rather than queueing it without bound.

#### Scenario: Oversized public request body

- **WHEN** a Funnel client sends a request body larger than the configured
  public limit
- **THEN** the request is refused before a backend starts

#### Scenario: Public pack concurrency limit

- **WHEN** the configured number of public pack exchanges is already running
- **THEN** a further public pack exchange is refused with a retryable server
  error rather than queued indefinitely

#### Scenario: Public access is logged

- **WHEN** a public request is served or refused
- **THEN** the log identifies the public principal and the public client's
  source address, not the Funnel relay's address

### Requirement: Public failures never take down the tailnet

A Funnel listener failure, a missing or malformed public capability, or a failed
public policy lookup MUST NOT stop the tailnet listeners. A failed public policy
lookup MUST refuse that public request with a logged server error.

#### Scenario: Public policy lookup fails

- **WHEN** the node cannot report its own capabilities while serving a Funnel
  request
- **THEN** that public request is refused with a server error, the failure is
  logged, and the tailnet keeps serving

#### Scenario: Funnel listener cannot bind

- **WHEN** the Funnel listener cannot be created
- **THEN** the tailnet keeps serving, no public listener is opened, and the
  failure is logged
