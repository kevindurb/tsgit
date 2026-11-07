# Spec Delta

## MODIFIED Requirements

### Requirement: Authorization derives from tailnet app capabilities

The system MUST derive repository access from tailnet policy. For a tailnet
identity, access comes from the app capabilities resolved for the connection.
For a Funnel request, read access comes from the node's own `nodeAttrs.app`
`cap/funnel` payload. The system MUST NOT read a local policy file or consult
any other authorization store.

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

#### Scenario: Public read comes from the node capability

- **WHEN** a Funnel request matches a repository pattern in the node's
  `cap/funnel` payload
- **THEN** read access is allowed, and only read access

### Requirement: Write implies create

A `write` capability matching the requested canonical name MUST authorize
creation of that repository when it is missing. A `read` capability MUST NOT
authorize creation. The public Funnel principal MUST NOT write or create, even
if a capability payload contains a value that would otherwise allow it.

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

#### Scenario: Public principal cannot create

- **WHEN** a Funnel request asks to write to or create a repository
- **THEN** the request is refused and no repository is created or changed
