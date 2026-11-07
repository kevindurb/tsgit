# tailnet-identity Specification

## Purpose
Resolve every inbound HTTP request to the tailnet identity and app capabilities behind it, so authorization is a decision about a known peer rather than about input the client controls.

## Requirements

### Requirement: Request identity resolution

The system MUST resolve each accepted request to a tailnet identity consisting
of the peer node, an optional user login, the node's ACL tags, and the app
capabilities granted for this service, before making any authorization decision
or starting any backend work.

#### Scenario: Known peer with capabilities

- **WHEN** a request arrives from an address the embedded node reports as a
  known peer with a repository capability
- **THEN** the system resolves the node, login when present, tags, and
  capabilities before authorization

#### Scenario: Unknown peer

- **WHEN** a request arrives from an address that does not resolve to a known
  tailnet peer
- **THEN** the system refuses the request without starting a backend or
  creating a repository

### Requirement: Client-supplied identity is never trusted

The system MUST NOT derive identity or capabilities from an HTTP header, query
parameter, request body, or any other value the client controls.

#### Scenario: Spoofed headers are ignored

- **WHEN** a client sends `X-Forwarded-For`, `Tailscale-User-Login`, or a
  claimed user name while the peer address maps to a different identity
- **THEN** authorization uses the identity resolved from the peer address and
  the claimed values have no effect

### Requirement: Tagged nodes are identified without a user login

The system MUST resolve a tagged node as a valid identity even when no user
login exists, and MUST make that node's granted capabilities available to
authorization.

#### Scenario: Tagged CI node

- **WHEN** a request comes from a node carrying `tag:ci` and the tailnet grants
  that tag a repository capability
- **THEN** the node resolves with no login and the capability is available for
  the decision

### Requirement: Capabilities are part of the resolved identity

The system MUST include the repository capability values from the same identity
response in the resolved identity, and MUST treat a missing capability as an
empty set rather than an error.

#### Scenario: Peer without a repository capability

- **WHEN** a known peer has no repository capability for this service
- **THEN** the identity resolves successfully with an empty capability set and
  later authorization denies access

### Requirement: Identity resolution failure is explicit

The system MUST refuse the request with a logged error when the local identity
lookup fails, and MUST NOT fall back to a permissive default or an assumed
identity.

#### Scenario: LocalAPI unavailable

- **WHEN** the embedded node cannot answer the identity lookup
- **THEN** the request is refused, the failure is logged, and no repository work
  or backend starts
