# Spec Delta

## Purpose

Resolve every inbound connection to the tailnet identity behind it, so that
authorization is a decision about a known peer rather than about input the
client controls.

## ADDED Requirements

### Requirement: Connection identity resolution

The system MUST resolve each accepted SSH connection to a tailnet identity
consisting of the peer node name, the authenticated tailnet user login, and the
node's ACL tags.

#### Scenario: Known peer is resolved

- **WHEN** a connection arrives from an address that tailscaled reports as a
  known peer
- **THEN** the system resolves that peer to its node name, user login, and tags
  before any git command is executed

#### Scenario: Unknown peer is refused

- **WHEN** a connection arrives from an address tailscaled does not report as a
  known peer
- **THEN** the system refuses the session without executing any git command

### Requirement: Client-supplied identity is never trusted

The system MUST NOT derive identity from the SSH user name, an environment
variable, a header, or any other value the client supplies.

#### Scenario: Claimed user name is ignored for authorization

- **WHEN** a client connects as SSH user `root` or any other name while
  tailscaled reports a different tailnet user
- **THEN** authorization uses the tailnet-reported user, and the claimed SSH
  user has no effect on the decision

### Requirement: Identity resolution failure is explicit

The system MUST fail the session with a logged error when tailscaled cannot be
queried, and MUST NOT fall back to a permissive default.

#### Scenario: LocalAPI unavailable

- **WHEN** the tailscaled LocalAPI socket is missing or returns an error
- **THEN** the session is refused and the failure is logged, with no fallback
  identity assumed
