# Spec Delta

## Purpose

Decide, for a given tailnet identity and repository name, whether to allow a
read, allow a write, or refuse — before any git process starts. The decision
uses the app capabilities the tailnet granted that identity for this service;
`tsgit` keeps no permission store of its own.

## ADDED Requirements

### Requirement: Authorization precedes execution

The system MUST complete an authorization decision for the requested
repository and operation before spawning any git subprocess.

#### Scenario: Allowed clone

- **WHEN** an authorized identity requests a fetch of a repository it may read
- **THEN** the system starts `git upload-pack` for that repository

#### Scenario: Denied push never starts git

- **WHEN** an identity without write access requests a push
- **THEN** the system refuses the session, and `git receive-pack` is never
  started for that repository

### Requirement: Authorization is a function of identity and repository

The authorization function MUST take an identity and a repository as values,
and MUST NOT require a network connection or request object to reach a
decision.

#### Scenario: Decision is unit-testable without a tailnet

- **WHEN** the authorization function is called with a constructed identity and
  a repository name in a test
- **THEN** it returns a decision without opening a socket or reading a live
  peer

### Requirement: Authorization derives from tailnet app capabilities

The system MUST derive repository access from the app capabilities resolved
from tailscaled for the connection, and MUST NOT read a local policy file or
consult any other authorization store.

#### Scenario: Write capability allows push

- **WHEN** the resolved identity carries a capability granting `write` on the
  requested repository
- **THEN** read and write operations are allowed

#### Scenario: Read capability allows clone but not push

- **WHEN** the resolved identity carries a capability granting `read` on the
  requested repository and no capability granting `write`
- **THEN** the fetch is allowed and the push is refused

#### Scenario: Capability is scoped to its repository patterns

- **WHEN** the resolved identity carries a capability for one repository or
  repository glob
- **THEN** a request for a repository that no pattern matches is refused

#### Scenario: Overlapping capabilities union

- **WHEN** two capability objects match the requested repository at different
  levels
- **THEN** the highest level wins, and `write` includes `read`

#### Scenario: Policy change takes effect on the next connection

- **WHEN** the tailnet policy is edited to remove a capability
- **THEN** a new connection is refused without any change to `tsgit`
  configuration or on-disk state

### Requirement: Deny by default

The system MUST refuse access when no capability grants it, and MUST treat a
capability payload it cannot interpret as granting nothing.

#### Scenario: No matching capability

- **WHEN** an identity has no capability granting access to the requested
  repository
- **THEN** the request is refused

#### Scenario: Malformed capability payload

- **WHEN** a capability object has an unknown `access` value, a non-array
  payload, or otherwise cannot be decoded
- **THEN** the request is refused, the error is logged, and no partial grant is
  applied

### Requirement: Tag-based access

The system MUST allow a capability granted to an ACL tag to authorize the
connection, so a tagged machine can be granted access without any user being
involved.

#### Scenario: Tagged machine reads

- **WHEN** a connection comes from a node carrying tag `tag:ci` and the tailnet
  policy grants that tag a read capability for the repository
- **THEN** the read is allowed even though no user login is associated with the
  connection
