# git-serving Specification

## Purpose
Serve clone, fetch, and push over git smart HTTP by handing an authorized
request to a replaceable pack backend, and create a repository on the first
authorized push.

## Requirements

### Requirement: Smart HTTP git endpoints

The system MUST serve the git smart-HTTP endpoints for reference discovery,
fetch, and push: `GET .../info/refs?service=git-upload-pack`,
`GET .../info/refs?service=git-receive-pack`,
`POST .../git-upload-pack`, and `POST .../git-receive-pack`. The tailnet
listener serves all four endpoints. The Funnel listener serves only the
upload-pack endpoints; receive-pack is not part of the public surface and MUST
be refused before any backend starts.

#### Scenario: Clone over HTTP

- **WHEN** an authorized client runs `git clone https://host/<namespace>/<repo>.git`
- **THEN** the client receives the repository contents and a valid checkout

#### Scenario: Push over HTTP

- **WHEN** a client with write access pushes to an existing repository
- **THEN** the push succeeds and the refs are updated on disk

#### Scenario: Unsupported request is refused

- **WHEN** a client requests a method, service, or path outside the smart-HTTP
  endpoints
- **THEN** the request is refused and no backend starts

#### Scenario: Non-git path starts no backend

- **WHEN** a request matches no smart-HTTP endpoint
- **THEN** no git backend starts and no repository is created, whether the
  request is refused or served by another front end

#### Scenario: Public receive-pack is refused

- **WHEN** a Funnel client requests the receive-pack service
- **THEN** the request is refused as not found and no backend starts

### Requirement: Interoperability with stock git

The system MUST produce responses that a stock git client accepts for clone,
fetch, and push, including the smart-HTTP service line, correct content types,
and streaming responses rather than a buffered body.

#### Scenario: Protocol v2 fetch

- **WHEN** a client sends a `Git-Protocol: version=2` header during reference
  discovery or fetch
- **THEN** the backend negotiates protocol v2 and the clone succeeds

#### Scenario: Large repository streams

- **WHEN** a client clones a repository with a pack larger than available
  request memory
- **THEN** the pack is streamed through the response without buffering the
  whole pack

### Requirement: Replaceable pack backend

The system MUST perform the pack exchange through a backend boundary that can
be replaced without changing identity resolution, authorization, repository
resolution, or HTTP routing. The initial backend MUST use git's own plumbing.

#### Scenario: Fake backend in tests

- **WHEN** the HTTP handler is tested with a fake backend
- **THEN** identity, authorization, and repository routing behave the same as
  with the git backend

#### Scenario: Backend failure is explicit

- **WHEN** the backend fails before any response bytes are written
- **THEN** the request fails with a server error rather than an empty success

### Requirement: Push-to-create

The system MUST create a missing repository during receive-pack reference
discovery when the identity has write capability for the requested canonical
name, and MUST make the creation atomic so a partially initialized repository is
never visible.

#### Scenario: First push creates the repository

- **WHEN** an authorized writer pushes to a repository that does not exist
- **THEN** an empty bare repository is created and the push stores its refs

#### Scenario: Concurrent first pushes

- **WHEN** two authorized writers first-push the same missing repository at the
  same time
- **THEN** exactly one repository exists and both requests operate on it

#### Scenario: Read request does not create

- **WHEN** a client fetches a repository that does not exist
- **THEN** the request is refused and no repository is created

### Requirement: Repository name containment

The system MUST resolve a requested repository name within the configured root
and MUST reject any name that escapes it.

#### Scenario: Traversal is rejected

- **WHEN** a client requests a repository name containing `..`, an absolute
  path, or a path that resolves outside the root
- **THEN** the request is refused and no path outside the root is touched

### Requirement: Missing and denied are indistinguishable

The system MUST respond with the same not-found result for a repository the
identity may not access and one that does not exist, so the response does not
confirm whether a repository exists.

#### Scenario: No existence oracle

- **WHEN** an identity requests a repository it may not access
- **THEN** the response is the same as for a repository that does not exist

### Requirement: Development mode

The system MUST provide a localhost-only development mode that serves the same
HTTP handler without joining a tailnet, using a temporary repository root and a
development identity.

#### Scenario: Clone and push locally

- **WHEN** the daemon runs in development mode
- **THEN** a local git client can clone and push through the same handler without
  a tailnet or a configured repository root
