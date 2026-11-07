# Tasks

## 1. Dependencies, configuration, and entry point

- [ ] 1.1 Add `tailscale.com` to `go.mod` and verify `go build ./...` succeeds
  with `tsnet`, `tailcfg`, `client/local`, and `envknob` imported.
- [ ] 1.2 Add `internal/config` with the flag/environment table from
  `design.md`, app-prefixed names with generic `TS_*` fallbacks, and validation
  that `--repo-root` is required unless `--dev-listen` is set; verify table
  tests cover flag-over-environment precedence and the required-root error.
- [ ] 1.3 Add `serve` and `version` handling to `cmd/tsgit`: `serve` loads
  config, `--version` prints the version, and an unknown command exits non-zero;
  verify `go run ./cmd/tsgit --version` and a deliberate config-error run.
- [ ] 1.4 Implement the `--dev-listen` configuration path: a temporary
  repository root and a development identity, with no tailnet contact; verify a
  unit test asserts the temp root is created and tsnet is not started.
- [ ] 1.5 Write `docs/configuration.md` with flags, environment variables,
  defaults, and the state/repository data layout; verify every documented flag
  is accepted by a `config` parsing test.

## 2. Authorization

- [ ] 2.1 Define `internal/authz` types: `Identity` (node, optional login, tags,
  grants), `Grant` (patterns, access), `Access`, `Op`, and `Decision`; verify the
  package compiles without importing `net/http` or `tailscale.com`.
- [ ] 2.2 Implement `Decide(Identity, canonicalRepo, Op) Decision` with
  deny-by-default, grant union, highest-level-wins, `write` implying `read`, and
  `path.Match` patterns that do not cross `/`; verify table tests cover each
  case.
- [ ] 2.3 Add creation authorization: `write` matching the canonical name
  allows creation, `read` does not, and a non-matching pattern does not; verify
  table tests.
- [ ] 2.4 Add malformed-payload behavior: an undecodable entry or unknown
  `access` value grants nothing and is logged; verify tests assert denial with
  no partial grant.
- [ ] 2.5 Write `docs/access-control.md` documenting
  `github.com/kevindurb/tsgit/cap/repo`, the payload schema, canonical names,
  and a complete grant with both `ip` and `app`; verify the documented JSON is
  used as a parser fixture in a test.

## 3. Repository store

- [ ] 3.1 Add `internal/repos` with strict `<namespace>/<repo>` parsing, single
  trailing `.git` stripping, and rejection of `.`/`..`, empty, hidden, absolute,
  backslash, or control-character segments; verify table tests.
- [ ] 3.2 Implement containment resolution under the repository root, including
  symlink evaluation for existing parents; verify traversal and symlink-escape
  tests are refused.
- [ ] 3.3 Implement atomic creation: stage under `<root>/.tsgit-tmp`,
  `git init --bare --initial-branch=<default>`, rename into place, and treat an
  existing non-empty target as a concurrent winner; verify concurrent-create and
  no-partial-visibility tests.
- [ ] 3.4 Add a startup check that the repository root exists or can be created
  and is writable, and that `git` is available; verify missing-root,
  read-only-root, and missing-git cases each produce an explicit error.
- [ ] 3.5 Document the storage layout in `docs/architecture.md` (state directory
  vs repository root, `.tsgit-tmp`, bare layout); verify the documented tree
  matches an integration fixture.

## 4. Identity

- [ ] 4.1 Add `internal/identity` with a `Resolver` interface and a
  tsnet-backed implementation that calls
  `LocalClient().WhoIs(ctx, remoteAddr)`; verify a constructed `WhoIsResponse`
  resolves to the expected `authz.Identity`.
- [ ] 4.2 Decode the repository capability with `tailcfg.UnmarshalCapJSON` into
  `authz.Grant`, preserve optional login and tags, and treat a missing
  capability as empty; verify tests for no capability, malformed capability, and
  a tagged node with no login.
- [ ] 4.3 Ensure the resolver uses only the peer address and never request
  headers; verify a handler-level test with spoofed `X-Forwarded-For` and
  `Tailscale-User-Login` still uses the resolved identity.
- [ ] 4.4 Document identity resolution and tagged-node behavior in
  `docs/architecture.md`; verify the section names the LocalAPI call and the
  no-header rule.

## 5. HTTP transport and git backend

- [ ] 5.1 Define the `transport.Backend` interface and `Request` type from
  `design.md`; verify the package compiles with a fake backend in a test.
- [ ] 5.2 Implement the `gitbin` backend: exec
  `git <service> --stateless-rpc [--advertise-refs] <repo>`, set
  `GIT_PROTOCOL`, stream stdin/stdout, and log stderr; verify fake-git and
  real-git tests.
- [ ] 5.3 Implement route parsing and operation mapping for the four smart-HTTP
  endpoints, rejecting other methods, services, and paths before any backend
  call; verify table tests.
- [ ] 5.4 Implement response framing: service pkt-line for v0/v1, v2 handling,
  content types, no-cache headers, gzip request bodies, and streaming; verify a
  real `git clone` and `git fetch` against `httptest`.
- [ ] 5.5 Wire identity, authorization, repository resolution, and
  push-to-create into the handler in the order from `design.md`; verify the fake
  backend is not called on deny and is called on allow.
- [ ] 5.6 Add integration tests with a real git client: clone, fetch, push,
  push-to-create, concurrent first push, protocol v2, and traversal; verify each
  runs against `httptest` with fixture bare repositories.
- [ ] 5.7 Implement uniform 404 for missing and denied repositories, 503 for
  identity lookup failure, and 500 for backend failure before headers; verify
  status-code tests.
- [ ] 5.8 Document the endpoints, status codes, and backend seam in
  `docs/architecture.md`; verify the documented routes match the handler table
  test.

## 6. tsnet daemon and listeners

- [ ] 6.1 Wire `serve` to start `tsnet.Server` with hostname, state directory,
  auth key or key file, advertise tags, control URL, and
  `hostinfo.SetApp("tsgit")`; verify startup with a documented manual run.
- [ ] 6.2 Wire the LocalAPI resolver into the handler; verify a request from a
  second tsnet node resolves its identity and capabilities.
- [ ] 6.3 Implement HTTPS detection (`CapabilityHTTPS` plus `CertDomains`), the
  TLS listener, HTTP redirect, and HSTS, with a plain-HTTP fallback; verify unit
  tests for the enable/disable decision and manual `git` over both.
- [ ] 6.4 Implement graceful shutdown for every listener and the tsnet server;
  verify a signal test drains an in-flight request.
- [ ] 6.5 Implement `--dev-listen`: localhost listener, temporary repository
  root, development identity; verify `git clone` and `git push` work without a
  tailnet.
- [ ] 6.6 Update `docs/project.md` and `docs/architecture.md` for the
  HTTP-first, tsnet-embedded shape and note that SSH is deferred; verify a
  read-through and `just spec-validate`.
- [ ] 6.7 Add `docs/decisions/0002-replaceable-git-backend.md` recording why the
  git binary is the first backend, the go-git v6 beta trade-off, and how a
  future backend swaps behind the interface; verify the record references
  ADR 0001 as still governing the default.

## 7. Integration and archive

- [ ] 7.1 Run `just ci` and `just spec-validate`; verify both pass.
- [ ] 7.2 Manual tailnet verification: add a grant with
  `ip: ["tcp:443"]` and the repository capability, clone and push from a second
  node, then remove the grant and confirm denial; record the result.
- [ ] 7.3 Confirm the documented capability grant and the HTTP endpoint table
  match the implementation and the specs; verify with `just spec-validate` and a
  final review pass.
- [ ] 7.4 Archive the change and sync specs to `openspec/specs/`.
