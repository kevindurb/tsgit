# Proposal

## Why

`tsgit` is a single Go binary whose deployment story is still "build it with
`just` and copy it somewhere." A container image is the shortest path from a
tagged commit to a running tailnet node: it packages the binary together with
the one runtime dependency it cannot avoid (`git`) and the CA certificates
needed to reach the control plane. Without a published image, every operator
has to reproduce that environment by hand and pin a commit.

The project also has no release artifact. CI proves the tree builds and tests,
but nothing leaves the repository. Publishing an image to GHCR on `main` and on
version tags gives the project an installable result and gives the existing
`version` command something meaningful to report.

## What Changes

- Add a `Containerfile` that builds `tsgit` in a Go builder stage and produces a
  small runtime image containing the binary, `git`, and CA certificates.
- Define the image runtime contract: a dedicated non-root user, separate
  persistent directories for tsnet state and repositories exposed as volumes,
  `--repo-root`/`--state-dir` wired to those paths, ports 80/443 declared, and
  `tsgit serve` as the entrypoint. The image supports `podman run <image>
  version` and `podman run <image> serve --dev-listen ...` for inspection.
- Inject the build version into the binary so a published image reports its tag
  or commit instead of `dev`.
- Add a GitHub Actions workflow that builds the image with podman/buildah,
  smoke-tests the `version` entrypoint, and publishes to GHCR using the
  workflow's `GITHUB_TOKEN`.
- Publish tags on pushes to `main` and on semver tags; pull requests build and
  smoke-test the image but never push and never need registry credentials.
- Document how to run the published image: required volumes, environment
  variables, auth key handling, and the non-root ownership requirement.

## Capabilities

### New Capabilities

- `container-image`: the project produces a runnable OCI image whose runtime
  contract (non-root user, volumes, ports, entrypoint, bundled git) is stable
  and testable.
- `image-publishing`: CI builds and smoke-tests the image, and publishes it to
  GHCR under defined triggers, permissions, and tag rules.

### Modified Capabilities

None. This adds deployment and release behavior; the existing serving,
identity, and authorization requirements are unchanged.

## Impact

- New files: `Containerfile`, a container ignore file, `.github/workflows/`
  container workflow, and deployment documentation.
- `README.md` gains the pull-and-run path; `docs/configuration.md` or a new
  `docs/deployment.md` records the container paths and volume contract.
- No new Go dependencies and no application code changes beyond passing
  `-ldflags -X main.version=...` at build time. The `version` variable already
  exists for this purpose.
- Release surface: packages under the repository owner in GHCR. The workflow
  needs `contents: read` and `packages: write`; pull request runs must not have
  push access.
- The existing `just ci` workflow is unchanged. Image work is a separate
  workflow so a registry outage cannot block the test contract.
- Out of scope: signing/attestations, SBOM generation, Docker Hub or other
  registries, Kubernetes manifests/Helm charts, automatic deployment, and
  host-tailscaled mode.
