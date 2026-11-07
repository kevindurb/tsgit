# Design

## Context

The binary is still a stub. The earlier SSH-first change was removed; its
artifacts are useful prior art but no specs remain. This design describes the
first working slice: an embedded `tsnet` node serving git smart HTTP to tailnet
peers, authorized only by Tailscale app capabilities. See `proposal.md` for
motivation and `specs/` for the behavioral contract.

Constraints carried forward:

- Go 1.27, no database, one binary on one tailnet node.
- Repositories are bare git directories under one root.
- `docs/decisions/0001-git-transport-via-plumbing.md` says the pack protocol is
  git's job. This design keeps that decision but puts it behind a replaceable
  backend.
- Identity always comes from the embedded node's LocalAPI; authorization is
  decided before any repository work or subprocess.

## Goals / Non-Goals

**Goals:**

- Prove clone, fetch, push, and push-to-create over smart HTTP with tailnet
  identity and app capabilities as the only authorization source.
- Keep `identity`, `authz`, `repos`, and `transport/http` independently
  testable without a tailnet.
- Make the pack implementation replaceable, with the git binary as the first
  backend.
- Follow the deployment conventions of Tailscale-native apps (golink, tsidp):
  `TS_*` auth settings, explicit state and data paths, `--dev-listen`,
  `--version`, and graceful shutdown.

**Non-Goals:**

- SSH transport, Funnel, Tailscale Service mode, or running against a host
  tailscaled. Each is a separate front end or trust boundary.
- Repository listing, browsing, deletion, ref-level permissions, or a web UI.
- A go-git backend in this slice. The seam exists so it can be added later.
- Caching identity or capabilities. Staleness is worse than a local lookup.
- A permission store. The tailnet policy is the only source.

## Decisions

### Embedded `tsnet` node, not a host tailscaled

The binary owns a `tsnet.Server`, joins the tailnet as its own node, and binds
listeners that are reachable only through the tailnet. No root, no host
Tailscale installation, no socket path to discover. `tsnet` also supplies the
in-process LocalAPI client and optional HTTPS certificates.

Alternative: keep the host `tailscaled` and bind the tailnet interface, as the
removed change did. Rejected because it ties deployment to the host, needs a
second identity path for development, and loses the single-binary property. The
cost is userspace WireGuard throughput; `tsnet.Server.Tun` is the escape hatch
if that ever matters.

### Native smart-HTTP handler, not CGI

The handler owns routing, identity, authorization, repository resolution, and
response framing, then calls `git upload-pack --stateless-rpc` or
`git receive-pack --stateless-rpc`. This keeps one repository resolution: the
path authorization approved is the path the backend opens. `git http-backend`
would resolve the path a second time and add CGI environment plumbing.

The implementation must reproduce a small part of git's smart-HTTP framing:

- For v0/v1, write `# service=git-upload-pack\n` (or receive-pack) as a
  pkt-line, then a flush packet, then the advertisement.
- For `Git-Protocol: version=2`, omit the service line for upload-pack and
  forward the header as `GIT_PROTOCOL`; mirror git's fallback behavior for
  receive-pack.
- Set `application/x-git-<service>-advertisement` and
  `application/x-git-<service>-result`.
- Stream the request body to the subprocess and its stdout to the response;
  flush rather than buffering. Handle gzip request bodies.
- `GET info/refs?service=git-receive-pack` is a write operation; it advertises
  a push and is where creation happens.

Alternative: shell out to `git http-backend` via `net/http/cgi`. Lower protocol
risk, but re-resolves the repository and makes authorization/creation harder to
keep in one place. The framing work above is small and testable against real
git.

### Replaceable pack backend

The handler never invokes git directly. It calls:

```go
type Request struct {
    RepoPath    string
    Service     string // git-upload-pack | git-receive-pack
    GitProtocol string
    Advertise   bool
    Stateless   bool
    Body        io.Reader
    Out         io.Writer
}

type Backend interface {
    Serve(ctx context.Context, req Request) error
}
```

`gitbin` execs `git <service> --stateless-rpc [--advertise-refs] <repo>` with
`GIT_PROTOCOL` set from the request header. A future `gogit` backend can use
go-git's `transport.UploadPack` / `ReceivePack` without touching identity,
authorization, HTTP routing, or repository resolution.

This is the seam that keeps ADR 0001 honest: the decision to use git plumbing is
the first implementation, not a permanent coupling. go-git v6 is not used now
because it is beta and its receive-pack path is still missing `atomic`; the
existing git binary is the reference implementation for the flow this slice is
trying to prove.

### Identity from `WhoIs` via the embedded LocalAPI

`tsnet.Server.LocalClient()` returns a `*local.Client`. For each request:

```go
who, err := lc.WhoIs(ctx, r.RemoteAddr)
```

`apitype.WhoIsResponse` carries `Node`, `UserProfile`, and `CapMap`. The
capability is decoded with `tailcfg.UnmarshalCapJSON[repoGrant](who.CapMap,
capName)`. `r.RemoteAddr` is authoritative; headers are never consulted.

Tagged nodes are first-class. `UserProfile` may be absent or synthetic, so the
identity value makes `Login` optional and authorization never depends on it.
This deliberately differs from the common tsnet example that rejects
`who.Node.IsTagged()`; CI machines are the main case where a capability, not a
user, is the identity.

`internal/authz` owns the plain types (`Identity`, `Grant`, `Access`, `Op`,
`Decision`) and the pure decision. `internal/identity` only turns a `WhoIs`
response into that value, so `authz` never imports tsnet or a connection type.

### Capability contract

Capability name: `github.com/kevindurb/tsgit/cap/repo`. It is a public contract
and must be fixed before anyone writes policy. If the project has a controlled
domain, prefer a name under it; the current name is recognizable but is not a
DNS domain the project owns.

Payload: an array of objects, each granting `access` (`read` or `write`) over
repository patterns:

```json
"grants": [{
  "src": ["group:devs", "tag:ci"],
  "dst": ["tag:tsgit"],
  "ip": ["tcp:443"],
  "app": {
    "github.com/kevindurb/tsgit/cap/repo": [
      {"repos": ["acme/*"],      "access": "write"},
      {"repos": ["shared/docs"], "access": "read"}
    ]
  }
}]
```

Both `ip` and `app` are required in practice: app capabilities only apply when
the device also has network access to the destination. An `app`-only grant
produces a capability nobody can reach.

Matching rules:

- Canonical name is `<namespace>/<repo>`, two segments for this slice. The URL's
  single trailing `.git` is not part of the name.
- Patterns use `path.Match`; `*` does not cross `/`.
- Grants union; the highest level wins; `write` implies `read` and creation.
- No match denies. A malformed entry denies and is logged; `UnmarshalCapJSON`
  failing the array means the whole capability grants nothing.

Alternative: a local YAML policy file. Rejected again: it would be a second
authorization source, cannot express tailnet groups, and splits the answer to
"why can this peer read this repo?" across two places.

### Storage layout

```
$TSGIT_STATE_DIR/               0700, node key + state, not in repo backups
  tailscaled.state

$TSGIT_REPO_ROOT/               0750, the durable artifact
  .tsgit-tmp/                   staging on the same filesystem
  acme/
    widget.git/
      HEAD                      -> refs/heads/main
      description               git's own file
      objects/
      refs/
```

The two trees are separate because the tsnet state is a secret tied to this
node, while the repositories are what gets backed up. `git init --bare` is the
only metadata writer; default branch and description use git's own files. No
custom metadata store is introduced.

Creation is atomic:

1. Validate the namespace and repository segments syntactically.
2. Ensure the namespace directory exists.
3. `mkdir` a staging directory under `$REPO_ROOT/.tsgit-tmp`.
4. `git init --bare --initial-branch=main` inside it.
5. Rename the bare directory to its final name. `EEXIST`/`ENOTEMPTY` means a
   concurrent creator won; use the existing repository and clean up.
6. Readers never see a half-initialized bare directory.

### HTTP route to operation mapping

| Method | Path | Operation |
|---|---|---|
| GET | `/<ns>/<repo>.git/info/refs?service=git-upload-pack` | read |
| GET | `/<ns>/<repo>.git/info/refs?service=git-receive-pack` | write + create |
| POST | `/<ns>/<repo>.git/git-upload-pack` | read |
| POST | `/<ns>/<repo>.git/git-receive-pack` | write + create |

Everything else is refused before a backend starts. The handler:

1. Resolve identity (failure is a logged 503; no fallback).
2. Parse the route into canonical name, service, and operation.
3. `authz.Decide`.
4. Resolve the repository; on receive-pack, create it if missing and allowed.
5. Call `Backend.Serve`.

A denied request and a missing repository both return 404, so the response is
not an existence oracle. Distinct reasons are logged server-side.

### Configuration and data conventions

Follow golink and tsidp: stdlib `flag` with environment fallbacks, flags win.

| Flag | Environment | Default |
|---|---|---|
| `--hostname` | `TSGIT_HOSTNAME` (fallback `TS_HOSTNAME`) | `tsgit` |
| `--port` | `TSGIT_PORT` | `443` |
| `--http-port` | `TSGIT_HTTP_PORT` | `80` |
| `--state-dir` | `TSGIT_STATE_DIR` (fallback `TS_STATE_DIR`) | `os.UserConfigDir()/tsnet-tsgit` |
| `--repo-root` | `TSGIT_REPO_ROOT` | required; dev mode supplies a temp dir |
| `--advertise-tags` | `TS_ADVERTISE_TAGS` | empty |
| `--authkey-file` | `TS_AUTHKEY_FILE` | empty; mutually exclusive with `TS_AUTHKEY` |
| `--control-url` | `TS_CONTROL_URL` | `ipn.DefaultControlURL` |
| `--https` | `TSGIT_HTTPS` | `true` |
| `--default-branch` | `TSGIT_DEFAULT_BRANCH` | `main` |
| `--dev-listen` | `TSGIT_DEV_LISTEN` | empty |
| `--log` | `TSGIT_LOG` | `info` |
| `--verbose`, `--debug-tsnet` | | false |
| `--version` | | false |

`hostinfo.SetApp("tsgit")` is set before starting tsnet. `--authkey-file` and
`TS_AUTHKEY` are mutually exclusive; a missing file is a warning if the node is
already registered, matching tsidp.

HTTPS follows golink: enable it only when `--https` is set,
`status.Self.HasCap(tailcfg.CapabilityHTTPS)` is true, and `CertDomains()` is
non-empty. Then serve TLS on `--port` with HSTS and redirect HTTP on
`--http-port`. Otherwise serve plain HTTP on `--http-port` (and log that HTTPS
is unavailable). WireGuard already encrypts tailnet traffic; TLS is for a
trusted hostname and client ergonomics.

`--dev-listen` skips tsnet, listens on localhost, creates a temporary repository
root, and uses a development identity with write access to `*/*`. It exists so
clone, push, and push-to-create can be exercised without a tailnet. It is
logged as development-only and is not a production fallback.

Shutdown follows golink: `signal.NotifyContext` plus `http.Server.Shutdown` for
every listener, then `tsnet.Server.Close`.

### Deliberate omissions

- **No Funnel.** Public internet access contradicts the project's scope.
- **No Tailscale Service mode.** App capabilities targeting `svc:` are
  currently broken upstream, and service mode adds a loopback + injected-header
  trust path.
- **No local-tailscaled mode.** tsidp marks it development-only and it switches
  identity to a forwarded address. That is the client-controlled identity the
  project forbids.
- **No go-git yet.** v6 is beta, its ready-made HTTP handler requires an
  `Authorization` header for receive-pack, and `atomic` push is still missing.
  The backend seam makes this a later, measured decision.

## Risks / Trade-offs

- **Userspace networking is slower than kernel WireGuard** -> acceptable at
  tailnet scale; `tsnet.Server.Tun` is available if throughput becomes a
  problem.
- **Losing the state directory re-registers the node** -> ACLs that name the old
  node break. Document the state directory as persistent and keep it out of
  repo backups.
- **tsnet uploads logs to Tailscale by default** -> document the log
  configuration location and how to disable it for privacy-sensitive
  deployments.
- **Push-to-create lets any writer fill the disk** -> capability globs are the
  current limit; a quota is a later change.
- **The capability name and payload are a public contract** -> fix them before
  implementation and document a complete grant, including `ip`.
- **Access decisions outlive the request** -> a long fetch keeps its decision
  if policy changes; already accepted by ADR 0001 and bounded in practice.
- **Two requests per operation** -> each does its own `WhoIs`; at tailnet scale
  this is negligible, and caching would introduce staleness.
- **`git` remains a runtime dependency** -> the backend seam makes it
  replaceable; the container image must include git, unlike the distroless
  reference apps.
- **Default branch mismatch** -> repositories are initialized with `main`; a
  first push of `master` leaves HEAD pointing at a missing ref. Document it and
  keep `--default-branch` configurable.
- **App capabilities targeting services are broken** -> no service mode until
  upstream supports it.

## Migration Plan

This is the first serving slice, so there is no data migration.

- Create `$TSGIT_STATE_DIR` and `$TSGIT_REPO_ROOT`; the repository root is plain
  bare git directories, readable by any git client.
- Rollback is stopping the daemon and reverting the binary; the repository tree
  needs no conversion.
- Update `docs/architecture.md` to the HTTP-first/tsnet shape and note in
  `docs/project.md` that SSH is deferred. ADR 0001 remains valid; the backend
  seam is the documented escape hatch.

## Open Questions

- Whether to add a JSON repository-listing endpoint after this slice. It does
  not change the specs or the plan here.
- Whether to support nested namespaces (`team/sub/repo`). The current matcher
  and two-segment parser would need to change; deferred.
- Whether a quota or maximum repository size is needed. Deferred; the risk is
  named above.
