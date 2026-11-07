set shell := ["bash", "-euc"]

default:
    @just --list

# --- Contract -------------------------------------------------------------
# Hooks and CI call these. Replace each stub with the project's real command.
# `fmt` and `lint` are checks: exit non-zero when something is wrong, do not
# fix it in place.

fmt:
    @echo "justfile: no fmt command configured yet" >&2 && exit 1

lint:
    @echo "justfile: no lint command configured yet" >&2 && exit 1

build:
    @echo "justfile: no build command configured yet" >&2 && exit 1

test:
    @echo "justfile: no test command configured yet" >&2 && exit 1

pre-commit: fmt lint
pre-push: build test

# Base validates its own scaffolding; a real project uses `ci: pre-commit pre-push`.
ci: check-template

install-hooks:
    git config core.hooksPath .githooks
    @echo "Hooks installed: pre-commit and pre-push now run via 'just'."

check-template:
    #!/usr/bin/env bash
    set -euo pipefail
    test -f README.md
    test -f AGENTS.md
    test -f openspec/config.yaml
    test -x .githooks/pre-commit
    test -x .githooks/pre-push
    echo "template scaffolding OK"

spec-list:
    openspec list

spec-validate:
    openspec validate --all
