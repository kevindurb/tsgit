# Spec Delta

## Purpose

Decide, for a given tailnet identity and repository name, whether to allow a
read, allow a write, or refuse — before any git process starts.

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

### Requirement: Policy is evaluated from identity and repository

The authorization function MUST take an identity and a repository as values,
and MUST NOT require a network connection or request object to reach a
decision.

#### Scenario: Decision is unit-testable without a tailnet

- **WHEN** the authorization function is called with a constructed identity and
  a repository name in a test
- **THEN** it returns a decision without opening a socket or reading a live
  peer

### Requirement: Deny by default

The system MUST refuse access when no policy rule matches, and MUST refuse when
the policy file cannot be read.

#### Scenario: No matching rule

- **WHEN** an identity requests a repository for which no rule grants access
- **THEN** the request is refused

#### Scenario: Unreadable policy

- **WHEN** the policy file is missing or malformed at request time
- **THEN** the request is refused and the error is logged

### Requirement: Tag-based access

The system MUST allow a rule to grant access on the basis of an ACL tag, so a
tagged machine can be granted access without any user being involved.

#### Scenario: Tagged machine reads

- **WHEN** a connection comes from a node carrying tag `tag:ci` and the policy
  grants read to `tag:ci`
- **THEN** the read is allowed even though no user login is associated with the
  connection
