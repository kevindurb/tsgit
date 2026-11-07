# Spec Delta

## Purpose

Serve clone, fetch, and push over SSH by handing an authorized session to git's
own plumbing, so the wire protocol is git's responsibility rather than ours.

## ADDED Requirements

### Requirement: SSH git transport

The system MUST accept the standard git SSH command form
`git-upload-pack '<repo>'` and `git-receive-pack '<repo>'` on an authorized
session.

#### Scenario: Clone over SSH

- **WHEN** an authorized client runs `git clone ssh://host/<repo>.git`
- **THEN** the client receives the repository contents and a valid checkout

#### Scenario: Push over SSH

- **WHEN** a client with write access pushes to a repository
- **THEN** the push succeeds and the refs are updated on disk

### Requirement: Repository name containment

The system MUST resolve a requested repository name within the configured root
directory and MUST reject any name that escapes it.

#### Scenario: Traversal is rejected

- **WHEN** a client requests a repository name containing `..` or an absolute
  path that resolves outside the root
- **THEN** the request is refused and no path outside the root is touched

### Requirement: Unknown repository is reported as such

The system MUST refuse a request for a repository that does not exist,
distinguishing it from a permission failure.

#### Scenario: Missing repository

- **WHEN** an authorized identity requests a repository that is not present
  under the root
- **THEN** the session fails with a not-found error, not a permission error
