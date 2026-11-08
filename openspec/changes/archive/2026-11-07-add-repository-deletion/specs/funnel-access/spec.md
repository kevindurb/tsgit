# Spec Delta

## MODIFIED Requirements

### Requirement: Public requests use a read-only principal

A Funnel request MUST resolve to a synthetic public principal whose grants are
read-only and derived only from the node capability. The system MUST NOT call
`WhoIs` for a Funnel request and MUST NOT derive the principal from the remote
address, an HTTP header, a query parameter, or a request body. The public
principal MUST NOT have write, create, or delete access under any payload.

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

#### Scenario: Public deletion is denied

- **WHEN** a Funnel request targets the repository deletion surface
- **THEN** the request is refused as not-found and no repository is changed
