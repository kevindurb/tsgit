# 1. Shell out to git plumbing for transport

## Status

Accepted.

## Context

`tsgit` must serve fetch and push. The git wire protocol is large, versioned,
and changes; reimplementing pack negotiation in Go is a multi-month project and
a permanent maintenance tail.

## Decision

Transports invoke `git upload-pack` and `git receive-pack` as subprocesses and
pass the client's streams through, the way `git-shell` and Gitolite do. `tsgit`
owns identity, authorization, discovery, and repository lifecycle. It does not
own the wire protocol.

## Consequences

- Every git feature, including protocol v2 and partial clone, works from the
  first commit without being implemented.
- Authorization must be decided *before* the subprocess starts. There is no
  mid-transfer permission check, and a long fetch holds its access decision.
- We inherit git's own quirks and must track its version on the host.
- The host must have `git` installed. A single static binary is not achievable,
  which is fine: the target is a tailnet node, not a container scratch image.
