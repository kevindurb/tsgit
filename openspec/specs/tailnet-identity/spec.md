# tailnet-identity Specification

## Purpose
Resolve every inbound HTTP request to the tailnet identity and app capabilities behind it, so authorization is a decision about a known peer rather than about input the client controls.

## Requirements

### Requirement: Request identity resolution

The system MUST resolve every request that is not a Funnel request to a tailnet
identity consisting of the peer node, an optional user login, the node's ACL
tags, and the app capabilities granted for this service, before making any
authorization decision or starting any backend work. A request that arrives
over Funnel MUST NOT be resolved this way: it MUST use the public read-only
principal defined by `funnel-access`, and the system MUST NOT call `WhoIs` for
it.

#### Scenario: Known peer with capabilities

- **WHEN** a request arrives from an address the embedded node reports as a
  known peer with a repository capability
- **THEN** the system resolves the node, login when present, tags, and
  capabilities before authorization

#### Scenario: Unknown peer

- **WHEN** a non-Funnel request arrives from an address that does not resolve to
  a known tailnet peer
- **THEN** the system refuses the request without starting a backend or
  creating a repository

#### Scenario: Funnel request uses the public principal

- **WHEN** a request arrives over Funnel
- **THEN** it resolves to the public read-only principal and no tailnet identity
  lookup is attempted

### Requirement: Client-supplied identity is never trusted

The system MUST NOT derive identity or capabilities from an HTTP header, query
parameter, request body, or any other value the client controls. This includes
values that claim a Funnel origin: a header, query parameter, or body value MUST
NOT select the public path or change which principal is used.

#### Scenario: Spoofed headers are ignored

- **WHEN** a client sends `X-Forwarded-For`, `Tailscale-User-Login`, or a
  claimed user name while the peer address maps to a different identity
- **THEN** authorization uses the identity resolved from the peer address and
  the claimed values have no effect

#### Scenario: Funnel origin cannot be claimed

- **WHEN** a tailnet client sends `Tailscale-Funnel-Request` or any other value
  claiming to be public
- **THEN** it is still resolved as the tailnet peer and its capabilities still
  apply

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
response in a tailnet identity, and MUST treat a missing capability as an empty
set rather than an error. The public principal's read grants come from the
node's `cap/funnel` capability defined by `funnel-access`; they MUST NOT be
taken from a `WhoIs` response.

#### Scenario: Peer without a repository capability

- **WHEN** a known peer has no repository capability for this service
- **THEN** the identity resolves successfully with an empty capability set and
  later authorization denies access

#### Scenario: Public grants come from the node capability

- **WHEN** a Funnel request is authorized
- **THEN** its read grants come from the node's own `cap/funnel` payload, not
  from a tailnet identity response

### Requirement: Identity resolution failure is explicit

The system MUST refuse the request with a logged error when the local identity
lookup fails, and MUST NOT fall back to a permissive default or an assumed
identity.

#### Scenario: LocalAPI unavailable

- **WHEN** the embedded node cannot answer the identity lookup
- **THEN** the request is refused, the failure is logged, and no repository work
  or backend starts
