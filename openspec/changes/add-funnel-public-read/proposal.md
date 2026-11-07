# Proposal

## Why

`tsgit` serves only tailnet peers. A tailnet operator may still want to publish a
curated, read-only subset of repositories — release artifacts, documentation, an
open-source project — without inventing accounts, running a second forge, or
opening the whole instance. Tailscale Funnel is the natural transport, but it is
all-or-nothing per node unless the application decides what to expose. A tailnet
app capability can express that decision as policy, which keeps the project's
"no permission store" rule intact.

Now is the moment because the smart-HTTP and discovery slices have landed. There
is a stable read-only surface and a clear identity/authorization boundary to
extend, rather than a stub.

## What Changes

- Add an opt-in Funnel front end, off by default. The deployment flag
  `--funnel`, the tailnet `funnel` attribute, and a non-empty `cap/funnel`
  capability are three independent keys; none alone exposes anything.
- Add the node capability `github.com/kevindurb/tsgit/cap/funnel`, placed under
  `nodeAttrs.app`, whose payload lists repository patterns that are publicly
  readable. The payload has no `access` field: public access is read-only by
  construction.
- Resolve public requests to a synthetic read-only principal from the node's own
  capability, read per request through `StatusWithoutPeers`. Public requests
  never call `WhoIs`; a Funnel request is not a tailnet peer.
- Serve only clone/fetch (`git-upload-pack`) and a filtered discovery view over
  Funnel. Receive-pack, push-to-create, and every mutation are refused before a
  backend starts or a repository is created.
- Treat a missing or malformed capability as "nothing is public". A policy edit
  takes effect on the next public request with no restart and no on-disk state.
- If Funnel is requested but unavailable, keep serving the tailnet, log a
  prominent error, and do not open the public listener.
- Harden the public listener with bounded request bodies and timeouts and a
  configurable cap on concurrent pack exchanges.
- Mark public repositories in the tailnet discovery index, and never render
  write indicators or first-push instructions in the public view.

**BREAKING**: this changes the project's stated scope. `docs/project.md`
currently lists "users outside the tailnet, and public anonymous clone" as out of
scope. This change narrows that to "public anonymous clone of repositories
explicitly designated by tailnet policy, read-only". It does not add public
write, public accounts, or control over who may read.

## Capabilities

### New Capabilities

- `funnel-access`: the opt-in Funnel front end, the public read-only principal
  derived from `nodeAttrs.app cap/funnel`, the public clone/fetch and discovery
  surface, failure behavior, and public-path limits.

### Modified Capabilities

- `tailnet-identity`: Funnel requests are not resolved through `WhoIs`; they use
  a policy-derived public principal, and client-supplied values still cannot
  select or alter it.
- `access-control`: public read access is derived from the node's own
  `cap/funnel` node capability rather than a peer/connection capability; public
  write and create are always denied.
- `git-serving`: the Funnel listener serves upload-pack only; receive-pack is
  refused before a backend starts or a repository is created. The tailnet
  listener is unchanged.
- `repository-discovery`: the public view lists only funnel-visible repositories
  and never renders write indicators or push instructions.

## Impact

- `internal/config`: `--funnel` and `--funnel-max-concurrent`, with environment
  fallbacks and validation.
- `internal/identity`: a Funnel resolver backed by a `StatusWithoutPeers`
  interface; it stays free of `net/http`.
- `internal/authz`: the `CapFunnel` contract and a read-only grant type; the
  existing `Decide` logic is unchanged.
- `cmd/tsgit`: a Funnel-only listener, a second handler composition, dual-server
  shutdown, and failure logging; the tailnet path is untouched.
- `internal/web`: public-mode copy, no push affordances, and an optional public
  badge in the tailnet index.
- Tests: pure identity and authorization tests, public handler tests, template
  tests, and a manual smoke test for listener dispatch and custom
  `nodeAttrs.app` delivery.
- Documentation: project scope, architecture, configuration, deployment,
  access control, and a new ADR recording why Funnel is acceptable and what it
  does not promise.
- No new Go dependencies, no database, and no on-disk public flags.
