# tsgit

A git forge for your tailnet. No accounts, no public write, no TLS setup.

Every peer that reaches tsgit is already authenticated by Tailscale. tsgit uses
that identity to decide who can read and write, so it never runs its own login
system.

## Built with AI

tsgit is built with AI. Coding agents write most of the code, tests, and docs
under human direction, and a human reviews every change.

## Status

Early, but usable. tsgit serves git over smart HTTP and shows a read-only
repository index. SSH, code browsing, issues, pull requests, and CI are not
implemented.

## What it does

- Serves `git clone`, `fetch`, and `push` over smart HTTP to tailnet peers.
- Creates a repository on an authorized first push.
- Lists the repositories you can read at `https://<hostname>/`.
- Deletes a repository from the web UI for viewers with `admin` access.
- Authorizes from Tailscale identity and ACL tags. There is no user database.
- Optionally publishes a policy-selected, read-only subset to the internet
  through Tailscale Funnel.

It does not serve users outside the tailnet except read-only Funnel repositories
that tailnet policy designates, run its own authentication, or act as a CI
system.

## Requirements

1. A tailnet.
2. A Tailscale auth key, or an existing registered node state.
3. A tailnet policy grant that gives peers the tsgit repository capability.

The grant needs both `ip` and `app`:

```json
"grants": [{
  "src": ["group:devs", "tag:ci"],
  "dst": ["tag:tsgit"],
  "ip": ["tcp:443"],
  "app": {
    "github.com/kevindurb/tsgit/cap/repo": [
      {"repos": ["acme/*"], "access": "write"},
      {"repos": ["shared/docs"], "access": "read"}
    ]
  }
}]
```

Without a matching grant, every request returns `404`. That is deliberate: the
server does not reveal whether a repository exists to someone who cannot read
it. See [`docs/access-control.md`](docs/access-control.md) for the full payload.

Deletion is a separate authority. Only the `admin` level can remove a
repository, and it also implies `write` and `read`. Grant it narrowly:

```json
"grants": [{
  "src": ["group:admins"],
  "dst": ["tag:tsgit"],
  "ip": ["tcp:443"],
  "app": {
    "github.com/kevindurb/tsgit/cap/repo": [
      {"repos": ["acme/*"], "access": "admin"}
    ]
  }
}]
```

Keep `admin` away from CI and other write-only identities. A peer granted
`admin` cannot use an older `tsgit` binary: an unknown access value fails the
whole capability payload closed, so remove admin entries before downgrading.

## Deploy

The image is published to GitHub Container Registry.

```sh
podman volume create tsgit-state
podman volume create tsgit-repos

export TS_AUTHKEY=tskey-auth-...

podman run -d --name tsgit \
  --restart=unless-stopped \
  -e TS_AUTHKEY \
  -e TSGIT_HOSTNAME=tsgit \
  -e TS_ADVERTISE_TAGS=tag:tsgit \
  -v tsgit-state:/var/lib/tsgit/state \
  -v tsgit-repos:/var/lib/tsgit/repos \
  ghcr.io/kevindurb/tsgit:main
```

The example advertises `tag:tsgit`, so the auth key must be allowed to apply
that tag. Use the same tag in the policy grant above.

`:main` tracks the latest merge. For a deployment that should not change
underneath you, pin a release tag such as
`ghcr.io/kevindurb/tsgit:v1.2.3`. `latest` moves only when a release tag is
published.

The state volume holds the node key, so the auth key is only needed for the
first registration. The repository volume holds the bare git directories. Back
up the repository volume; keep it separate from the state volume. For bind
mounts, auth-key files, upgrades, and arm64, see
[`docs/deployment.md`](docs/deployment.md).

## Use it

Open the index at `https://tsgit/` (MagicDNS). It lists the repositories you may
read and marks whether you may push. To clone:

```sh
git clone https://tsgit/acme/widget.git
```

To create a repository, push to it. A user or tagged node with `write` access
creates it on the first push:

```sh
git remote add origin https://tsgit/acme/widget.git
git push -u origin main
```

### Delete a repository

A viewer with `admin` on a repository sees a Delete link on its summary page.
The confirmation page requires typing the canonical name, then permanently
removes the directory and all of its history. There is no undo and no trash;
back up the repository volume if you might need the data. The public Funnel
listener does not serve the delete surface.

### Rename a repository

Rename is a client-side operation: mirror the old repository to the new name,
then delete the old one in the web UI.

```sh
git clone --mirror https://tsgit/acme/widget.git widget.git
cd widget.git
git push --mirror https://tsgit/acme/gadget.git
```

The client needs `write` on the new name and `admin` on the old one. Refs and
objects carry over; the bare repository's `description` and local config may
not. Old clone URLs stop working once the old name is deleted.

If HTTPS is unavailable, the node serves plain HTTP on port 80, so use
`http://tsgit/`. WireGuard still encrypts the traffic.

## Public read-only access (Funnel)

Off by default. Publishing a curated subset to the internet needs all three
keys:

1. Start the server with `TSGIT_FUNNEL=true` (or `serve --funnel`).
2. Give the node the `funnel` attribute.
3. Give it a `cap/funnel` payload under `nodeAttrs.app` naming the repositories
   to publish.

```json
"nodeAttrs": [{
  "target": ["tag:tsgit"],
  "attr": ["funnel"],
  "app": {
    "github.com/kevindurb/tsgit/cap/funnel": [
      {"repos": ["public/*"]}
    ]
  }
}]
```

Funnel also needs MagicDNS, HTTPS certificates, and a supported port (`443`,
`8443`, or `10000`). None of the three keys alone exposes anything.

The public URL is the node's DNS name:

```sh
git clone https://tsgit.<tailnet>.ts.net/public/widget.git
```

Only clone/fetch and the filtered index are public. Receive-pack,
push-to-create, and every mutation return `404`; the payload has no access
field, so public write is impossible. If a prerequisite is missing, `tsgit`
logs an error and keeps serving the tailnet. See
[`docs/access-control.md`](docs/access-control.md) and
[`docs/deployment.md`](docs/deployment.md).

## Local development

Development mode needs no tailnet and no auth key. It uses a temporary
repository root and a fixed write-capable identity:

```sh
just run serve --dev-listen 127.0.0.1:8080
```

Then open `http://127.0.0.1:8080/` or clone:

```sh
git clone http://127.0.0.1:8080/acme/widget.git
```

This mode is not a production fallback.

## Documentation

- [`docs/project.md`](docs/project.md) — purpose, users, scope, non-goals
- [`docs/architecture.md`](docs/architecture.md) — how the pieces fit
- [`docs/configuration.md`](docs/configuration.md) — flags and environment variables
- [`docs/deployment.md`](docs/deployment.md) — container details and publishing
- [`docs/access-control.md`](docs/access-control.md) — the capability contract

## Layout

| Path | Holds |
|---|---|
| `cmd/tsgit/` | The binary's entry point |
| `internal/` | Implementation, one package per concern |
| `docs/` | Project and architecture documentation |
| `openspec/` | Specs and change artifacts |
| `.agents/skills/` | Durable agent conventions |

## Contributing

Requires Go, `just`, and `git`.

```sh
git clone git@github.com:kevindurb/tsgit.git
cd tsgit
just install-hooks   # once per clone
just ci
```

`just` lists the rest. `AGENTS.md` explains the workflow and conventions.
