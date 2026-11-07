# Tasks

## 1. Config and skeleton

- [ ] 1.1 Add `internal/config` with listen address, repo root, policy path, and LocalAPI socket.
- [ ] 1.2 Add `serve` subcommand to `cmd/tsgit` that loads config and starts the SSH listener.
- [ ] 1.3 Fail at startup when the repo root is missing or unreadable.

## 2. Tailnet identity

- [ ] 2.1 Add `internal/identity` with a LocalAPI client hitting `/localapi/v0/whois`.
- [ ] 2.2 Define an `Identity` value: node name, user login, tags.
- [ ] 2.3 Resolve an SSH connection's peer address to an `Identity`.
- [ ] 2.4 Test: unknown peer returns an error and no identity.

## 3. Access control

- [ ] 3.1 Add `internal/authz` with a policy loader for the YAML rule list.
- [ ] 3.2 Implement `Decide(Identity, Repo, Op) Decision` with deny-by-default.
- [ ] 3.3 Support `user:<login>` and `tag:<tag>` subjects and repo globs.
- [ ] 3.4 Test: no matching rule denies; unreadable policy denies; tag grants read with no user.
- [ ] 3.5 Test: `Decide` takes no connection or request argument and runs without a tailnet.

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
- [ ] 6.2 Manual: clone and push from a second tailnet node.
- [ ] 6.3 Add `docs/architecture.md` detail if the shipped shape differs.
- [ ] 6.4 Archive the change and sync specs to `openspec/specs/`.
