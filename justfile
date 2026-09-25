# ccu (compose-check-updates) — task runner
#
# Install `just`:  winget install Casey.Just   (or  go install github.com/casey/just@latest)
# List recipes:    just            (or  just --list)
#
# Shared recipes (build, test, fmt, ci, release, …) live in .just/, copied from
# ~/coding/just-common. Edit them there and run `just sync-common`; this file
# only holds what is specific to ccu.
#
# Layout:
#   .                  — the `ccu` CLI entry point lives at the repo root  (-> ccu / ccu.exe)
#   internal/policy    — what the user recorded about an image; imports nothing
#   internal/versioning — reading a tag as a version (semver, loose, regex)
#   internal/compose   — compose files, the images they declare, their Dockerfiles
#   internal/registry  — tag lists and digests from OCI registries
#   internal/check     — resolves one file's images, and writes the new tags back
#   internal/scanner   — walks a directory and checks every file it finds
#   internal/report    — the non-interactive output (pretty / JSON Lines)
#   internal/tui       — the interactive interface
#   internal/cli       — flag parsing and usage; internal/modes — the check run
#   internal/config    — .ccu.yaml and the global config
#   internal/logger, buildinfo, registrytest — logging, version stamp, test registry
#   tests/             — fixture-driven tests (covered by `go test ./...`)
#   VERSION            — single source of truth for the version (stamped into the binary)

set allow-duplicate-recipes

import '.just/common.just'
import '.just/go.just'
import '.just/release.just'

BIN_NAME := "ccu"
BUILDINFO_PKG := "github.com/p-arndt/compose-check-updates/internal/buildinfo"

# The golangci-lint the lint recipe runs. Bump together with the version the CI
# lint job pins.
GOLANGCI_LINT_VERSION := "2.14.0"

# Run golangci-lint. Installs the pinned version into GOPATH/bin whenever the
# binary there is missing or a different version, so bumping the pin reaches
# every checkout. It runs that binary rather than whatever is first on PATH: an
# older one, or one built by an older Go, fails to load packages written for the
# toolchain in use.
[unix]
lint:
    @"$(go env GOPATH)/bin/golangci-lint" version 2>/dev/null | grep -q "version {{GOLANGCI_LINT_VERSION}} " || go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v{{GOLANGCI_LINT_VERSION}}
    "$(go env GOPATH)/bin/golangci-lint" run

# Run golangci-lint. Installs the pinned version into GOPATH/bin whenever the
# binary there is missing or a different version, so bumping the pin reaches
# every checkout. It runs that binary rather than whatever is first on PATH: an
# older one, or one built by an older Go, fails to load packages written for the
# toolchain in use.
[windows]
lint:
    $exe = Join-Path (go env GOPATH) "bin\golangci-lint.exe"; if (-not ((Test-Path $exe) -and ((& $exe version 2>$null) -match "version {{GOLANGCI_LINT_VERSION}} "))) { go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v{{GOLANGCI_LINT_VERSION}} }; & $exe run

# Print the test coverage per package and in total. Nothing enforces a number;
# this is here to look at when you want to know, not to gate on.
[unix]
cover:
    go test -coverprofile=coverage.out ./...
    go tool cover -func=coverage.out | tail -1

# Print the test coverage per package and in total. Nothing enforces a number;
# this is here to look at when you want to know, not to gate on.
[windows]
cover:
    go test -coverprofile=coverage.out ./...
    go tool cover -func=coverage.out | Select-Object -Last 1

# Run every check the way CI should.
ci: fmt-check vet lint test

# The recording runs against invented stacks and a fake registry — never the
# real Docker Hub, never your own compose files. Pass --keep to inspect the
# throwaway world it built. Watch the GIF afterwards: a zero exit code only
# means vhs did not crash.
#
# Record assets/demo.gif from demo/ccu.tape (needs `vhs` and `node`).
demo *ARGS:
    node scripts/demo.mjs {{ARGS}}
