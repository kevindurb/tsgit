---
name: smart-http-framing
description: Invariants for tsgit's git smart-HTTP framing and backend invocation. Use when changing internal/transport, adding a backend, or debugging a stock git client that rejects the service ("is this a git repository?", "Could not read from remote repository").
paths:
  - "internal/transport/**"
---

# Smart-HTTP framing invariants

The handler reproduces a small, exact part of git's smart HTTP. A fake backend
will happily accept a subtly wrong response; a stock git client will not. Test
framing changes against real `git clone`, `git fetch`, and `git push`, not only
a fake backend.

## The discovery preamble needs the flush packet

For v0/v1 reference discovery, the response body is:

```
<service pkt-line>0000<advertisement>
```

`<service pkt-line>` is `# service=git-upload-pack\n` (or
`git-receive-pack`) with its four-byte length prefix. The `0000` flush packet is
easy to miss and is mandatory. Without it git reports
`info/refs not valid: is this a git repository?` and never issues the POST that
would reveal the real problem.

With `Git-Protocol: version=2`, omit the service pkt-line and the flush for
**upload-pack** only; the v2 advertisement starts directly with `version 2`.
Receive-pack keeps the v0 preamble even when the header is present.

## Content types use the full service name

The HTTP service name already contains the `git-` prefix:

| Request | Header |
|---|---|
| discovery | `Content-Type: application/x-git-upload-pack-advertisement` |
| discovery | `Content-Type: application/x-git-receive-pack-advertisement` |
| fetch | `Content-Type: application/x-git-upload-pack-result` |
| push | `Content-Type: application/x-git-receive-pack-result` |

Do not build these by prepending `application/x-git-` to a service name that
already starts with `git-`; that produces
`application/x-git-git-upload-pack-advertisement` and git silently falls back
to dumb HTTP or fails.

## The git subcommand drops the `git-` prefix

`Request.Service` uses the HTTP names `git-upload-pack` and `git-receive-pack`.
The process to run is `git upload-pack` / `git receive-pack`:

```
git upload-pack --stateless-rpc [--advertise-refs] <repo>
git receive-pack --stateless-rpc [--advertise-refs] <repo>
```

`git git-upload-pack` is not a command and fails with
`git: 'git-upload-pack' is not a git command`. Strip the prefix before building
argv, and set `GIT_PROTOCOL` from the request header.

## Other shape rules that git depends on

- Headers must be set before the first backend byte. This is why the writer
  defers `WriteHeader` and the advertisement prefix until the backend's first
  `Write`: a backend failure before that point can still become a `500`.
- Stream the request body to the subprocess and its stdout to the response, and
  flush each write. Buffering the pack breaks large clones and push.
- Inflate `Content-Encoding: gzip` request bodies. Git compresses large pushes.
- `GET .../info/refs?service=git-receive-pack` is a write + create operation,
  not a read. Creation happens during discovery, before any pack arrives.
