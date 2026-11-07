# Proposal

## Why

The README describes the project's philosophy well but addresses contributors
rather than people who want to run it. A new user gets no requirements, no
deploy steps, and no working example. The single biggest gap is authorization:
without a tailnet ACL `app` grant, every request is a deliberate `404`, so a
correct deployment looks broken. The README also never says the project is
built with AI, which the project wants to state plainly.

## What Changes

- Rewrite `README.md` around the user path: what it is, what works today,
  requirements, deploy, and first use.
- Add a short **Built with AI** section near the top:

  > tsgit is built with AI. Coding agents write most of the code, tests, and
  > docs under human direction, and a human reviews every change.

- Add a **Requirements** section naming the tailnet, an auth key, and an ACL
  grant, with the complete `ip` + `app` capability snippet.
- Inline the happy-path deployment (podman, volumes, `TS_AUTHKEY`, hostname)
  instead of only linking `docs/deployment.md`; keep edge cases in that doc.
- Add a **Use it** section: open the index, the clone URL shape, and that an
  authorized push creates the repository.
- Correct the image tag guidance: `:latest` moves only on a `vX.Y.Z` tag, so the
  quickstart uses `:main` and recommends a pinned release for stable
  deployments.
- Reframe **Status** to match what is implemented: smart HTTP serving and the
  read-only discovery UI work; SSH, code browsing, issues, pull requests, and
  CI do not.
- Keep the pitch, scope boundaries, layout, and documentation links, but trim
  wordy prose.
- Move contributor setup (`git clone`, `just install-hooks`, `just ci`) into a
  clearly separate section after the user path.

Out of scope: any code, configuration, or behavior change; `docs/` rewrites
beyond the links the README already carries; a `LICENSE` file (noted as a gap,
not added here).

## Capabilities

### New Capabilities

None. This is a documentation-only change; no spec-level behavior changes.

### Modified Capabilities

None.

## Impact

- `README.md` only.
- No code, tests, configuration, dependencies, or runtime behavior change.
- No new or modified capabilities; the change sets `skip_specs: true`.
