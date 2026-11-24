# Proposal

## Why

`tsgit` hosts code, but a push into it reaches no other system. Operators running
their own CI (Tekton, Woodpecker, Drone, …) either poll the ref across the
tailnet or run without push triggers at all. tsgit already has everything a
webhook needs — a reliable "this push changed these refs" moment, a WireGuard
network with node identity, and a policy channel for operator configuration —
so it can notify external CI directly, over the tailnet, without becoming a CI
system itself.

tsgit deliberately does not run CI: it will not execute pipelines, schedule
builds, or store delivery history. This change only delivers a signed-or-not,
best-effort POST to operator-configured URLs after a push: notification, not
execution.

## What Changes

- New outbound webhook on push: after a `git-receive-pack` exchange completes, tsgit diffs the repository's refs before and after the exchange; when any ref moved, it POSTs a GitHub-shaped `push` event to every configured hook matching the repository.
- Hook configuration lives in the tailnet policy as an app capability (`github.com/kevindurb/tsgit/cap/hooks`) on the `tsgit` node (`nodeAttrs`, like `cap/funnel`), not in files, environment, or a database. Strict parsing: an invalid entry grants nothing and is logged. Re-read on each push, so policy edits take effect without a restart.
- Delivery goes through the embedded tsnet node's HTTP client — never the host network stack — so webhook targets must be reachable tailnet addresses. This is the authentication model: the receiving endpoint's authorization is a tailnet ACL naming the tsgit node, not a bearer secret shipped in the policy.
- Optional per-hook HMAC: a hook entry may name an environment variable (`secretEnv`) holding an HMAC key; when set, deliveries carry `X-Hub-Signature-256` (GitHub-compatible). The policy holds only the variable's name, never its value, so the policy stays committable to git. A configured-but-missing env var fails closed: the delivery is not sent and an error is logged. A hook with no `secretEnv` delivers unsigned.
- Best-effort delivery: fire-and-retry in the background (3 attempts, ~1s/5s backoff, 5s per-attempt timeout, no redirects), bounded concurrency, no persistence, no ordering guarantee, deliveries lost on shutdown. The push is never blocked or failed by a webhook.
- Payload is deliberately lean: `ref`, `before`, `after`, `created`/`deleted`/`forced`, `repository{name, full_name, clone_url}`, `pusher`. `X-GitHub-Event: push` and `X-GitHub-Delivery: <uuid>` headers. Receivers can clone from `clone_url` with their own tailnet credentials.
- Lean scope for v1: `push` events only (push-to-create and ref deletions are covered by the same diff); repository deletion via the UI, branch-protection events, and delivery persistence are out of scope.
- Documentation: `docs/project.md` scope carves out "CI notification, not CI execution"; `docs/access-control.md` documents the new capability; an ADR records why delivery is tailnet-only and secret-optional.

## Capabilities

### New Capabilities
- `webhook-delivery`: push events to operator-configured, tailnet-reachable HTTP endpoints, configured via tailnet policy, with optional HMAC signing.

### Modified Capabilities
<!-- None: push behavior, authorization, and the smart-HTTP surface are unchanged. The hook fires after the pack exchange, not inside it. -->

## Impact

- Affected code: new `internal/hooks` (policy read, ref diff, dispatch, delivery), touched in `internal/transport` (post-receive hook point) and `cmd/tsgit` (wiring the dispatcher and the tsnet HTTP client); `internal/repos` gains a ref-snapshot helper alongside the existing `for-each-ref` read.
- APIs: new policy capability `cap/hooks`; no new flags, no new HTTP surface on tsgit itself (it is a caller, not a receiver). Zero new third-party dependencies (stdlib `net/http`, `crypto/hmac`).
- CI contract: receivers authenticate deliveries via tailnet ACL (recommended) or via `X-Hub-Signature-256` when `secretEnv` is set; Tekton consumption is via a CEL interceptor (unsigned) or the GitHub interceptor (signed).
- Risk surface: new outbound network path from the forge (mitigated by tsnet-only delivery and policy control of targets); one extra `for-each-ref` per ref-changing push.
