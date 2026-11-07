# Tasks

## 1. Structure and positioning

- [x] 1.1 Reorder `README.md` to the user path — pitch, Built with AI, status,
  scope, requirements, deploy, use, local development, docs, contributor
  setup — and verify every section named in `proposal.md` is present.
- [x] 1.2 Add the pitch and the exact two-sentence **Built with AI** section
  from `proposal.md`, placed before Requirements, and verify the wording matches
  the approved text.
- [x] 1.3 Rewrite the status and scope copy to match shipped behavior: smart
  HTTP clone/fetch/push and the read-only discovery UI work; SSH, code
  browsing, issues, pull requests, and CI do not. Verify every claim against
  `docs/project.md` and `docs/architecture.md`.

## 2. User path

- [x] 2.1 Add a **Requirements** section naming the tailnet, an auth key, and a
  tailnet ACL grant, with a complete `ip` + `app` snippet. Verify the JSON is
  valid and matches the tested example in `docs/access-control.md`.
- [x] 2.2 Inline the happy-path deployment (pull, volumes, `TS_AUTHKEY`,
  `TSGIT_HOSTNAME`, `--restart`) and link `docs/deployment.md` for bind mounts,
  auth-key files, and upgrade edge cases. Verify every flag and environment
  variable matches `docs/configuration.md` and `docs/deployment.md`.
- [x] 2.3 Add a **Use it** section covering the repository index, the clone URL
  shape, and that an authorized push creates the repository. Verify the URL
  shape against `docs/architecture.md` and the commands against a local
  `--dev-listen` run.

## 3. Accuracy and consistency

- [x] 3.1 Correct the image tag guidance: use `:main` in the quickstart and
  recommend a pinned `vX.Y.Z` tag for stable deployments, noting that `latest`
  moves only on a release tag. Verify against
  `.github/workflows/container.yml`.
- [x] 3.2 Move contributor setup (`git clone`, `just install-hooks`, `just ci`)
  below the user path, keep the layout table and documentation links, and trim
  wordy prose without dropping the scope boundaries.
- [x] 3.3 Read the finished README top to bottom and verify there are no stale
  claims, no broken relative links, and no contradiction with `docs/`.
- [x] 3.4 Run `just ci` and `just spec-validate`; verify both pass and confirm
  the diff touches `README.md` and the change artifacts only.
