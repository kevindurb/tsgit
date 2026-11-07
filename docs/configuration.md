# Configuration

`tsgit serve` takes flags with app-prefixed environment fallbacks. Flags always
win over the environment. Defaults are shown for the `serve` command.

| Flag | Environment | Default | Meaning |
|---|---|---|---|
| `--hostname` | `TSGIT_HOSTNAME`, then `TS_HOSTNAME` | `tsgit` | Tailnet hostname to advertise |
| `--port` | `TSGIT_PORT` | `443` | HTTPS listener port inside the tailnet |
| `--http-port` | `TSGIT_HTTP_PORT` | `80` | HTTP listener port inside the tailnet |
| `--state-dir` | `TSGIT_STATE_DIR`, then `TS_STATE_DIR` | `os.UserConfigDir()/tsnet-tsgit` | tsnet state, including the node key |
| `--repo-root` | `TSGIT_REPO_ROOT` | required unless `--dev-listen` | Root of the bare repository tree |
| `--advertise-tags` | `TS_ADVERTISE_TAGS` | empty | Comma-separated ACL tags for this node |
| `--authkey-file` | `TS_AUTHKEY_FILE` | empty | File containing a Tailscale auth key |
| `--control-url` | `TS_CONTROL_URL` | hosted control plane | Coordination server URL |
| `--https` | `TSGIT_HTTPS` | `true` | Serve TLS when the tailnet permits it |
| `--funnel` | `TSGIT_FUNNEL` | `false` | Open a read-only public listener through Tailscale Funnel |
| `--funnel-max-concurrent` | `TSGIT_FUNNEL_MAX_CONCURRENT` | `4` | Maximum concurrent public pack exchanges; must be positive |
| `--default-branch` | `TSGIT_DEFAULT_BRANCH` | `main` | Initial branch of created repositories |
| `--dev-listen` | `TSGIT_DEV_LISTEN` | empty | Localhost address for development mode |
| `--log` | `TSGIT_LOG` | `info` | `debug`, `info`, `warn`, or `error` |
| `--verbose` | | `false` | Verbose logging |
| `--debug-tsnet` | | `false` | Log tsnet internals |
| `--version` | | `false` | Print the version and exit |

`--authkey-file` and the `TS_AUTHKEY` environment variable are mutually
exclusive; tsnet itself reads `TS_AUTHKEY` when no file is given. A missing auth
key file is only a warning when the state directory already holds a registered
node.

`--repo-root` is required in tailnet mode. `--dev-listen` supplies a temporary
root and a development identity, so no root is configured.

## Data layout

Two directories, separate on purpose:

```
$TSGIT_STATE_DIR/                0700, node key and tsnet state
  tailscaled.state               never backed up with repositories

$TSGIT_REPO_ROOT/                0750, the durable artifact
  .tsgit-tmp/                    staging for atomic creation
  acme/
    widget.git/                  a normal bare git directory
```

The state directory is a secret tied to this node. Losing it re-registers the
node, and ACLs that name the old node stop matching. The repository root is the
thing to back up; it is plain bare git directories readable by any git client.

## HTTPS

TLS is enabled only when `--https` is true, the node has the `https`
capability, and the tailnet returns at least one certificate domain. Otherwise
the daemon serves plain HTTP on `--http-port` and logs that HTTPS is
unavailable. WireGuard already encrypts tailnet traffic; TLS provides a trusted
hostname and normal client ergonomics.

## Public listener (Funnel)

`--funnel` is off by default. It opens a second, public listener through
Tailscale Funnel serving only clone/fetch and the filtered discovery pages.
Three independent keys are required before anything is published: `--funnel`,
the tailnet `funnel` node attribute, and a non-empty `cap/funnel` payload under
the node's `nodeAttrs.app`. None of them alone exposes a repository. The payload
contract is in `docs/access-control.md`.

Funnel needs HTTPS certificate domains, so it is opened only when the node has
the `https` and `funnel` capabilities and a certificate domain. If any
prerequisite is missing, or the public listener cannot bind, `tsgit` logs an
error and keeps serving the tailnet; the public listener is not opened.

The public server is deliberately stricter than the tailnet server:

| Control | Value | Reason |
|---|---|---|
| `ReadHeaderTimeout` | `10s` | bounds slow header writes |
| `ReadTimeout` | `60s` | bounds request-body reads |
| `IdleTimeout` | `60s` | bounds idle keep-alive |
| `WriteTimeout` | none | large clones are legitimate |
| `MaxHeaderBytes` | `1 MiB` | cheap header ceiling |
| request body | `64 MiB` | fetch negotiation is small |
| concurrent pack POSTs | `--funnel-max-concurrent` (`4`) | CPU, disk, and Funnel bandwidth |

A declared `Content-Length` over the body limit is refused with `413` before a
backend starts. A chunked body is bounded by the same limit while it is read. A
public `POST .../git-upload-pack` that finds every slot busy is refused with
`503` and `Retry-After`; it is never queued. The public client's source address
comes from `FunnelConn.Src` and is logged with the `funnel` principal; it is
never used for authorization.

## Development mode

```
just run serve --dev-listen 127.0.0.1:8080
git clone http://127.0.0.1:8080/acme/widget.git
```

Development mode never contacts a tailnet and never starts tsnet. It serves the
same handler with a fixed write-capable identity and a temporary repository
root that is removed on exit. The discovery UI is served at `/` on the same
listener. It is not a production fallback.
