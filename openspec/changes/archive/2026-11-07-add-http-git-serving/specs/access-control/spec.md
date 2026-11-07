# Spec Delta

## Purpose

Decide whether a tailnet identity may read, write, or create a canonical
repository from its Tailscale app capabilities, before any repository work or
git subprocess starts, while keeping no permission store of its own.

## ADDED Requirements

### Requirement: Authorization precedes execution and creation

The system MUST complete an authorization decision for the repository and
operation before starting any git backend process and before creating a
repository.

#### Scenario: Allowed clone

- **WHEN** an identity may read the requested repository
- **THEN** the system starts the backend for the fetch

#### Scenario: Denied push never starts the backend

- **WHEN** an identity without write access requests a push
- **THEN** the system refuses the request, starts no backend, and does not
  create the repository

### Requirement: Authorization is a pure decision

The authorization function MUST take an identity, a canonical repository name,
and an operation as values, and MUST NOT require a network connection or an
HTTP request to reach a decision.

#### Scenario: Decision is unit-testable without a tailnet

- **WHEN** authorization is called with a constructed identity and a repository
  name in a test
- **THEN** it returns a decision without opening a socket or reading a live peer

### Requirement: Authorization derives from tailnet app capabilities

The system MUST derive repository access from the app capabilities resolved for
the connection, and MUST NOT read a local policy file or consult any other
authorization store.

#### Scenario: Write allows read and write

- **WHEN** the resolved identity carries a capability granting `write` on the
  requested repository
- **THEN** fetch and push are both allowed

#### Scenario: Read allows fetch but not push

- **WHEN** the resolved identity carries a capability granting `read` on the
  requested repository and none granting `write`
- **THEN** the fetch is allowed and the push is refused

#### Scenario: Capability is scoped to its repository patterns

- **WHEN** the resolved identity carries a capability for one repository or
  repository glob
- **THEN** a repository that no pattern matches is refused

#### Scenario: Overlapping capabilities union

- **WHEN** two capability entries match the requested repository at different
  levels
- **THEN** the highest level wins, and `write` includes `read`

#### Scenario: Policy change takes effect on the next request

- **WHEN** the tailnet policy is edited to remove a capability
- **THEN** a new request is refused without any change to tsgit configuration or
  on-disk state

### Requirement: Repository names are matched canonically

The system MUST match capability patterns against the canonical
`<namespace>/<repo>` name, ignoring a single trailing `.git` in the URL, and
MUST treat `*` as not crossing `/`.

#### Scenario: URL suffix does not change the name

- **WHEN** a capability matches `acme/widget` and the client requests either
  `/acme/widget` or `/acme/widget.git`
- **THEN** both requests match the same capability

#### Scenario: A namespace glob does not cross a separator

- **WHEN** a capability grants `acme/*` and the request names `other/widget`
- **THEN** the request is refused

### Requirement: Deny by default

The system MUST refuse access when no capability grants it, and MUST treat a
capability payload it cannot interpret as granting nothing.

#### Scenario: No matching capability

- **WHEN** an identity has no capability granting access to the requested
  repository
- **THEN** the request is refused

#### Scenario: Malformed capability payload

- **WHEN** a capability entry cannot be decoded, has an unknown `access` value,
  or has the wrong shape
- **THEN** the request is refused, the error is logged, and no partial grant is
  applied

### Requirement: Tag-based access

The system MUST allow a capability granted to an ACL tag to authorize the
connection, so a tagged machine can access repositories without any user being
involved.

#### Scenario: Tagged machine reads

- **WHEN** a connection comes from a node carrying `tag:ci` and the tailnet
  policy grants that tag a read capability for the repository
- **THEN** the read is allowed even though no user login is associated with the
  connection

### Requirement: Write implies create

A `write` capability matching the requested canonical name MUST authorize
creation of that repository when it is missing. A `read` capability MUST NOT
authorize creation.

#### Scenario: Writer creates on first push

- **WHEN** an identity with `write` on a matching repository name requests the
  receive-pack service for a repository that does not exist
- **THEN** the repository is created and the push proceeds

#### Scenario: Reader cannot create

- **WHEN** an identity with only `read` on a matching repository name requests
  the receive-pack service for a repository that does not exist
- **THEN** the request is refused and no repository is created

#### Scenario: Writer cannot create outside its patterns

- **WHEN** an identity has `write` on `acme/*` and requests `other/repo`
- **THEN** the request is refused and no repository is created
