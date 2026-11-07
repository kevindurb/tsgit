set shell := ["bash", "-euc"]

default:
    @just --list

# --- Contract -------------------------------------------------------------
# Hooks and CI call these. `fmt` and `lint` are checks: they exit non-zero when
# something is wrong rather than fixing it in place.

fmt:
    #!/usr/bin/env bash
    set -euo pipefail
    unformatted=$(gofmt -l .)
    if [[ -n $unformatted ]]; then
        echo "gofmt needed on:" >&2
        echo "$unformatted" >&2
        exit 1
    fi

lint:
    go vet ./...

build:
    go build -o bin/tsgit ./cmd/tsgit

test:
    go test ./...

pre-commit: fmt lint
pre-push: build test

ci: pre-commit pre-push

install-hooks:
    git config core.hooksPath .githooks
    @echo "Hooks installed: pre-commit and pre-push now run via 'just'."

run *args:
    go run ./cmd/tsgit {{args}}

spec-list:
    openspec list

spec-validate:
    openspec validate --all
