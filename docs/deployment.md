# Deployment

`tsgit` is published as an OCI image to GitHub Container Registry. This page
covers the container contract and how to run it. `docs/configuration.md` remains
the source of truth for flags and environment variables.

## Image contract

| Property | Value |
|---|---|
| User | `10001:10001` (`tsgit`) |
| Binary | `/usr/local/bin/tsgit` |
| Default command | `tsgit serve` |
| State volume | `/var/lib/tsgit/state`, mode `0700` |
| Repository volume | `/var/lib/tsgit/repos`, mode `0750` |
| Runtime dependencies | `git`, CA certificates |
| Image reference | `ghcr.io/kevindurb/tsgit` |

The state and repository paths are separate on purpose. The state directory
holds the tsnet node key and must not be backed up with the repositories; the
repository root is the durable data. Both are declared volumes, so a container
can be replaced without losing the node registration or the repositories.

The image sets `TSGIT_STATE_DIR` and `TSGIT_REPO_ROOT` to those paths. It does
not contain an auth key, `tailscaled.state`, or repository content.

## Pull the image

```sh
podman pull ghcr.io/kevindurb/tsgit:latest
```

If the GHCR package is private, log in first with a token that has
`read:packages`:

```sh
printf '%s' "$GITHUB_TOKEN" | podman login ghcr.io -u "$GITHUB_USER" --password-stdin
```

Pin a release tag instead of `latest` for reproducible deployments.

## Run on a tailnet

Tailnet mode does not publish host ports. The tsnet listeners live inside the
userspace network stack, so the container only needs outbound connectivity to
the Tailscale control plane and to tailnet peers.

```sh
podman volume create tsgit-state
podman volume create tsgit-repos

export TS_AUTHKEY=tskey-auth-...
podman run -d --name tsgit \
  --restart=unless-stopped \
  -e TS_AUTHKEY \
  -e TSGIT_HOSTNAME=tsgit \
  -v tsgit-state:/var/lib/tsgit/state \
  -v tsgit-repos:/var/lib/tsgit/repos \
  ghcr.io/kevindurb/tsgit:latest
```

`-e TS_AUTHKEY` forwards the value from your shell without putting it in the
command line. After the first successful registration the state volume holds
the node key, and the auth key is only needed again for a fresh state directory.

To use an auth key file instead, mount it and set `TS_AUTHKEY_FILE`:

```sh
podman run -d --name tsgit \
  --restart=unless-stopped \
  -e TS_AUTHKEY_FILE=/run/tsgit-authkey \
  -v "$PWD/authkey:/run/tsgit-authkey:ro" \
  -e TSGIT_HOSTNAME=tsgit \
  -v tsgit-state:/var/lib/tsgit/state \
  -v tsgit-repos:/var/lib/tsgit/repos \
  ghcr.io/kevindurb/tsgit:latest
```

Any other setting uses its existing environment variable or a `serve` flag.
Flags go after `serve`, for example:

```sh
podman run -d --name tsgit \
  -e TS_AUTHKEY \
  -v tsgit-state:/var/lib/tsgit/state \
  -v tsgit-repos:/var/lib/tsgit/repos \
  ghcr.io/kevindurb/tsgit:latest serve --http-port 8080
```

## Bind mounts and ownership

Named volumes inherit the image directories' ownership. Bind mounts do not.
The image runs as UID/GID 10001, so the host directories must be writable by
that user:

```sh
mkdir -p /srv/tsgit/state /srv/tsgit/repos
sudo chown -R 10001:10001 /srv/tsgit
```

With rootless podman, use the user namespace mapping instead:

```sh
podman unshare chown -R 10001:10001 /srv/tsgit
```

Then mount them:

```sh
podman run -d --name tsgit \
  -e TS_AUTHKEY \
  -v /srv/tsgit/state:/var/lib/tsgit/state \
  -v /srv/tsgit/repos:/var/lib/tsgit/repos \
  ghcr.io/kevindurb/tsgit:latest
```

If the repository root is not writable, startup fails with an error naming the
path rather than serving a partially working forge.

## Upgrade and restart

```sh
podman pull ghcr.io/kevindurb/tsgit:v1.2.3
podman rm -f tsgit
podman run -d --name tsgit \
  --restart=unless-stopped \
  -e TS_AUTHKEY \
  -v tsgit-state:/var/lib/tsgit/state \
  -v tsgit-repos:/var/lib/tsgit/repos \
  ghcr.io/kevindurb/tsgit:v1.2.3
```

Reusing the same state and repository volumes preserves the node registration
and all repositories.

## Development mode

Development mode needs a published port because it listens with a normal
localhost socket, not tsnet:

```sh
podman run --rm -p 8080:8080 \
  ghcr.io/kevindurb/tsgit:latest serve --dev-listen 0.0.0.0:8080
```

Then clone from the host:

```sh
git clone http://127.0.0.1:8080/acme/widget.git
```

Development mode uses a temporary repository root and a fixed write-capable
identity. It never contacts a tailnet and is not a production fallback.

## Build and smoke-test locally

```sh
podman build --build-arg VERSION=dev -t tsgit:dev .
podman run --rm tsgit:dev version
podman run --rm --entrypoint git tsgit:dev --version
podman run --rm --entrypoint id tsgit:dev -u
```

The image's entrypoint is the `tsgit` binary, so commands other than `tsgit`
subcommands need `--entrypoint`. An arm64 build uses the same Containerfile:

```sh
podman build --platform linux/arm64 --build-arg VERSION=dev -t tsgit:dev-arm64 .
```

## Publishing

`.github/workflows/container.yml` owns image builds and publication. It runs
`just ci` in a separate `test` job, and the `image` job depends on it, so an
image cannot be published from a commit that fails formatting, lint, build, or
tests.

| Trigger | Tags | Published |
|---|---|---|
| push to `main` | `main`, `sha-<12>` | yes |
| push of `vX.Y.Z` | `vX.Y.Z`, `latest` | yes |
| pull request | none (local `pr-<12>` for the smoke test) | no |
| any other ref | workflow does not run | no |

`latest` moves only on a semver tag. Pin an exact release tag for a deployment
that should not change underneath you. A `v*` tag that is not a plain
`v<major>.<minor>.<patch>` still builds and smoke-tests, but does not publish
and cannot overwrite `latest`.

The image reference is `ghcr.io/` plus the repository name lowercased, for
example `ghcr.io/kevindurb/tsgit`. Publication uses only the workflow's
`GITHUB_TOKEN` with `contents: read` and `packages: write`; no personal access
token or repository secret is stored. Pull requests, including forks, build
only `linux/amd64` and never reach the login or push steps. Published builds are
a single manifest containing `linux/amd64` and `linux/arm64` images.

The first push creates the GHCR package. Its visibility and its link to the
repository are GitHub package settings, not workflow inputs: after the first
publish, open the package settings and link it to the repository, and keep it
private unless the project deliberately wants public pulls.
