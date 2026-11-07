# base

The template repo for new projects. It carries the workflow scaffolding I want
in every repo and nothing about a language, build system, or deployment.

Marked as a GitHub template: **Use this template → Create a new repository**.

## What's here

| Path | Why |
|---|---|
| `justfile` | The command contract. Every repo exposes the same entry points. |
| `.githooks/` | `pre-commit` and `pre-push`, both thin wrappers over `just`. |
| `AGENTS.md` | Canonical guide for coding agents: commands, workflow, conventions. |
| `REVIEW.md` | Generic pre-land review checklist. |
| `.agents/skills/` | Durable, versioned project conventions. One topic per `SKILL.md`. |
| `openspec/` | Spec-driven change artifacts (`proposal → specs → design → tasks`). |
| `.opencode/` | OpenSpec commands and skills for OpenCode, generated and committed. |
| `.github/workflows/ci.yml` | Runs `just ci` on pushes to `main` and on pull requests. |
| `.editorconfig`, `.gitattributes`, `.gitignore` | Baseline hygiene. |

## Starting a project

1. Create the repo from this template — GitHub's **Use this template**, or
   `gh repo create <name> --template kevindurb/base --private --clone`.
2. `just install-hooks` — once per clone.
3. Fill in the contract in `justfile`: replace the failing stubs for `fmt`,
   `lint`, `build`, and `test` with the project's real commands. `fmt` and
   `lint` must check, not fix.
4. Fill in `AGENTS.md`: purpose, the commands table, and conventions.
5. Fill in the `context:` block in `openspec/config.yaml` — it is what agents
   read before writing a proposal.
6. In `justfile`, change `ci: check-template` to `ci: pre-commit pre-push` and
   delete the `check-template` recipe.
7. Rewrite this README and add project docs under `docs/`.
8. Commit.

### About this repo itself

`base` has no code, so its own `ci` points at `just check-template`, which
verifies the scaffolding is intact. Do **not** run `just install-hooks` here:
the stubs are meant to fail and would block template edits.

## Deliberately not included

Stack-specific build, lint, and test commands; release and tagging policy;
deploy steps; a licence; `CLAUDE.md` / `.claude/`; and stack overlay
directories. Those are first-commit decisions for each project, not template
defaults.
