# Spec Delta

## MODIFIED Requirements

### Requirement: Smart HTTP git endpoints

The system MUST serve the git smart-HTTP endpoints for reference discovery,
fetch, and push: `GET .../info/refs?service=git-upload-pack`,
`GET .../info/refs?service=git-receive-pack`,
`POST .../git-upload-pack`, and `POST .../git-receive-pack`. The system MUST
route a request that matches one of these endpoints to the git handler. A
request that matches none of them is not a git request: it MUST start no git
backend, and another front end MAY serve it under its own authorization.

#### Scenario: Clone over HTTP

- **WHEN** an authorized client runs `git clone https://host/<namespace>/<repo>.git`
- **THEN** the client receives the repository contents and a valid checkout

#### Scenario: Push over HTTP

- **WHEN** a client with write access pushes to an existing repository
- **THEN** the push succeeds and the refs are updated on disk

#### Scenario: Unsupported request is refused

- **WHEN** a client sends a smart-HTTP method, service, or path that the git
  handler does not recognize
- **THEN** the request is refused as a git request and no backend starts

#### Scenario: Non-git path starts no backend

- **WHEN** a request matches no smart-HTTP endpoint
- **THEN** no git backend starts and no repository is created, whether the
  request is refused or served by another front end
