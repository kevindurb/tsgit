# Design

## Context

See `proposal.md` for motivation. The current implementation serves HTTP from an
embedded `tsnet` node: a tailnet TLS listener on `--port`, an HTTP redirect
listener on `--http-port`, `WhoIs` on every request, and app capabilities from
the peer's `PeerCapMap`. The discovery front end is separate but uses the same
resolver. Funnel is currently named as a non-goal in `docs/project.md` and in
the archived smart-HTTP design.

Three properties of the vendored `tsnet` v1.104.5 shape this design:

- `Server.ListenFunnel("tcp", addr, tsnet.FunnelOnly())` returns a TLS listener
  that accepts only Funnel flows. Listeners are keyed internally by
  `(network, addr, port, funnel)`, so a Funnel-only listener and a tailnet TLS
  listener can share `:443` without colliding.
- A public flow is delivered as `*ipn.FunnelConn`. Its embedded connection's
  remote address is the Funnel relay, not the client; the real client address is
  `FunnelConn.Src`. `WhoIs` on the relay address is at best unhelpful and at
  worst attributes a tailnet identity to public traffic.
- `nodeAttrs` carries both `attr` (for example `funnel`) and `app` payloads.
  The node's own capability map is exposed as `Self.CapMap` / `SelfNode.CapMap`,
  and `tailcfg.UnmarshalNodeCapJSON` decodes it. This is the same mechanism
  `sniproxy` uses to read device-level configuration.

## Goals / Non-Goals

**Goals:**

- Expose a policy-selected, read-only set of repositories over Funnel without
  adding accounts, tokens, or on-disk authorization state.
- Keep the tailnet front end exactly as it is: same identity source, same
  capabilities, same push behavior.
- Make public traffic structurally unable to reach tailnet identity or write
  paths.
- Keep a Funnel failure from taking down the private forge.

**Non-Goals:**

- Restricting *who* may read a public repository. Funnel is anonymous by design.
- Public push, public repository creation, or any public mutation.
- Public SSH, public code browsing, issues, or pull requests.
- A local public-repository list, a permission database, or a second identity
  provider.
- Supporting a host `tailscaled` or Tailscale Service mode.

## Decisions

### Separate Funnel-only listener, same host and port

The daemon keeps `ListenTLS(":443")` for the tailnet and adds
`ListenFunnel(":443", tsnet.FunnelOnly())` when Funnel is enabled. Each listener
gets its own handler composition. tsnet dispatches by flow origin, so the
tailnet handler never sees a public connection and the public handler never sees
a tailnet peer.

Why not one mixed listener with runtime classification? A mixed listener would
have to decide identity from `FunnelConn` type, a client-controlled
`Tailscale-Funnel-Request` header, or a failed `WhoIs`. The first is subtle, the
second is spoofable, and the third is a fail-open hazard on any LocalAPI
problem. Separate listeners make the boundary structural.

Why not Funnel on 8443? It is the fallback if the same-port combination proves
unreliable on a real tailnet. The cost is a port suffix in public clone URLs,
not a change to the authorization model.

### Public identity from `nodeAttrs.app`, read per request

The public allowlist is a device property, not a connection property. The
operator writes:

```json
"nodeAttrs": [{
  "target": ["tag:tsgit"],
  "attr":   ["funnel"],
  "app": {
    "github.com/kevindurb/tsgit/cap/funnel": [
      { "repos": ["public/*", "shared/docs"] }
    ]
  }
}]
```

The public resolver calls `local.Client.StatusWithoutPeers(ctx)` on each public
request, decodes the payload, and builds a synthetic identity:

```go
authz.Identity{
    Node:   "funnel",
    Grants: []authz.Grant{{Repos: patterns, Access: authz.AccessRead}},
}
```

`StatusWithoutPeers` is an in-process LocalAPI call and omits the peer list, so
it is cheap. Reading it per request gives the same "policy change takes effect
on the next request" property the tailnet path has, without a watcher, a
reconnect loop, or a stale snapshot. A lookup failure returns an error and the
public handler answers 503; the tailnet handler is unaffected. A malformed
payload returns zero grants, not an error, matching the existing capability
behavior.

Why not `grants[].app`? Grant capabilities land in the *source peer's*
`PeerCapMap`. Anonymous public clients have no peer and no `WhoIs` response, so
that placement cannot reach them. It is a useful failure mode to document: a
correct-looking grant has no public effect.

Why not a local file or flag? It would create a second authorization source and
break the project's "tailnet policy is the only source" invariant. The
deployment flag exists only to opt into opening the listener, not to choose
content.

Why not a `WatchIPNBus` snapshot? It works, but it adds a goroutine, update
semantics, and a stale-policy question when the watch fails. The per-request
read is smaller and fails closed for the one public request that cannot read
policy.

### Read-only by payload shape

The `cap/funnel` payload is an array of `{ "repos": [...] }` objects. It has no
`access` field. Public write is therefore not merely denied by policy; it is
unrepresentable in the contract. Internally the resolver still builds an
`authz.Grant` with `AccessRead`, so the existing pure `Decide` function is reused
unchanged and `CanCreate` is always false.

Why not reuse the `cap/repo` grant shape? A policy author could write
`access: "write"` and reasonably expect anonymous push, or the implementation
would have to silently clamp it. A separate shape makes the guarantee legible.

### Public failures leave the tailnet serving

If `--funnel` is set but the node lacks the `funnel` attribute, HTTPS, or a
certificate domain, or the Funnel listener cannot bind, the daemon logs a
prominent error and continues with the tailnet listeners only. The public
listener is not opened. The same applies to an empty or malformed
`cap/funnel`: the public surface returns 404 for everything; the tailnet is
unaffected.

This is deliberately different from a hard startup failure. A public add-on
should not take down the private forge. The explicit error log and the absent
public listener are the signal. Operators who want the process to fail can watch
the log or health check the public URL.

### Public listener hardening

The public server is separate from the tailnet server and gets stricter limits:

| Control | Value | Reason |
|---|---|---|
| `ReadHeaderTimeout` | 10s | slow header defense |
| `ReadTimeout` | 60s | bounds request-body reads |
| `IdleTimeout` | 60s | bounds idle keep-alive |
| `WriteTimeout` | none | large clones are legitimate |
| `MaxHeaderBytes` | 1 MiB | cheap ceiling |
| concurrent pack POSTs | 4, flag-configurable | CPU, disk, and Funnel bandwidth |
| request body | 64 MiB | fetch negotiation is small |

The concurrency limiter wraps the public `front` dispatcher for
`POST .../git-upload-pack`. A full limiter returns `503` with `Retry-After`
rather than queueing. `Content-Length` over the body limit is rejected early
with `413`; chunked bodies get `http.MaxBytesReader`. A `ConnContext` callback
unwraps the TLS connection and records `FunnelConn.Src` for access logs; it is
never used for authorization.

### Public discovery is filtered, not a second UI

The public handler reuses the same templates and store with a public `Config`.
The index is filtered by the public principal, the summary uses the same
denied/missing 404, and the empty-repository template suppresses first-push
instructions when the viewer cannot write. The tailnet index gains a public
badge for repositories matched by `cap/funnel`, so exposure is visible where the
operator already looks.

## Risks / Trade-offs

- **A policy typo publishes private code.** `repos: ["*/*"]` is one edit away
  from publishing everything. Mitigations: the payload is read-only, the
  deployment flag is a second key, startup and policy changes are logged loudly,
  and the tailnet index marks public repositories. There is no undo for a clone.
- **Public `upload-pack` is a resource lever.** Mitigations: concurrent-pack
  cap, body limit, read/idle timeouts, and the operator's choice of public
  repositories. Funnel's own limits are a backstop, not a guarantee.
- **Public clients are not identified.** This is a product property, not a bug.
  The operator can restrict *content*, not *audience*. Node sharing or an
  identity provider is the alternative if audience control is needed.
- **`FunnelConn.RemoteAddr` is the relay.** Using `WhoIs` on the public path
  would be wrong. Mitigation: the separate listener never calls `WhoIs`; the
  public resolver ignores the address.
- **Custom `nodeAttrs.app` delivery is an assumption.** The Tailscale docs
  permit application-defined capability names, and `sniproxy` reads a node
  capability this way, but third-party keys should be verified on a real
  tailnet. Fallback if unavailable: a local configuration source, with the
  associated invariant cost.
- **Same-port `ListenTLS` + `ListenFunnel(FunnelOnly)` is undocumented.** The
  listener map supports it, but it deserves a smoke test. Fallback: Funnel on
  8443/10000.
- **Funnel is beta and plan-gated.** Documentation must name the prerequisites:
  MagicDNS, HTTPS certificates, the `funnel` node attribute, and the supported
  ports.
- **Stale serve configuration after a crash.** `ListenFunnel` records
  `AllowFunnel` and removes it on graceful close. A crash can leave the entry;
  a later startup with `--funnel` reuses it, and a startup without `--funnel`
  does not advertise a listener. This is a small operational edge, documented
  rather than guessed at.
- **Public metadata disclosure.** A public summary shows description, branch,
  and latest commit for a public repository. The operator chooses the
  repository, so the metadata is part of that choice.

## Migration Plan

No data migration. Funnel is off by default and every existing tailnet URL,
identity, capability, and push path is unchanged.

To enable:

1. Add the `funnel` attribute and the `nodeAttrs.app` `cap/funnel` payload to the
   tailnet policy for the tsgit node.
2. Verify HTTPS and certificate domains are available.
3. Start the daemon with `--funnel`.
4. Clone a public repository from outside the tailnet and confirm that a
   non-public repository returns 404 and that receive-pack is refused.

Rollback is removing `--funnel` and restarting; the public listener is not
opened and the tailnet continues. Removing the capability alone makes the public
surface empty without a restart.

## Open Questions

None that change the specs or the approach. The two validation spikes
(`nodeAttrs.app` delivery and same-port listener dispatch) are tasks with
explicit fallbacks, not unresolved design choices.
