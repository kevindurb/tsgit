# 5. Webhook delivery is tailnet-only and best-effort

## Status

Accepted.

## Context

Pushes into tsgit produced no signal outside the forge. External automation —
typically CI — had to poll refs over HTTP to notice changes, or go without
build-on-push entirely. The obvious fix, "webhooks", carries a familiar set of
risks borrowed from public forges: SSRF into internal services, secrets in
configuration, and delivery guarantees that grow into a queueing system.

Two constraints shaped the answer. First, tsgit has no database and no desire
for one; any design that needs replay, ordering, or exactly-once delivery is
out before the first line is written. Second, the whole product premise is
that the tailnet is the security boundary: identity comes from tailscaled, and
policy is configuration. A webhook design that smuggles in its own trust model
would undermine that premise.

## Decision

1. **Delivery happens over the tailnet, through the node's own HTTP client.**
   Webhooks dial with the embedded tsnet node's client. The host network, its
   routes, and its proxy environment are never used. There is no route from a
   webhook URL to the public internet or to `169.254.169.254` unless the
   operator deliberately grants one to the tsgit node — and policy, not
   application code, decides that.

2. **The tailnet ACL is the authentication; HMAC is opt-in.** The receiver is
   expected to accept connections only from `tag:tsgit` (an ACL grant on the
   hook port). A hook may optionally name an environment variable in
   `secretEnv`; deliveries then carry `X-Hub-Signature-256` (HMAC-SHA256),
   which is what Tekton's GitHub ClusterInterceptor and similar receivers can
   verify. If `secretEnv` is set but the variable is missing or empty, no
   request is sent and an error is logged — the delivery fails closed, never
   silently unsigned. Secrets never appear in policy, never in logs; rotation
   is an environment change plus a restart.

3. **Push detection is a ref diff, not a hook and not a wire parse.** Refs are
   snapshotted before and after the pack exchange and compared; only actual
   movement produces an event. Rejected pushes deliver nothing. This costs two
   `for-each-ref` calls per push and zero new on-disk state.

4. **Delivery is best-effort and unordered.** Three attempts with short
   backoff, five seconds per attempt, at most eight concurrent deliveries per
   node, no persistence, no reordering protection, and nothing delivered is
   stronger than the network providing it. A failed delivery is logged and
   dropped. The push itself is never blocked, delayed, or failed by a webhook.

5. **Payloads are GitHub-compatible `push` events.** One delivery per changed
   ref (`created`/`updated`/`deleted`, `forced` as merge-base ancestry, object
   IDs, `repository.clone_url` pointing at this node). Receivers written for
   GitHub receive the event unchanged; Tekton consumes it via a CEL interceptor
   with no secret at all, or via the GitHub interceptor with `secretRef`.

Alternatives rejected:

- **Host-network delivery with an allowlist** reintroduces SSRF and bypasses
  the tailnet boundary the rest of the product depends on.
- **Git post-receive hooks installed per repository** sprawl across the repo
  tree, miss nothing but cost a config channel we do not have, and execute
  whatever is on disk rather than what policy says.
- **Parsing the receive-pack `report-status` stream** couples notification to
  protocol framing details and fails on rejection paths that move no refs.
- **A persistent delivery queue** buys at-least-once semantics nobody asked
  for, at the cost of a storage engine, crash recovery, and an operational
  surface this project does not otherwise have.
- **Unsigned by default with no opt-out; or signed-always.** The first leaves
  receivers that require signatures stranded; the second forces every receiver
  to hold a shared secret even when the tailnet ACL already authenticates the
  sender. Optional keeps both honest.

## Consequences

- Operators who run CI on the tailnet get build-on-push with one nodeAttrs
  entry and one ACL grant; no secrets are required anywhere.
- Operators who want defense in depth (or who cannot ACL the hook receiver)
  set `secretEnv` and verify the signature; the policy still names only a
  variable, not a value.
- Deliveries can be lost (process restart mid-retry, outage longer than
  seconds, receiver down past the third attempt). CI must tolerate a missed
  trigger; triggering on the next push or on a schedule remains the fallback.
- Multi-ref pushes produce one delivery per ref; ordering across deliveries is
  not guaranteed. Receivers key work off SHAs, not arrival order.
- Anything other than push events (repository deletion, branch protection,
  notes) is out of scope until a receiver asks for it; the event model leaves
  room to add them without breaking existing receivers.
