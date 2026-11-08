# repository-deletion Specification

## Purpose

Remove one repository from the tailnet web UI through an admin-gated, confirmed
action, without adding accounts, sessions, or a database.

## Requirements

### Requirement: Deletion is an admin-gated tailnet action

The system MUST serve a repository deletion action on the tailnet listener
without requiring a separate configuration flag. The system MUST resolve the
requester through the same tailnet identity path used for git and discovery
requests, and MUST require `admin` access on the canonical repository name
before deleting anything. A requester without `admin`, and a repository that
does not exist, MUST receive the same not-found response. The Funnel listener
MUST NOT serve the deletion action.

#### Scenario: Admin opens the deletion surface

- **WHEN** a viewer with `admin` on an existing repository requests the deletion
  surface
- **THEN** the confirmation page is rendered for that repository

#### Scenario: Writer is refused

- **WHEN** a viewer with only `write` on the repository requests the deletion
  surface
- **THEN** the response is not-found and the repository is unchanged

#### Scenario: Denied and missing are indistinguishable

- **WHEN** a viewer without `admin` requests an existing repository, or any
  viewer requests a missing repository
- **THEN** both responses are the same not-found result

#### Scenario: Public listener does not serve deletion

- **WHEN** a Funnel request targets the deletion path
- **THEN** the request is refused as not-found and no repository is changed

### Requirement: Deletion requires a confirmation with the canonical name

The confirmation page MUST identify the canonical name and MUST show the
description, default branch, latest commit, and clone URL when they are
available. The page MUST warn that deletion is permanent. The system MUST NOT
delete a repository until it receives a `POST` whose confirmation value exactly
equals the canonical name. A mismatch MUST leave the repository and all of its
refs unchanged and MUST re-render an error.

#### Scenario: Matching confirmation deletes

- **WHEN** the submitted confirmation equals the canonical name
- **THEN** the repository is deleted

#### Scenario: Mismatched confirmation does not delete

- **WHEN** the submitted confirmation differs from the canonical name
- **THEN** the repository and all of its refs remain and an error is rendered

#### Scenario: Confirmation page shows what will be deleted

- **WHEN** the confirmation page is rendered
- **THEN** it shows the canonical name, description, default branch, latest
  commit, and clone URL when those values are available

### Requirement: Deletion requests are protected against cross-site submission

The system MUST accept deletion only on `POST`. The deletion form MUST carry a
token bound to the requester's node, the canonical name, the delete action, and
a short expiry, signed with a key that is not exposed to clients. The system
MUST reject a request whose token is missing, altered, or expired. When the
`Origin` or `Sec-Fetch-Site` header is present and indicates an initiator other
than the request's own origin, the system MUST reject the request. The system
MUST NOT require a cookie or a server-side session for this protection.

#### Scenario: Cross-site submission is refused

- **WHEN** a `POST` arrives with a cross-origin `Origin` or `Sec-Fetch-Site`
- **THEN** the request is refused and the repository is unchanged

#### Scenario: Missing or expired token is refused

- **WHEN** the token is absent, altered, or expired
- **THEN** the request is refused and nothing is deleted

#### Scenario: Same-origin submission succeeds

- **WHEN** a `POST` carries a valid token for this node and repository from the
  same origin
- **THEN** the deletion proceeds

### Requirement: Deletion is atomic and permanent

The system MUST remove the repository directory and all of its history. The
repository MUST disappear from the served tree atomically, so a reader sees
either the complete repository or a not-found result and never a partially
deleted repository. A clone, fetch, or push already in progress MUST be allowed
to finish against its open files. A request that begins after the deletion MUST
receive not-found. The system MUST NOT remove any other repository.

#### Scenario: Repository disappears

- **WHEN** deletion completes
- **THEN** the index no longer lists the repository and a clone request returns
  not-found

#### Scenario: In-flight transfer finishes

- **WHEN** a clone is reading the repository while deletion completes
- **THEN** the clone can finish and a new request returns not-found

#### Scenario: Second deletion is not-found

- **WHEN** deletion is requested for a name that has already been deleted
- **THEN** the response is not-found and no other repository is touched

### Requirement: Deletion is isolated to the named repository

The system MUST delete only the canonical repository named in the request and
MUST leave every other repository and namespace in place. The system MUST
attempt to remove a namespace directory that becomes empty and MUST ignore a
failure when the directory is not empty. The system MUST refuse to delete a
repository whose directory is a symlink, leaving the link and its target
unchanged.

#### Scenario: One of several repositories

- **WHEN** a namespace contains two repositories and one is deleted
- **THEN** the other repository remains readable and clonable

#### Scenario: Last repository in a namespace

- **WHEN** the last repository in a namespace is deleted
- **THEN** the namespace directory is removed when possible, and repositories in
  other namespaces are unaffected

#### Scenario: Symlinked repository is refused

- **WHEN** the repository directory is a symlink
- **THEN** the deletion is refused and the link and its target are unchanged

### Requirement: Deletion is logged

On success the system MUST log the canonical name, the requester's node, the
optional login, the node's tags, and the latest commit when it is available. On
refusal the system MUST log the reason and the requester. The log is the audit
record; the system MUST NOT create a separate audit store.

#### Scenario: Successful deletion is logged

- **WHEN** a deletion succeeds
- **THEN** the log identifies the repository, the requester, and the latest
  commit when known

#### Scenario: Refused deletion is logged

- **WHEN** a deletion is refused
- **THEN** the log records the reason and the repository is unchanged

### Requirement: The name is available after deletion

After a successful deletion the canonical name MUST no longer exist. A later
authorized push MUST create a new repository at that name, and the deleted
history MUST NOT be restored. The old clone URL MUST remain not-found until a
repository is created there again.

#### Scenario: Recreate after delete

- **WHEN** a writer pushes to a name that was deleted
- **THEN** a new empty repository is created and the old history is not restored

#### Scenario: Old clone URL before recreation

- **WHEN** a client clones a deleted name before a new repository is created
- **THEN** the response is not-found
