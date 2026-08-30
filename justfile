# ccu (compose-check-updates) — task runner
#
# Install `just`:  winget install Casey.Just   (or  go install github.com/casey/just@latest)
# List recipes:    just            (or  just --list)
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

# Run recipes through PowerShell on Windows so multi-line bodies and env work.
# Everything else runs under the default `sh`, so the recipes that need shell
# syntax exist twice: once `[unix]`, once `[windows]`.
set windows-shell := ["pwsh.exe", "-NoLogo", "-NoProfile", "-Command"]

# Default: show the recipe list.
default:
    @just --list

# ---------------------------------------------------------------------------
# Dev
# ---------------------------------------------------------------------------

# Run the CLI from source, passing through any args:  just run check -d .
run *ARGS:
    go run . {{ARGS}}

# Build a plain dev binary -> ccu (version reports as "dev").
[unix]
build:
    go build -o ccu .

# Build a plain dev binary -> ccu.exe (version reports as "dev").
[windows]
build:
    go build -o ccu.exe .

# ldflags used by the release builds: stamp version metadata + strip symbols.

# Build a stripped, statically-linked release binary for the host platform,
# stamped with the current VERSION -> ccu.
[unix]
build-release:
    CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X github.com/p-arndt/compose-check-updates/internal/buildinfo.Version=$(tr -d '[:space:]' < VERSION) -X github.com/p-arndt/compose-check-updates/internal/buildinfo.Commit=$(git rev-parse --short HEAD) -X github.com/p-arndt/compose-check-updates/internal/buildinfo.Date=$(date -u +%Y-%m-%dT%H:%M:%SZ)" -o ccu .

# Build a stripped, statically-linked release binary for the host platform,
# stamped with the current VERSION -> ccu.exe.
[windows]
build-release:
    $env:CGO_ENABLED = "0"; go build -trimpath -ldflags "-s -w -X github.com/p-arndt/compose-check-updates/internal/buildinfo.Version=$((Get-Content VERSION -Raw).Trim()) -X github.com/p-arndt/compose-check-updates/internal/buildinfo.Commit=$(git rev-parse --short HEAD) -X github.com/p-arndt/compose-check-updates/internal/buildinfo.Date=$(Get-Date -AsUTC -Format o)" -o ccu.exe .

# ---------------------------------------------------------------------------
# Quality
# ---------------------------------------------------------------------------

# Run the test suite (includes the tests/ fixtures).
test:
    go test ./...

# Vet for suspicious constructs.
vet:
    go vet ./...

# Format all Go code.
fmt:
    gofmt -w .

# Verify formatting without writing changes (fails if anything is unformatted).
[unix]
fmt-check:
    @unformatted=$(gofmt -l .); if [ -n "$unformatted" ]; then echo "$unformatted"; echo "unformatted files (run: just fmt)" >&2; exit 1; fi

# Verify formatting without writing changes (fails if anything is unformatted).
[windows]
fmt-check:
    @if (gofmt -l .) { Write-Error "unformatted files (run: just fmt)"; exit 1 }

# Run golangci-lint. Installs the pinned version into GOPATH/bin on first use,
# so this works without a separate install step; keep the version in step with
# the one the CI lint job pins.
[unix]
lint:
    @command -v golangci-lint >/dev/null 2>&1 || go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.13.2
    "$(command -v golangci-lint || echo "$(go env GOPATH)/bin/golangci-lint")" run

# Run golangci-lint. Installs the pinned version into GOPATH/bin on first use,
# so this works without a separate install step; keep the version in step with
# the one the CI lint job pins.
[windows]
lint:
    @if (-not (Get-Command golangci-lint -ErrorAction SilentlyContinue)) { go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.13.2 }
    $exe = (Get-Command golangci-lint -ErrorAction SilentlyContinue).Source; if (-not $exe) { $exe = Join-Path (go env GOPATH) "bin\golangci-lint.exe" }; & $exe run

# Print the test coverage per package and in total. CI fails below the floor
# named in its "Coverage floor" step; keep the two in step when raising it.
[unix]
cover:
    go test -coverprofile=coverage.out ./...
    go tool cover -func=coverage.out | tail -1

# Print the test coverage per package and in total. CI fails below the floor
# named in its "Coverage floor" step; keep the two in step when raising it.
[windows]
cover:
    go test -coverprofile=coverage.out ./...
    go tool cover -func=coverage.out | Select-Object -Last 1

# Run every check the way CI should.
ci: fmt-check vet lint test

# ---------------------------------------------------------------------------
# Release
# ---------------------------------------------------------------------------

# Print the current version (read from the VERSION file).
[unix]
version:
    @printf '%s\n' "$(tr -d '[:space:]' < VERSION)"

# Print the current version (read from the VERSION file).
[windows]
version:
    @(Get-Content VERSION -Raw).Trim()

# Stamp a version into the VERSION file without committing. Accepts a bump
# keyword or an explicit version. Examples:
#   just set-version patch        just set-version 0.5.0
set-version BUMP="patch":
    node scripts/set-version.mjs {{BUMP}}

# Cut a release: bump the version (patch|minor|major, or an explicit x.y.z),
# stamp VERSION, commit, tag, and push -> the tag push triggers the release
# workflow which builds the binaries for every platform. Examples:
#   just release            just release minor            just release 1.0.0
release BUMP="patch":
    node scripts/release.mjs {{BUMP}}

# ---------------------------------------------------------------------------
# Housekeeping
# ---------------------------------------------------------------------------

# Remove build artifacts.
[unix]
clean:
    rm -f ccu
    rm -rf dist build stage

# Remove build artifacts.
[windows]
clean:
    -Remove-Item -Force ccu.exe -ErrorAction SilentlyContinue
    -Remove-Item -Recurse -Force dist, build, stage -ErrorAction SilentlyContinue

# ---------------------------------------------------------------------------
# Demo
# ---------------------------------------------------------------------------

# The recording runs against invented stacks and a fake registry — never the
# real Docker Hub, never your own compose files. Pass --keep to inspect the
# throwaway world it built. Watch the GIF afterwards: a zero exit code only
# means vhs did not crash.
#
# Record assets/demo.gif from demo/ccu.tape (needs `vhs` and `node`).
demo *ARGS:
    node scripts/demo.mjs {{ARGS}}
