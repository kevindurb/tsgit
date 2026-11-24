# Design

## Context

tsgit serves git smart HTTP from a single node; pushes arrive as
`git-receive-pack` exchanges that `internal/transport` hands to `git receive-pack
--stateless-rpc` and streams to the client. There is no event system, no
database, and no hook point today — the push path is `resolve identity →
authorize → resolve/create repo → backend`. The project runs on the tailnet,
has no public surface for pushes, and its authorization model is tailnet
identity + tailnet policy. See proposal.md for motivation.

Constraints that shape this design:

- No database, no queue, no new always-on service: tsgit is one binary whose
  state is bare git directories and a tsnet state dir.
- Tailnet-first: anything that can be done with the tailnet instead of new
  credentials SHOULD be (that is the project's whole thesis).
- The tailnet policy is the only operator-facing configuration channel with a
  precedent for per-node JSON (`cap/funnel`), and it is commonly committed to
  git — so it must never carry secret values.
- A webhook call is a side effect of a push; it must never change the push's
  outcome.

## Goals / Non-Goals

**Goals:**

- After an authorized push changes refs, deliver a GitHub-compatible `push`
  payload to operator-configured endpoints reachable over the tailnet.
- Authorization to configure hooks belongs to the operator (policy), never to
  callers, repositories, or the pushing user.
- Configuration editable without a tsgit restart.
- Push latency and failure semantics are unchanged by hook delivery.
- Receivers on the tailnet (Tekton EventListener, Woodpecker, Drone, Jenkins
  Generic Webhook Trigger, hand-rolled scripts) work with zero or minimal
  receiver-side custom code.

**Non-Goals:**

- Running CI, building, or executing anything (this is notification, not execution).
- Events other than `push` (repository deletion, branch protection, notes).
- Delivery guarantees: persistence, ordering, exactly-once, replay. Receivers
  that need at-least-once semantics must tolerate duplicates or dedupe on
  `after`.
- Delivery outside the tailnet (no public URLs, no relay service). The GitHub
  interceptor path for off-tailnet receivers is out of scope; an operator who
  needs SaaS CI can bridge with their own tunnel.
- Per-push user-defined endpoints (a pusher cannot register their own hook).

## Decisions

### 1. Detect ref movement by diffing refs around the pack exchange

Snapshot `git for-each-ref --format=%(refname)%00%(objectname)` (under a timeout,
reusing the pattern in `internal/repos`) immediately before the backend starts
and immediately after it returns; the symmetric difference is the event set.

Alternatives considered:

- **Server-side `post-receive` hooks written into each bare repo.** Catches
  pushes from any git path, but requires installing and maintaining hooks in
  every repo (including existing ones and ones restored from backup), gives us
  no policy-controlled config channel, and duplicates the delivery pipeline per
  repo. Rejected.
- **Parse the receive-pack `report-status` pkt-lines out of the response
  stream.** Protocol-specific, garbled by framing (v0 vs v2), and couples the
  hook to backend internals. Rejected.
- **`GIT_INTERNAL_SUPER_PREFIX`/reflog watching.** Racy and touches git
  internals. Rejected.

Ref-diff is the ground truth: it covers push-to-create (repo appears with refs),
branch/tag create/update/delete, force pushes, and rejected pushes (empty diff,
no event), regardless of protocol version or transport bookkeeping. Cost is one
extra `for-each-ref` per push — bounded and cheap next to a pack exchange.

### 2. One delivery per changed ref

GitHub fires one `push` event per ref; receivers are written to expect that.
Batching refs into one payload would require a bespoke schema, defeating the
GitHub-compatibility goal. A single push that updates N refs produces N
deliveries per matching hook.

### 3. Delivery through the embedded tsnet node, not the host network

`tsnet.Server` already gives tsgit a tailnet identity. The dialer for webhooks
is built from that node (the same way `Funnel` reads node status), never from
the host's default transport. Consequences:

- WireGuard authenticates and encrypts the hop end to end; the receiver can
  WhoIs the caller and see the tsgit node (this is the authentication story).
- The host's routes, proxy env vars, and CA bundle are irrelevant; a hook
  cannot reach the public internet or `169.254.169.254` because there is no
  route (no exit node, no subnet routes on a tsnet node unless the operator
  grants them).
- `http://` inside the tailnet is acceptable — the wire is already encrypted;
  TLS to the receiver remains available for defense in depth and is what
  MagicDNS + `tailscale cert` deployments will use.
- URL schemes are restricted to `http`/`https` as belt and braces; no redirect
  following, so a 3xx from the receiver is a delivery failure, not a pivot.

Alternative rejected: delivering via the host network with an allowlist. That
reintroduces SSRF as a real (not merely theoretical) surface and discards the
node's network position; it only makes sense for off-tailnet delivery, which is
explicitly out of scope.

### 4. Hooks are configured in the policy, but secrets are not

`cap/hooks` under `nodeAttrs.app` on the tsgit node, mirroring `cap/funnel`:

```jsonc
"nodeAttrs": [{
  "target": ["tag:tsgit"],
  "app": {
    "github.com/kevindurb/tsgit/cap/hooks": [
      {
        "repos": ["acme/*"],
        "url": "http://tekton.tools.ts.net/hook",
        "secretEnv": "TSC_HOOK_TEKTON"        // optional; the NAME, never the value
      }
    ]
  }
}]
```

- An entry is `{repos: [path.Match patterns], url: http(s) URL, secretEnv?: string}`.
  Unknown fields, wrong types, or an unparseable pattern invalidate the entry;
  invalid entries grant nothing and are logged (same strictness as `cap/funnel`).
- The cap is re-read on every push (one LocalAPI call the size of the node's
  capability map — the same approach the funnel path uses per request), so a
  policy edit needs no restart.
- Secrets never enter the policy because policies are routinely committed to
  git. `secretEnv` names an environment variable the tsgit process reads at
  delivery time; the value lives exactly where `TS_AUTHKEY`-style secrets
  already live (unit file, container env, secret manager). Rotation = update
  env + restart, same operational shape as today's auth key.
- If `secretEnv` is set but the variable is absent/empty, the delivery is
  **not sent** and an error is logged (fail closed). If `secretEnv` is absent,
  the delivery is unsigned — valid in the common case where the receiving
  EventListener is reachable only from `tag:tsgit` via ACL. This is the
  documented posture: **the tailnet ACL authenticates the caller; HMAC is
  opt-in defense in depth**, and it is what unlocks Tekton's GitHub
  ClusterInterceptor (`secretRef`) for receivers that cannot ACL.

Alternatives rejected: secret value in policy (leaks via git — the failure this
change was shaped around); one global `--webhook-secret` flag (one leaked
receiver forges for all hooks; rotation is all-or-nothing); per-repo git config
(no review surface, no central inventory, secrets sprawl across repo dirs).

### 5. GitHub-compatible `push` payload, lean by contract

Body fields: `ref`, `before`, `after`, `created`, `deleted`, `forced`,
`repository{name, full_name, clone_url}`, `pusher.name`. Headers:
`Content-Type: application/json`, `X-GitHub-Event: push`,
`X-GitHub-Delivery: <uuid v4>`, `User-Agent: tsgit/<version>`; plus
`X-Hub-Signature-256: sha256=<hex>` when signed.

- `forced` is computed with `git merge-base --is-ancestor before after`; if the
  check errors (e.g., objects pruned mid-push), the field is `false` and a line
  is logged — a receiver can always treat it as advisory.
- `clone_url` is derived from the configured hostname/port and the effective
  scheme (`https` when the node serves HTTPS, else `http`), pointing at the
  same path the git remote uses; a CI receiver clones straight from it using
  its own tag grant — no hand-maintained URLs.
- `pusher.name` is the peer's login, or the node name when the pusher is a
  tagged machine without a login.
- No commit messages, authors, or diffs. Receivers that want them fetch the
  ref; keeping them out keeps the payload stable, avoids leaking commit text
  to endpoints that only need a trigger, and keeps us out of the business of
  serializing git objects.

Alternatives rejected: Gitea-shaped payload (near-identical but strictly less
tooling speaks it; Tekton has no Gitea interceptor); a minimal custom schema
(every receiver needs a bespoke CEL binding, and we gain nothing).

### 6. Best-effort, bounded, asynchronous delivery

Delivery happens in a goroutine spawned after the backend returns, off the
request path entirely; the push neither waits on nor fails because of a
webhook. Per delivery: immediate attempt, then retry after ~1s and ~5s
(3 attempts total), 5s per-attempt timeout, redirects never followed, response
body drained-and-discarded (capped) on success, status logged otherwise. A
process-wide semaphore (8 concurrent deliveries) caps goroutines; overflow is
dropped with a log line rather than queued. Pending retries are not persisted
and are lost on shutdown. No ordering guarantee across deliveries.

Alternatives rejected: synchronous delivery (webhook latency lands on `git
push`, hook outage fails pushes); a persistent queue (new storage engine,
crash-recovery semantics, and operational surface far beyond the value of an
at-most-3-times notification); unbounded goroutines (a hung receiver becomes a
resource leak).

### 7. Ref-diff placement and failure isolation

The pre-snapshot happens after authorization and before the backend starts; the
post-snapshot runs after the backend returns and before the push handler
returns. If the pre-snapshot fails (repo unreadable, timeout), no event is
possible and the push proceeds untouched — the failure is logged. If the
post-snapshot fails, likewise: push result stands, error logged, zero
deliveries. Delivery itself can never affect the HTTP response. Funnel is
untouched (read-only; no receive-pack there), and the web UI delete path does
not emit events in v1.

## Risks / Trade-offs

- **[Risk]** A receiver reachable only from the tailnet still trusts
  `tag:tsgit`; a compromised tsgit node could forge deliveries. → Mitigation:
  HMAC opt-in; the node already holds the keys to the repositories, so this
  adds no new capability to an attacker who owns it.
- **[Risk]** Operator points `url` at the wrong tailnet service (e.g. an
  admin panel, not the CI listener). → Mitigation: signing makes forgery
  detectable only if the receiver verifies it; document the pattern of a
  dedicated hook port + ACL scoping `tag:tsgit → tag:ci:port`; tsgit cannot
  judge intent, only reachability.
- **[Trade-off]** Policy-read-per-push adds one LocalAPI round trip per push.
  → Accepted; pushes are human-rate operations and the funnel path already
  pays this per request.
- **[Trade-off]** At-most-3-times, unordered delivery will miss events during
  outages longer than seconds. → Accepted and documented; CI systems with
  `push`-fallback polling or manual retrigger are the norm; persistence is a
  later change if reality disagrees.
- **[Trade-off]** Ref-diff cannot distinguish "ref moved then moved back"; a
  push that nets no ref change delivers nothing. That is the correct CI
  semantics (nothing to build) and is documented.
- **[Risk]** `forced` relies on merge-base on objects that may be concurrently
  gc'd. → Treat as advisory: false on error, logged.
- **[Trade-off]** No webhook for repository deletion/mirror events in v1.
  → Intentional scope; the event model (`X-GitHub-Event` header values) leaves
  room to add them without breaking receivers.

## Migration Plan

Additive only; no data migration, no flags, no state. Rollback is: remove the
`nodeAttrs` entry from the policy (takes effect on next push) or revert the
binary. Repos untouched; nothing is written to the repository tree. Rollout
sequence: ship the binary → operators add `cap/hooks` to policy for a canary repo pattern → watch logs for `hook delivery` lines → widen the pattern.

Docs to add: `docs/project.md` scope carve-out (notification, not execution),
`docs/access-control.md` capability reference for `cap/hooks` with a signed and
unsigned example, and a short `docs/webhooks.md` (or ADR 0005) capturing the
tailnet-only-delivery decision and the fail-closed `secretEnv` rule.

## Open Questions

None blocking. Deferred by explicit scope line: whether repository deletion
should ever emit an event (needs a `repository` event type and UI plumbing;
only if operators ask).
