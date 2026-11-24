# Tasks

## 1. Ref snapshot and diff plumbing

- [x] 1.1 Add a ref snapshot helper to `internal/repos` (`git for-each-ref` for `refs/heads`, `refs/tags`, and any other `refs/` namespaces worth reporting) returning map[refname]sha with a timeout, mirroring `readRefs`; verify with a test that creates refs in a temp bare repo and asserts the snapshot.
- [x] 1.2 Add `DiffRefs(before, after map[string]string) []RefChange` classifying created / updated / deleted (empty-oid for deletions); verify with table-driven unit tests including create, update, delete, no-op, and mixed refs.
- [x] 1.3 Add an ancestry check helper (using `git merge-base --is-ancestor <old> <new>`) to compute `forced` for updates, returning a sentinel error on failure (missing object); verify with tests for fast-forward vs forced update vs missing object.

## 2. Hook configuration from the policy

- [x] 2.1 Create `internal/hooks` with the `cap/hooks` payload type (`repos []string`, `url string`, `secretEnv string`) parsed strictly (unknown fields invalidate the entry); verify malformed payloads, unknown fields, and bad `path.Match` patterns are rejected with logged errors and grant nothing.
- [x] 2.2 Implement the policy reader: read `nodeAttrs.app["github.com/kevindurb/tsgit/cap/hooks"]` from the node every push (per-push, mirroring the funnel per-request read), returning matched hooks for a canonical repo name; verify that pattern matching, signedness setup, and re-read semantics are covered by tests against a fake LocalAPI.
- [x] 2.3 Implement `secretEnv` resolution at delivery time: variable present and non-empty enables signing; missing/empty is a hard error for that delivery (never send unsigned); verify all three paths (present, missing, empty) in tests and that the variable *name* (never value) appears in logs.

## 3. Payload and signing

- [x] 3.1 Implement the GitHub-shaped `push` payload builder (`ref`, `before`, `after`, `created`, `deleted`, `forced`, `repository.{name,full_name,clone_url}` from the configured public hostname/scheme, `pusher.name` from the resolved identity); verify with golden-file tests.
- [x] 3.2 Implement HMAC-SHA256 signing producing `X-Hub-Signature-256: sha256=<hex>` over the exact request body, plus `X-GitHub-Event: push`, `X-GitHub-Delivery: <uuid>` (crypto/rand), `Content-Type: application/json`, `User-Agent: tsgit/<version>`; verify signature validity against a reference HMAC computed in the test, and that headers are set as specified.

## 4. Delivery engine

- [x] 4.1 Implement the dispatcher: takes matched hooks + ref changes, emits one delivery per ref per hook into a background goroutine pool bounded by a semaphore (8), each attempt bounded by a 5s timeout, up to 3 attempts with ~1s/~5s backoff, no redirects, drained-and-discarded response bodies; verify retry, timeout, no-retry-on-2xx, and semaphore-drop behavior with a controllable fake HTTP client.
- [x] 4.2 Wire the delivery client to the embedded tsnet node's outbound HTTP client (verify in the tsnet API which constructor applies — `Server.HTTPClient` or an equivalent `DialContext` — and use it; never the host default transport); keep the HTTP client behind an interface so tests inject `httptest` servers; verify with a test that the production path dials through the tsnet client (e.g., via a stubbed dialer).
- [x] 4.3 Ensure deliveries, their goroutines, and pending retries never block or fail the receive-pack response: request-path code only snapshots, spawns, and returns; verify with an integration test where the receiver hangs and the push still completes promptly.

## 5. Push-path integration

- [x] 5.1 Add a nil-safe post-receive-pack seam to `internal/transport` (e.g., an optional observer set at handler construction) invoked after the backend returns for receive-pack requests, passing repo name, before/after snapshots, and the resolved identity; verify existing transport tests pass unchanged with no observer set.
- [x] 5.2 Wire the seam in `cmd/tsgit` (tailnet serving path and dev mode): build the dispatcher (policy reader from the LocalAPI client, tsnet HTTP client, signature/verify helpers, logger) and pass it into the git handler; in dev mode with no tailnet, hooks configure to a no-op dispatcher (a debug log at startup) and pushes behave exactly as before; verify `just run serve --dev-listen` pushes succeed with no hooks configured.
- [x] 5.3 Add an end-to-end test: push through the real dev-mode HTTP server against a real bare repo with a fake receiver (httptest via the injected client) — assert the receiver got exactly the expected per-ref deliveries, signed when `secretEnv` is set, unsigned otherwise, and nothing when the push is rejected by a reference-update hook.

## 6. Documentation

- [x] 6.1 Update `docs/project.md`: carve "CI notification, not CI execution" out of the CI/CD out-of-scope line; verify by reading the updated paragraph in context.
- [x] 6.2 Add the `cap/hooks` capability to `docs/access-control.md` (nodeAttrs example, signed/unsigned variants, `secretEnv` semantics, ACL scoping note); verify every claim in it against the implementation.
- [x] 6.3 Add ADR 0005 (`docs/decisions/0005-webhook-delivery.md`): tailnet-only delivery, secret-optional-with-fail-closed `secretEnv`, best-effort semantics, no persistence; verify it matches the spec's normative statements.
- [x] 6.4 Update `docs/architecture.md` with the delivery path (one paragraph plus the pipeline diagram edit); verify the diagram still matches the layers table.

## 7. Verification

- [x] 7.1 `just ci` passes (fmt, lint, build, test).
- [x] 7.2 `just spec-validate` passes for the new capability.
- [x] 7.3 Manual smoke: dev-mode push with a hook configured via a fake policy source delivers to a local `nc`/httptest listener; receiver output matches the spec's payload requirements (headers + fields).

## Workflow follow-up

- Archive the change after implementation and review are complete (`/opsx-archive`).
- Consider whether `docs/configuration.md` needs a note if any flag appears during implementation (it should not — configuration is policy-only).
