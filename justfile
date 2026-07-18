# ccu (compose-check-updates) — task runner
#
# Install `just`:  winget install Casey.Just   (or  go install github.com/casey/just@latest)
# List recipes:    just            (or  just --list)
#
# Layout:
#   .                  — the `ccu` CLI entry point lives at the repo root  (-> ccu.exe)
#   internal/…         — scanner, modes, tui, registry lookups, buildinfo
#   tests/             — fixture-driven tests (covered by `go test ./...`)
#   VERSION            — single source of truth for the version (stamped into the binary)

# Run recipes through PowerShell on Windows so multi-line bodies and env work.
set windows-shell := ["pwsh.exe", "-NoLogo", "-NoProfile", "-Command"]

# ldflags shared by the release builds: stamp version metadata + strip symbols.
_LDFLAGS := "-s -w -X github.com/padi2312/compose-check-updates/internal/buildinfo.Version=$(Get-Content VERSION -Raw).Trim() -X github.com/padi2312/compose-check-updates/internal/buildinfo.Commit=$(git rev-parse --short HEAD) -X github.com/padi2312/compose-check-updates/internal/buildinfo.Date=$(Get-Date -AsUTC -Format o)"

# Default: show the recipe list.
default:
    @just --list

# ---------------------------------------------------------------------------
# Dev
# ---------------------------------------------------------------------------

# Run the CLI from source, passing through any args:  just run -d . -i
run *ARGS:
    go run . {{ARGS}}

# Build a plain dev binary -> ccu.exe (version reports as "dev").
build:
    go build -o ccu.exe .

# Build a stripped, statically-linked release binary for the host platform,
# stamped with the current VERSION -> ccu.exe.
build-release:
    $env:CGO_ENABLED = "0"; go build -trimpath -ldflags "{{_LDFLAGS}}" -o ccu.exe .

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
fmt-check:
    @if (gofmt -l .) { Write-Error "unformatted files (run: just fmt)"; exit 1 }

# Run every check the way CI should.
ci: fmt-check vet test

# ---------------------------------------------------------------------------
# Release
# ---------------------------------------------------------------------------

# Print the current version (read from the VERSION file).
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
clean:
    -Remove-Item -Force ccu.exe -ErrorAction SilentlyContinue
    -Remove-Item -Recurse -Force dist, build, stage -ErrorAction SilentlyContinue
