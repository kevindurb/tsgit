# Spec Delta

## ADDED Requirements

### Requirement: Admin authorizes deletion

Repository access levels MUST be ordered `read < write < admin`. A level at or
above `write` MUST include read and create authority. Only `admin` MUST
authorize deletion; `write` MUST NOT. The public Funnel principal MUST NOT have
admin access under any payload.

#### Scenario: Admin deletes

- **WHEN** an identity carries `admin` on the requested canonical name
- **THEN** deletion is allowed

#### Scenario: Write cannot delete

- **WHEN** an identity carries only `write` on the requested canonical name
- **THEN** deletion is refused and the repository is unchanged

#### Scenario: Admin retains read and write

- **WHEN** an identity carries `admin` on a matching name
- **THEN** read, push, and push-to-create are also allowed

#### Scenario: Admin is scoped to its patterns

- **WHEN** an identity carries `admin` on `acme/*` and requests deletion of
  `other/widget`
- **THEN** the deletion is refused

#### Scenario: Public principal cannot delete

- **WHEN** a Funnel request asks to delete any repository
- **THEN** the request is refused and no repository is changed

## MODIFIED Requirements

### Requirement: Authorization precedes execution and creation

The system MUST complete an authorization decision for the repository and
operation before starting any git backend process, before creating a
repository, and before deleting a repository.

#### Scenario: Allowed clone

- **WHEN** an identity may read the requested repository
- **THEN** the system starts the backend for the fetch

#### Scenario: Denied push never starts the backend

- **WHEN** an identity without write access requests a push
- **THEN** the system refuses the request, starts no backend, and does not
  create the repository

#### Scenario: Denied deletion never deletes

- **WHEN** an identity without admin access requests a deletion
- **THEN** the system refuses the request and the repository is unchanged

### Requirement: Write implies create

A capability at or above `write` that matches the requested canonical name MUST
authorize creation of that repository when it is missing. A `read` capability
MUST NOT authorize creation. The public Funnel principal MUST NOT write or
create, even if a capability payload contains a value that would otherwise
allow it.

#### Scenario: Writer creates on first push

- **WHEN** an identity with `write` on a matching repository name requests the
  receive-pack service for a repository that does not exist
- **THEN** the repository is created and the push proceeds

#### Scenario: Admin creates on first push

- **WHEN** an identity with `admin` on a matching repository name requests the
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
