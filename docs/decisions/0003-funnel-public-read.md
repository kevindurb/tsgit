# 3. Allow read-only public clone over Tailscale Funnel

## Status

Accepted.

## Context

The project scope in `docs/project.md` originally placed "users outside the
tailnet, and public anonymous clone" out of scope. That kept the trust boundary
single and simple: every request had a tailnet peer behind it, and
`tailscaled`'s `WhoIs` supplied the identity.

A tailnet operator may still want to publish a curated, read-only subset of
repositories — release artifacts, documentation, an open-source project —
without opening the whole instance, inventing accounts, or running a second
forge. Tailscale Funnel is the natural transport, but it is all-or-nothing per
node unless the application decides what to expose.

The smart-HTTP design
(`openspec/changes/archive/2026-11-07-add-http-git-serving/design.md`) left Funnel
out of scope deliberately. This decision revisits that boundary without changing
how tailnet requests are served.

## Decision

Public access is an opt-in add-on, not a second forge:

1. **Three independent keys.** The deployment flag `--funnel`, the tailnet
   `funnel` node attribute, and a non-empty `cap/funnel` payload under
   `nodeAttrs.app` are all required. None alone exposes a repository. Removing
   any one turns the public surface off.
2. **Read-only by payload shape.** `cap/funnel` is an array of `{ "repos": [...] }`
   objects with no `access` field. The resolver builds `AccessRead` grants, so
   the existing `authz.Decide` is reused unchanged and `CanCreate` is false. A
   public write is unrepresentable, not merely denied.
3. **The public principal is node-derived.** A Funnel request calls no `WhoIs`.
   Its grants come from the node's own capability map, read on each request via
   `StatusWithoutPeers`. A Funnel connection's remote address is Tailscale's
   relay, so it is never used as identity.
4. **A separate `FunnelOnly` listener.** tsnet dispatches by flow origin, so the
   public handler never sees a tailnet peer and the tailnet handler never sees a
   public client. There is no runtime classification by header or by a failed
   lookup.
5. **Upload-pack and filtered discovery only.** Receive-pack and push-to-create
   are refused before a backend starts or a repository is created. The public
   view never shows a write indicator or first-push instructions.
6. **A public failure never stops the tailnet.** A missing prerequisite, a bind
   failure, or a failed status lookup is logged and leaves the private forge
   serving.
7. **Bounded work.** Request bodies, headers, and request lifetimes are limited,
   and concurrent public pack exchanges are capped with a retryable `503`. Funnel
   itself is a backstop, not a guarantee.

## Consequences

- **No audience control.** Funnel is anonymous by design. The operator can
  restrict which repositories are public, not who may read them. Node sharing or
  an identity provider is the alternative if audience control is needed.
- **A policy typo can publish private code.** `repos: ["*/*"]` is one edit away
  from publishing everything, and there is no undo for a clone. Mitigations:
  read-only payload, the deployment flag as a second key, prominent logging, and
  a public badge in the tailnet index.
- **Public metadata is disclosed.** A public summary shows description, default
  branch, latest commit, and clone URL for repositories the operator chooses.
- **No accounts, no permission store, no on-disk public state.** The tailnet
  policy remains the only authorization source. A policy edit takes effect on the
  next public request without a restart.
- **The tailnet path is unchanged.** Identity, capabilities, push, discovery, and
  creation behave exactly as before.
- **Funnel is beta and plan-gated.** Deployment needs MagicDNS, HTTPS
  certificates, the `funnel` node attribute, and a supported port (443, 8443, or
  10000). These are documented in `docs/deployment.md`.
