# Spec Delta

## MODIFIED Requirements

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
