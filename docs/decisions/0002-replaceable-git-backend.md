# 2. Keep the git backend replaceable

## Status

Accepted.

## Context

ADR 0001 says `tsgit` shells out to `git upload-pack` and `git receive-pack`
rather than implementing the pack protocol. That remains the right default: the
git binary is the reference implementation, and it gives protocol v2, partial
clone, and every other negotiated feature for free.

But ADR 0001 does not say the subprocess must be reached from every layer of the
server. If HTTP routing, identity, authorization, and repository resolution all
called `exec.Command` directly, replacing the pack implementation would mean
rewriting all of them. The protocol is large and the implementation strategy is
the part most likely to change.

## Decision

`internal/transport` hands a pack exchange to a `Backend` interface:

```go
type Backend interface {
    Serve(ctx context.Context, req Request) error
}
```

The handler owns routing, framing, identity, authorization, and repository
resolution. It passes the resolved repository path, service, protocol header,
request body, and output writer to the backend. It never invokes git itself.

The first backend, `GitBin`, execs
`git <service> --stateless-rpc [--advertise-refs] <repo>` and streams the
exchange. ADR 0001 still governs this default.

## Consequences

- A future `gogit` backend can use go-git's `transport.UploadPack` and
  `ReceivePack` without changing identity, authorization, routing, or
  repository resolution.
- The seam was chosen now, before a second backend exists, because it is cheap
  to add and expensive to retrofit. It is not speculative abstraction over a
  volatile API; it isolates one large, replaceable subsystem.
- go-git v6 is not used yet: it is still beta, its ready-made HTTP handler
  requires an `Authorization` header for receive-pack, and its `atomic` push
  support is incomplete. The git binary remains the measured choice.
- Tests can substitute a fake backend and assert that a denied request never
  starts one.
- `git` remains a runtime dependency of the default build. The interface makes
  that a deployment choice rather than an architectural commitment.
