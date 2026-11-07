# <project>

TODO: one paragraph — what this is, who it is for, and what it explicitly does
not do.

`docs/` holds project documentation. `openspec/` holds change artifacts.

## Commands

Everything runs through `just`.

| Command | Does |
|---|---|
| `just` | List recipes |
| `just fmt` | Format check; fails if anything is unformatted |
| `just lint` | Lint and static analysis |
| `just build` | Build |
| `just test` | Test suite |
| `just pre-commit` | Hook phase: `fmt lint` |
| `just pre-push` | Hook phase: `build test` |
| `just ci` | What CI runs |
| `just install-hooks` | Point git at `.githooks` (once per clone) |
| `just spec-list` | List OpenSpec changes |
| `just spec-validate` | Validate every spec and change |

The four checks are stubs until the project fills them in. Run `just ci` before
reporting any task complete. If a check fails, fix the code, not the check.

## Hooks

Run `just install-hooks` once per clone. After that `git commit` runs `fmt` and
`lint`, and `git push` runs `build` and `test`. Do not bypass hooks with
`--no-verify`; a broken hook is a bug to fix.

## Capturing learnings

When review feedback, a bug, a failed attempt, or a debugging detour reveals a
convention that should hold beyond the immediate change, record it as a
versioned skill in `.agents/skills/<name>/SKILL.md`. Never leave it only in an
agent's personal memory: that store is machine-local and invisible to other
people, machines, and agents.

Capture it when:

- **A failure cost real time.** Record the non-obvious cause so the next agent
  does not rediscover it from scratch.
- **New work establishes a pattern.** A package boundary, naming convention, or
  cross-cutting wiring decision belongs in a skill so later work stays
  consistent instead of drifting back to ad hoc code.
- **Review feedback generalizes.** If the lesson applies beyond this diff, it is
  a convention.

A one-off fix that does not generalize is just fixed. A skill needs both the
directive and a short rationale; a rule with no *why* is easy to silently
violate or "correct" back to the thing it was written to prevent. Fold a new
learning into an existing skill when the topic fits, and create a new file only
when it does not. Scope a skill with `paths:` frontmatter when it only applies
to part of the tree.

## Feature lifecycle (OpenSpec)

Feature-shaped work — not a one-line fix — moves through OpenSpec. Artifacts
live in `openspec/changes/<name>/`; completed changes are archived.

| Phase | Artifact | Command |
|---|---|---|
| Propose | `proposal.md` — why, what, capabilities | `/opsx-new` or `/opsx-propose` |
| Spec | `specs/<capability>/spec.md` — requirements and scenarios | `/opsx-continue` |
| Design | `design.md` — decisions and trade-offs | `/opsx-continue` |
| Tasks | `tasks.md` — implementation checklist | `/opsx-continue` |
| Implement | code; run `just ci` | `/opsx-apply` |
| Verify | `just spec-validate` plus a review pass | `/opsx-verify` |
| Archive | move the change under `openspec/changes/archive/` | `/opsx-archive` |

Specs use `SHALL` / `MUST` and `#### Scenario:` blocks; `just spec-validate`
must pass before archiving. The proposal and spec need a human accept/reject
before implementation starts — the gate is the approval, not the file.

## Workflow

Commit directly to `main` in small, self-contained units: one bug fix, one
feature slice, one refactor. Keep each commit revertable on its own so
`git bisect` and `git revert` stay useful.

An OpenSpec change lands as two commits:

1. `<change>: implement <summary>`
2. `<change>: archive and sync specs to main` — only the moves under
   `openspec/` plus the updated `openspec/specs/`.

Adopt branches and pull requests only if the project needs them. If you do,
update this section so the guide stays true.

## Conventions

TODO as the project acquires them. Keep this list short and push detail into
`.agents/skills/`: comment discipline, dependency policy, error handling,
naming, and anything a reviewer has had to say twice.
