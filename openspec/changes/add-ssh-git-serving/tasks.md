# Tasks

## 1. Config and skeleton

- [ ] 1.1 Add `internal/config` with listen address, repo root, and LocalAPI socket.
- [ ] 1.2 Add `serve` subcommand to `cmd/tsgit` that loads config and starts the SSH listener.
- [ ] 1.3 Fail at startup when the repo root is missing or unreadable.

## 2. Tailnet identity

- [ ] 2.1 Add `internal/identity` with a LocalAPI client hitting `/localapi/v0/whois`.
- [ ] 2.2 Define an `Identity` value: node name, user login, tags, and capabilities.
- [ ] 2.3 Resolve an SSH connection's peer address to an `Identity`.
- [ ] 2.4 Decode the project's capability name from the `WhoIs` capability map into a typed value.
- [ ] 2.5 Test: unknown peer returns an error and no identity.
- [ ] 2.6 Test: a peer with no capability resolves to an empty capability set.

## 3. Access control

- [ ] 3.1 Add `internal/authz` with `Decide(Identity, Repo, Op) Decision`.
- [ ] 3.2 Deny by default: no matching capability grants nothing.
- [ ] 3.3 Match repository globs in canonical `<owner>/<repo>` form with `path.Match`.
- [ ] 3.4 Union capability objects; the highest matching level wins, `write` implies `read`.
- [ ] 3.5 Test: read capability allows read and denies write; write allows both.
- [ ] 3.6 Test: a capability for one repo does not grant another.
- [ ] 3.7 Test: malformed capability JSON, unknown `access`, and an empty set all deny.
- [ ] 3.8 Test: tag-granted capability allows read with no user login.
- [ ] 3.9 Test: `Decide` takes no connection or request argument and runs without a tailnet.

## 4. Repository resolution

- [ ] 4.1 Add `internal/repos` mapping a name to a bare dir under the root.
- [ ] 4.2 Reject names escaping the root after symlink evaluation.
- [ ] 4.3 Distinguish not-found from not-permitted.

## 5. SSH transport

- [ ] 5.1 Accept SSH sessions and read the git command before authorizing.
- [ ] 5.2 Reject anything that is not `git-upload-pack` or `git-receive-pack`.
- [ ] 5.3 Exec the corresponding git plumbing with the session's streams attached.
- [ ] 5.4 Propagate the git exit status to the client.
- [ ] 5.5 Test: clone against a fixture repo; push by a writer; push denied for a reader.

## 6. Verification

- [ ] 6.1 `just ci` passes.
- [ ] 6.2 Manual: add a grant with `ip: ["tcp:22"]` and the repo capability, then clone and push from a second tailnet node; remove the grant and confirm denial.
- [ ] 6.3 Document the capability name and payload schema, with a complete grant example.
- [ ] 6.4 Add `docs/architecture.md` detail if the shipped shape differs.
- [ ] 6.5 Archive the change and sync specs to `openspec/specs/`.
