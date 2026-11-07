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

## Development mode

```
just run serve --dev-listen 127.0.0.1:8080
git clone http://127.0.0.1:8080/acme/widget.git
```

Development mode never contacts a tailnet and never starts tsnet. It serves the
same handler with a fixed write-capable identity and a temporary repository
root that is removed on exit. It is not a production fallback.
