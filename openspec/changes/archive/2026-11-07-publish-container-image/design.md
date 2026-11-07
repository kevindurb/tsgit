# Design

## Context

See `proposal.md` for motivation and `specs/` for the behavioral contract. The
current state that shapes this design:

- `cmd/tsgit` already has a `version` variable that `-ldflags -X` can set, and a
  `serve` command whose defaults are `--port 443`, `--http-port 80`, and
  `--https true`.
- `repos.Open` already creates the repository root and fails with a path-named
  error when it is not writable; it also checks that `git` is on `PATH`.
- `tsnet` runs userspace WireGuard and its listeners live inside the netstack,
  so a container does not bind the host's 80/443 and needs no `/dev/net/tun`,
  no `NET_ADMIN`, and no published ports in tailnet mode.
- CI is a single workflow running `just ci` on `ubuntu-latest`; there is no
  release or registry step today.
- The remote is `github.com/kevindurb/tsgit`, so the image belongs at
  `ghcr.io/kevindurb/tsgit`.

## Goals / Non-Goals

**Goals:**

- One container definition that works locally with `podman build` and in CI
  with the Red Hat podman/buildah actions.
- A runtime image small enough to pull on a tailnet node, with `git` and CA
  certificates present and a fixed non-root identity.
- An image workflow that is safe for fork pull requests and publishes only from
  `main` and semver tags.
- Reuse the existing `just ci` contract as the publication gate.

**Non-Goals:**

- Docker Hub, quay.io, or any registry other than GHCR.
- Image signing, SBOMs, provenance attestations, or vulnerability scanning.
- Kubernetes manifests, Helm charts, Compose files, or automatic deployment.
- A distroless or static-`git` build; that trades simplicity for size before
  there is a reason.
- Changing the application's flags, data layout, or serving behavior.

## Decisions

### Multi-stage Alpine image, not distroless

The builder stage is `golang:1.27.3-alpine`; the runtime stage is
`alpine:3.24` with `git` and `ca-certificates` from apk. Alpine is the smallest
base that packages a usable `git` with a maintained CA bundle, supports both
target architectures, and keeps the build readable.

Alternatives considered:

- **Distroless/static with a copied `git`**: smallest image, but `git` is
  dynamically linked against libc and its helpers; copying it plus its shared
  libraries is fragile and the image would still need CA certificates. The
  project already accepts `git` as a runtime dependency, so packaging it is not
  a regression.
- **Debian slim**: better glibc compatibility and a larger image; nothing here
  needs glibc, and the extra pull size is paid on every node.
- **Scratch with a fully static git**: no maintained static `git` build exists
  for this use; building one adds a second build system to the project.

Pin the Go and Alpine tags and bump them deliberately. `go.mod` requires Go
1.27.3, so the builder tag must not float below that patch.

The builder cross-compiles from the build platform:

```
FROM --platform=$BUILDPLATFORM golang:1.27.3-alpine AS build
ARG TARGETOS TARGETARCH
ARG VERSION=dev
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath -buildvcs=false \
    -ldflags "-s -w -X main.version=$VERSION" \
    -o /out/tsgit ./cmd/tsgit
```

Only the final Alpine stage is architecture-specific, so multi-arch builds do
not emulate the Go compiler. The build context copies `go.mod`, `go.sum`,
`cmd/`, and `internal/`; a container ignore file keeps `.git`, `bin/`, docs,
and OpenSpec artifacts out of the context. `-buildvcs=false` keeps the build
reproducible when the context has no `.git`.

### Runtime contract: fixed UID 10001, two volumes, exec-form entrypoint

```
/usr/local/bin/tsgit        binary
/var/lib/tsgit/state        tsnet state, 0700, UID 10001
/var/lib/tsgit/repos        bare repositories, 0750, UID 10001
```

- `USER 10001:10001`, created in the image as `tsgit`.
- `TSGIT_STATE_DIR=/var/lib/tsgit/state` and
  `TSGIT_REPO_ROOT=/var/lib/tsgit/repos`, so the required `--repo-root` is
  satisfied with no arguments.
- `VOLUME ["/var/lib/tsgit/state", "/var/lib/tsgit/repos"]`.
- `ENTRYPOINT ["/usr/local/bin/tsgit"]`, `CMD ["serve"]` in exec form. `podman
  run <image> version` therefore runs the version command, and SIGTERM reaches
  the process directly as PID 1; Go's signal handling provides graceful
  shutdown.
- `HOME=/var/lib/tsgit` and a writable `TMPDIR` are set so `os.UserConfigDir`
  and temporary files never depend on a missing home directory.

`EXPOSE` is deliberately omitted for tailnet mode: the tsnet listeners are
inside the userspace network stack, so publishing host ports would imply a
reachability model the daemon does not use. Development mode is the only case
that needs a published port (`-p 8080:8080` for `serve --dev-listen
0.0.0.0:8080`), and that is documented with the run command rather than the
image metadata.

Named volumes inherit the image directories' ownership when first created.
Bind mounts do not, so documentation must require `chown 10001:10001` on host
directories (or `podman unshare chown` for rootless bind mounts). The existing
`repos.Open` check turns a mis-owned repository root into a clear startup
error instead of a half-working service.

The image contains no `TS_AUTHKEY`, no `tailscaled.state`, and no repository
content. Operators supply `TS_AUTHKEY` or mount an auth key file and pass
`--authkey-file`, matching the existing configuration table.

### Version injection from the workflow, not from git inside the image

The image build accepts `VERSION` as a build argument and passes it to
`-X main.version`. CI computes it:

- `main` push: `sha-<12-character commit SHA>`
- semver tag push: the tag name, e.g. `v1.2.3`
- pull request: `pr-<12-character commit SHA>` (smoke test only)

Injecting from CI avoids copying `.git` into the build context and keeps the
image reproducible. A local build without `--build-arg VERSION=...` reports
`dev`, which is the current development behavior.

### Separate `container.yml` workflow with a test gate job

The existing `.github/workflows/ci.yml` stays as-is. A new
`.github/workflows/container.yml` runs on:

- `pull_request`
- `push` to `main`
- `push` of tags matching a semver pattern (`v*`, validated in a step)

It has two jobs:

1. `test` — the same `just ci` contract, so an image can never be published
   from a commit that fails fmt, lint, build, or test.
2. `image` — `needs: test`; builds, smoke-tests, and conditionally publishes.

Alternatives considered:

- **Add an `image` job to `ci.yml`**: fewer files, but the spec requires the
  image workflow to be independent, and a registry failure would mark the test
  workflow failed.
- **Trigger the image workflow with `workflow_run` after CI**: avoids duplicate
  tests but couples to a second workflow's completion event, loses the PR
  context, and makes the publish condition harder to audit.
- **Publish without running tests in this workflow**: relies on branch
  protection and leaves a window where a direct push or a flaky prior run
  publishes untested code.

The duplicate `just ci` run on `main` and PRs is accepted: the image workflow
must be able to prove its own gate, and the runtime is small.

### Podman and Buildah actions, QEMU only for the runtime stage

The workflow uses the Red Hat actions:

- `redhat-actions/podman-login@v1` for GHCR login
- `redhat-actions/buildah-build@v2` for the build
- `redhat-actions/push-to-registry@v2` for the push

Buildah is daemonless and runs rootless, which matches the project's
single-binary, no-daemon deployment model better than requiring a Docker
daemon. The runner installs `podman`, `buildah`, and `qemu-user-static` (or
uses `docker/setup-qemu-action` for binfmt registration only) before the build.

Pull requests build only `linux/amd64` for fast feedback. Publication builds
`linux/amd64` and `linux/arm64` into one manifest list. The Go stage
cross-compiles, so only the Alpine runtime stage of the arm64 image runs under
emulation.

The image reference is `ghcr.io/${GITHUB_REPOSITORY}` lowercased with shell
parameter expansion, because GHCR requires lowercase repository names and
GitHub logins may contain uppercase characters.

### Tags: moving `main` and immutable commit tags, exact release tags and `latest`

| Trigger | Tags pushed |
|---|---|
| push to `main` | `main`, `sha-<12>` |
| push of `vX.Y.Z` | `vX.Y.Z`, `latest` |
| pull request | none (local `pr-<12>` for the smoke test) |
| any other ref | none |

`latest` moves only on a release tag, so it never points at unreleased `main`
code. Floating `vX` and `vX.Y` tags are omitted: an operator can pin the exact
tag, and adding aliases later is a non-breaking workflow change. The semver
pattern is validated in a shell step; a `v*` tag that does not match builds and
smoke-tests but does not publish, so a mistaken tag cannot overwrite `latest`.

### Credentials and permissions

The workflow sets `contents: read` at the top level. The `image` job adds
`packages: write`, which is the minimum needed to push to GHCR, and uses
`${{ secrets.GITHUB_TOKEN }}` only. No personal access token, no repository
secret, and no registry password is stored. Login and push steps are guarded
by `if: github.event_name != 'pull_request'` plus the ref check, so fork pull
requests never present a write credential and never attempt a push.

The first publish creates the GHCR package. Documentation must note that the
package visibility and its link to the repository are GitHub settings outside
the workflow; a private package is the safe default for this project.

### Smoke test is the runtime contract in miniature

After the build, CI runs the image on the runner's native architecture:

```
podman run --rm <image> version
podman run --rm --entrypoint git <image> --version
```

The first command proves the binary starts and the injected version is present;
the second proves the git backend is packaged. A failure stops the workflow
before the push step. No tailnet, auth key, or repository volume is needed for
this test, so it is safe on fork pull requests.

### Documentation lives in `docs/deployment.md`

Add a deployment page covering: pull command, named volume creation, auth key
via environment or mounted file, required UID/GID ownership for bind mounts, the
fact that tailnet mode needs no published host ports, the dev-mode `-p` example,
and the tag scheme. The README gets a short container path that links to it.
`docs/configuration.md` remains the source of truth for flags and environment
variables; the deployment page links there instead of duplicating the table.

## Risks / Trade-offs

- **Alpine's musl libc differs from the host** → the Go binary is built with
  `CGO_ENABLED=0`, so only `git` is musl-linked; git is tested by the smoke
  test and by repository creation during serving. A distroless/glibc switch
  would be a later change if a glibc-only dependency appears.
- **Multi-arch builds are slower and depend on QEMU for the runtime stage** →
  cross-compile Go natively, build only `amd64` on pull requests, and keep the
  image's apk install minimal. A failed arm64 emulation is visible before
  publish.
- **UID 10001 collides with nothing in the image but can surprise bind-mount
  users** → document the UID/GID and the `chown`/`podman unshare` commands; the
  existing repository-root writability check produces a clear error.
- **The `test` job duplicates `just ci` on `main` and pull requests** → accepted
  for an independently auditable publish gate; the job uses the same `just ci`
  entry point, so it cannot drift from the project contract.
- **`GITHUB_TOKEN` package permissions can differ for forks and organization
  policies** → PRs never push; publish runs use only `packages: write`; the
  workflow documents that an org policy blocking package writes is an
  environment problem, not a code workaround.
- **`latest` is mutable and can be pulled by accident** → it moves only from a
  semver tag, and the deployment docs recommend pinning the exact tag.
- **Registry outage blocks releases** → the test workflow remains independent;
  a failed image workflow can be re-run without re-running or masking CI.
- **Image build context can leak local files** → explicit `COPY` of
  `go.mod`/`go.sum`/`cmd`/`internal` plus a container ignore file; no `.git`,
  `.env`, `bin/`, or `.agents` content.
- **A local build without a version argument reports `dev`** → that is the
  existing development behavior; release builds always pass `VERSION` from the
  workflow.

## Migration Plan

There is no runtime data migration and no application change.

1. Land `Containerfile`, the container ignore file, the workflow, and the
   deployment docs.
2. Build locally with `podman build -t tsgit:dev .` and run the smoke commands;
   run `serve --dev-listen` with `-p` to confirm the image serves a repository.
3. Push to `main` to publish `main` and `sha-<12>`; push a `vX.Y.Z` tag to
   publish the exact tag and `latest`.
4. Rollback is deleting the workflow and the Containerfile; published tags
   remain in GHCR and can be deleted independently. No repository data or node
   state changes.

## Open Questions

- Whether to publish floating `vX` and `vX.Y` tags later. The exact tag and
  `latest` cover current needs, and adding aliases does not change the specs.
- Whether to add image signing or SBOMs once a dependency policy exists. It is
  deliberately out of scope now.
- Whether to cache Buildah layers across runs. The runner is ephemeral and the
  project is small; a cache can be added without changing the image contract.
