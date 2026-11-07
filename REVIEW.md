# Review checklist

What a review pass — human or agent — checks before a change lands. One flat
list: no per-feature copies, no severity tiers.

- **Correctness** — does the change do what its `openspec/changes/<name>/`
  artifacts say? Are there edge cases the spec implies and the code misses?
- **Error paths** — what happens on bad input, a missing dependency, or a
  failed network call? Failures should be explicit, not silent.
- **Tests** — do they assert behaviour rather than implementation? Would they
  fail if the change were reverted?
- **Dependencies** — is each new one justified? A dependency is a maintenance
  commitment, not a convenience.
- **Secrets and safety** — no credentials, tokens, or private keys in the diff,
  and no debug output or commented-out code left behind.
- **Comments** — default to none. A comment explains *why*, never *what*.
- **Docs and specs** — if behaviour changed, was the spec or doc updated to
  match? Divergence is a defect even when the code is right.
- **Rollback** — if this ships and breaks, is reverting it a clean operation?

Automated checks (`just ci`) already cover formatting, lint, build, and tests.
This list covers what they cannot: whether the change is correct and matches
intent.
