# Tasks

## 1. Container image

- [x] 1.1 Add a container ignore file that excludes `.git`, `bin/`, `.agents/`,
  `.opencode/`, `openspec/`, docs, and local secret files from the build
  context; verify a local image build succeeds from a checkout that contains
  those directories.
- [x] 1.2 Add `Containerfile` with a Go builder stage that cross-compiles for
  `TARGETOS`/`TARGETARCH`, accepts `VERSION` and passes
  `-X main.version=$VERSION`, and a pinned Alpine runtime stage that installs
  `git` and `ca-certificates`; verify `podman build --build-arg
  VERSION=v0.0.0-test -t tsgit:test .` succeeds.
- [x] 1.3 Create the fixed `tsgit` user and group at UID/GID 10001, create
  `/var/lib/tsgit/state` (0700) and `/var/lib/tsgit/repos` (0750) owned by it,
  set `TSGIT_STATE_DIR`, `TSGIT_REPO_ROOT`, `HOME`, and `TMPDIR`, declare both
  data paths as volumes, and use an exec-form entrypoint with `serve` as the
  default command; verify `podman run --rm tsgit:test version` prints
  `v0.0.0-test` and `podman run --rm --entrypoint id tsgit:test -u` prints
  `10001`.
- [x] 1.4 Verify the runtime dependencies and secret hygiene: `podman run --rm
  --entrypoint git tsgit:test --version` succeeds, `podman inspect` shows no
  auth key in the environment, and `podman run --rm --entrypoint sh tsgit:test
  -c 'test ! -e /var/lib/tsgit/state/tailscaled.state'` succeeds.
- [x] 1.5 Verify the arm64 build path: build with the Containerfile for
  `linux/arm64` on a host with binfmt registered, and confirm the resulting
  image architecture reports `arm64`; if the local host cannot run arm64,
  record that CI's publish build is the verification point instead.
- [x] 1.6 Write the runtime sections of `docs/deployment.md`: pull command,
  named volumes, UID/GID ownership for bind mounts, auth key via environment or
  file, no published host ports in tailnet mode, `-p 8080:8080` for
  `--dev-listen`, and the volume/restart behavior; verify each command block
  runs as written against the local image.

## 2. Image workflow and publishing

- [x] 2.1 Add `.github/workflows/container.yml` with `pull_request`, `main`,
  and `v*` tag triggers, top-level `contents: read`, and a `test` job that runs
  `just ci`; verify `go run github.com/rhysd/actionlint/cmd/actionlint@latest
  .github/workflows/container.yml` passes.
- [x] 2.2 Add the image job with `needs: test`, runner setup for
  `podman`/`buildah` and binfmt, and a pull-request path that builds only
  `linux/amd64` and smoke-tests `version` and `git --version`; verify
  `actionlint` passes and the workflow graph shows no login or push step
  reachable for `pull_request` events.
- [x] 2.3 Implement tag computation and publication: `main` publishes `main`
  and `sha-<12>`; a validated `vX.Y.Z` tag publishes the exact tag and
  `latest`; only those refs log in and push. Derive the image reference from
  `${GITHUB_REPOSITORY}` lowercased. Verify the tag decision block with sample
  refs (`refs/heads/main`, `refs/tags/v1.2.3`, `refs/pull/1/merge`) produces the
  documented table and that an invalid `v*` tag does not publish.
- [x] 2.4 Build and push a multi-architecture manifest for `linux/amd64` and
  `linux/arm64` using the podman/buildah actions on publish refs; verify the
  push step references the manifest and `actionlint` reports no errors.
- [x] 2.5 Confirm credentials are least-privilege: publish steps use only
  `secrets.GITHUB_TOKEN` with `packages: write`, no personal access token or
  repository secret is referenced, and fork pull requests cannot reach the
  login or push steps; verify with a grep for token/registry inputs and a
  review of the `if` conditions.
- [x] 2.6 Document the publishing contract in `docs/deployment.md`: triggers,
  tag table, GHCR package name, package visibility/linking as a GitHub setting,
  and how to pin a release tag; verify the documented table matches the
  workflow conditions and add a link from the README container section.

## 3. End-to-end verification

- [x] 3.1 Run `just ci` and confirm formatting, lint, build, and tests pass.
- [x] 3.2 Run the image with `serve --dev-listen 0.0.0.0:8080 -p 8080:8080`,
  then clone, push, and push-to-create against it from the host with a real git
  client; verify each operation succeeds and the repository survives a
  container restart with the same volumes.
- [x] 3.3 Verify the ownership failure path: start the container as a UID that
  cannot write the mounted repository root and confirm startup fails with an
  error naming that path rather than serving partially.
- [x] 3.4 Run `just spec-validate` and confirm every change spec validates.
- [ ] 3.5 After merge, confirm GHCR publishes `main` and `sha-<12>` for a main
  commit and the exact tag plus `latest` for a `vX.Y.Z` tag; pull the published
  tag on both architectures if available and confirm `version` reports the
  injected version.
- [ ] 3.6 Archive the change with the archive workflow and sync the two new
  capabilities to `openspec/specs/`.
