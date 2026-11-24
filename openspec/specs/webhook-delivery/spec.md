# webhook-delivery Specification

## Purpose

Notify operator-configured HTTP endpoints — typically CI systems — when a push
changes refs in a repository, so external automation can react without polling.
Delivery is best-effort, tailnet-native, and configured entirely through the
tailnet policy.

## Requirements

### Requirement: Push events are derived from ref movement
The system SHALL compare the set of refs and their target object IDs immediately
before and after a receive-pack exchange completes, and SHALL treat only refs
that were created, updated, or deleted as push events. A receive-pack exchange
that leaves all refs unchanged, or fails before updating any ref, SHALL NOT
produce an event.

#### Scenario: Refs changed by a push
- **WHEN** a client pushes a fast-forward update to an existing branch
- **THEN** one push event is reported containing that ref's old and new object IDs

#### Scenario: Failed push produces no event
- **WHEN** a push is rejected (for example by a reference-update hook) and no ref moved
- **THEN** no webhook delivery is made

#### Scenario: Ref deletion is an event
- **WHEN** a push deletes a branch
- **THEN** a push event is reported for that ref with the empty object ID as the new value

#### Scenario: First push to a new repository delivers
- **WHEN** an authorized writer pushes to a repository that did not previously exist
- **THEN** a push event is reported (creation is a ref update)

### Requirement: One event per changed ref
The system SHALL emit one delivery per changed ref, each carrying that ref's own
old and new values, rather than batching several refs into one payload.

#### Scenario: Multi-ref push
- **WHEN** a push updates two branches and one tag
- **THEN** three deliveries are made to each matching hook, one per ref

### Requirement: Hooks are configured through the tailnet policy
The system SHALL read hook configuration from the `github.com/kevindurb/tsgit/cap/hooks`
app capability under `nodeAttrs.app` of the node running tsgit, like `cap/funnel`.
Each entry SHALL name `repos` (a list of `path.Match` patterns against the
canonical name), a `url`, and optionally `secretEnv`. An invalid entry SHALL
grant nothing and be logged. Configuration SHALL be re-read on each push. A
node without the capability delivers nothing.

#### Scenario: Policy edit takes effect without restart
- **WHEN** the tailnet policy is edited to add a hook and a push occurs
- **THEN** the delivery is attempted according to the new policy without restarting tsgit

#### Scenario: Malformed entry grants nothing
- **WHEN** a hook entry contains an unknown field or an unusable repository pattern
- **THEN** that entry delivers nothing and the error is logged

#### Scenario: Non-matching repository
- **WHEN** a push moves refs in a repository matched by no hook's `repos` patterns
- **THEN** no delivery is made for that repository

#### Scenario: Multiple hooks match one repository
- **WHEN** two hook entries match the pushed repository
- **THEN** both receive deliveries, independently

### Requirement: Delivery goes over the tailnet
The system SHALL deliver webhook requests through the tailnet using the node's
own tailnet client, and SHALL NOT deliver via the host network stack. It SHALL
NOT follow redirects, and SHALL NOT deliver to a URL whose scheme is not
`http` or `https`.

#### Scenario: Delivery to a tailnet service
- **WHEN** a hook names a `http://ci.tools` style MagicDNS name reachable through the tailnet
- **THEN** the request is sent through the tailnet and the receiver observes a peer from the tailnet

#### Scenario: Non-HTTP scheme is refused
- **WHEN** a hook's URL uses a scheme other than `http` or `https`
- **THEN** no delivery is made and an error is logged

### Requirement: Delivery is asynchronous and best-effort
The system SHALL dispatch deliveries in the background; delivery SHALL NOT
block, delay, or fail the push. Each delivery SHALL be attempted at most 3
times (immediate, then bounded backoff) with a per-attempt timeout and no
redirects. Concurrency SHALL be bounded; excess deliveries are dropped with a
log entry. Pending deliveries are lost on shutdown and nothing is persisted.

#### Scenario: Failing receiver does not affect the push
- **WHEN** the configured receiver is unreachable or returns an error
- **THEN** the push completes normally and the delivery is retried per the retry policy, then dropped with a logged error

#### Scenario: Successful delivery is not retried
- **WHEN** the receiver responds with a 2xx status
- **THEN** no further attempts are made

#### Scenario: Slow receiver is bounded
- **WHEN** the receiver never responds
- **THEN** the attempt times out and counts as a failed attempt

### Requirement: Payload is GitHub-compatible
The system SHALL send a JSON body shaped as a GitHub `push` event containing
`ref`, `before`, `after`, `created`, `deleted`, `forced`, `repository.name`,
`repository.full_name`, `repository.clone_url`, and `pusher.name`. `forced` is
merge-base ancestry when computable, else false with a logged note.
`clone_url` SHALL point at the node's own smart-HTTP clone endpoint. Headers:
`Content-Type: application/json`, `X-GitHub-Event: push`, a unique
`X-GitHub-Delivery`, and a tsgit User-Agent.

#### Scenario: Receiver parses a stock payload
- **WHEN** a push event is delivered
- **THEN** a receiver configured for GitHub push webhooks can parse ref, before, after, and repository fields without custom mapping

#### Scenario: Deleted ref reports the empty object ID
- **WHEN** a branch is deleted
- **THEN** `after` is the all-zeros object ID and `deleted` is true

### Requirement: Signing is opt-in per hook
The system SHALL sign a delivery with HMAC-SHA256 when, and only when, the
hook entry names a `secretEnv` whose variable is present and non-empty; the
signature SHALL be `X-Hub-Signature-256: sha256=<hex>` over the exact body. If
`secretEnv` is set but the variable is missing or empty, no request SHALL be
sent and an error SHALL be logged. Without `secretEnv`, deliveries are
unsigned (tailnet ACLs are the authentication). The secret value SHALL NOT
appear in logs or errors.

#### Scenario: Signed delivery
- **WHEN** `secretEnv` names a set, non-empty variable
- **THEN** the request carries a valid `X-Hub-Signature-256` header

#### Scenario: Missing secret fails closed
- **WHEN** `secretEnv` names a variable that is unset or empty
- **THEN** no request is sent and an error naming the variable (not its value) is logged

#### Scenario: Unsigned delivery
- **WHEN** a hook omits `secretEnv`
- **THEN** the delivery is sent without an `X-Hub-Signature-256` header
