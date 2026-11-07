# Tasks

## 1. Validate the external assumptions

- [x] 1.1 Spike a custom `nodeAttrs.app` capability: on a real tailnet, add a
  `github.com/kevindurb/tsgit/cap/funnel` payload to the tsgit node and verify
  its `Self.CapMap` exposes the value (temporary debug log or a small status
  harness). Record the result. If it fails, stop and revisit the allowlist
  source before implementing.
  Result: verified on a real tailnet; the node capability reached `Self.CapMap`.
- [x] 1.2 Spike same-port listener dispatch: bind a tailnet TLS listener and a
  `FunnelOnly` Funnel listener on the same port, then verify a tailnet request
  reaches the tailnet handler and a public request reaches the public handler.
  Record the result. If it fails, move Funnel to 8443/10000 and note the public
  URL change.
  Result: verified on a real tailnet; both listeners served on the same port
  with distinct identities.

## 2. Configuration and capability contract

- [x] 2.1 Add `--funnel` and `--funnel-max-concurrent` to `internal/config` with
  environment fallbacks, a default of 4, and validation that the concurrency
  value is positive; verify table tests cover flag-over-environment precedence
  and the invalid value.
- [x] 2.2 Add `authz.CapFunnel` and a read-only public grant type whose payload
  contains only `repos` patterns; verify the package still compiles without
  `net/http` or `tailscale.com`, and that a synthesized public identity allows
  read but denies write and create.
- [x] 2.3 Document `--funnel` and `--funnel-max-concurrent` in
  `docs/configuration.md`; verify every documented flag is accepted by a config
  parsing test.

## 3. Public identity

- [x] 3.1 Add an `identity.Funnel` resolver backed by a
  `StatusWithoutPeers` interface. Decode `cap/funnel` from `Self.CapMap` into
  read-only grants; treat a missing or malformed payload as zero grants; return
  an error when the status lookup fails. Verify unit tests with fake statuses
  for valid, missing, malformed, and failing cases.
- [x] 3.2 Verify the public path never calls `WhoIs`: add a test that a Funnel
  request resolves from the node capability even when its address would not
  resolve to a tailnet peer, and a test that a tailnet request carrying a
  spoofed `Tailscale-Funnel-Request` header still uses its tailnet identity.
- [x] 3.3 Document the `cap/funnel` payload, its `nodeAttrs.app` placement, and
  why `grants[].app` has no public effect in `docs/access-control.md`; verify
  the documented JSON is used as a parser fixture in a test.

## 4. Listener and composition

- [x] 4.1 Add a `ListenFunnel` seam to the tailnet wrapper and open the
  Funnel-only listener only when `--funnel` is enabled. If the listener cannot
  be created, log a prominent error and continue with the tailnet listeners
  only. Verify the enable/disable decision with unit tests and the failure path
  with a fake tailnet.
- [x] 4.2 Compose a second git/web handler pair with the Funnel resolver and a
  public-mode web configuration, run both HTTP servers, and shut both down
  gracefully. Verify the tailnet handler is never built with the Funnel resolver
  and that an in-flight public request drains on shutdown.
- [x] 4.3 Update `docs/architecture.md` with the Funnel front end and the
  listener trust boundary, and `docs/deployment.md` with the Funnel
  prerequisites and public URL shape; verify the documented routes and ports
  match the listener tests.
- [x] 4.4 Add `docs/decisions/0003-funnel-public-read.md` recording the
  read-only decision, the no-audience-control limitation, and the three
  independent opt-ins; verify it references the project scope change and the
  smart-HTTP design.

## 5. Public surface

- [x] 5.1 Ensure public receive-pack and push-to-create are refused before any
  backend starts or directory is created; verify handler tests assert the same
  404 as for an unreadable repository and that no repository appears.
- [x] 5.2 Filter the public index and summary by the public principal; verify
  tests for a public repository, a non-public repository, and a missing
  repository, including identical responses for the latter two.
- [x] 5.3 Suppress write indicators and first-push instructions in the public
  view, and mark public repositories in the tailnet index; verify template tests
  for a public empty repository and for a tailnet row with the public badge.
- [x] 5.4 Amend the out-of-scope statement in `docs/project.md` to allow
  read-only public clone of policy-designated repositories; verify it does not
  contradict `proposal.md` and `just spec-validate` still passes.
- [x] 5.5 Add a handler-level end-to-end test with the Funnel resolver over
  `httptest`: a public clone succeeds, a non-public clone is 404, and a
  receive-pack request is 404 with no backend call. Verify with a fake backend
  that records calls.

## 6. Public hardening

- [x] 6.1 Configure the public HTTP server with the design's timeout and header
  limits and enforce the public request-body limit. Verify tests cover `413` for
  an oversized declared body and that the configured limits match the design.
- [x] 6.2 Add the concurrent-pack limiter for public `POST .../git-upload-pack`.
  Verify a saturated limiter returns `503` with `Retry-After`, a completed
  request releases its slot, and no unbounded queue is created.
- [x] 6.3 Log the public client source address from `FunnelConn.Src` through a
  connection-context callback. Verify a test with a synthetic wrapped connection
  records the client address, not the relay address, and that the value is never
  used for authorization.
- [x] 6.4 Document the public limits and their defaults in
  `docs/configuration.md` and `docs/deployment.md`; verify the documented values
  match the code constants and flag defaults.

## 7. Integration and archive

- [x] 7.1 Run `just ci` and `just spec-validate`; verify both pass.
- [x] 7.2 Manual real-tailnet verification: from outside the tailnet, clone a
  public repository, confirm a non-public repository is 404, confirm
  receive-pack is refused, remove the public capability and confirm the next
  request is 404, and confirm tailnet clone and push still work. Record the
  result without any tailnet-specific hostname or identifier in the repository.
  Result: verified on a real tailnet; public and private behavior matched the
  spec.
- [x] 7.3 Archive the change and sync specs to `openspec/specs/`.
