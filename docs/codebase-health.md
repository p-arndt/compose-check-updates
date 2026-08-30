# Codebase health

Audit of `compose-check-updates` at commit `1fedc7b` (after the package refactor).
9,737 LOC of production code across 97 Go files, 9,914 LOC of tests.

This file records where the codebase stands and what it takes to close the gap.
Tick the boxes as the work lands; the scores are meant to be re-measured, not
trusted forever.

## Scorecard

| Area | Score |
| ------------------------ | ----: |
| Structure & architecture |  92% |
| Code quality             |  86% |
| Tests                    |  80% |
| Documentation            |  75% |
| Dependency hygiene       |  85% |
| CI & tooling             |  65% |
| **Overall**              |  **81%** |

### How each score was measured

- **Structure** — `go list` import graph (no cycles), largest file 393 LOC, longest
  function 115 lines.
- **Code quality** — `go vet` clean, `gofmt -l` empty, zero `TODO`/`FIXME`/`HACK`,
  zero `panic()` outside tests, zero discarded errors, 420 of 429 exported
  declarations documented.
- **Tests** — `go test ./... -coverprofile` at 78.2% total, `go test -race` clean on
  every concurrent package.
- **CI & tooling** — reading `.github/workflows/ci.yml` and running the `justfile`
  recipes on macOS.

## What is already good

Worth stating plainly, so nobody "fixes" it later:

- **No import cycles, and `policy` is a dependency-free core.** The layering reads
  `main → modes/tui → scanner → check → compose/registry/versioning → policy`.
- **No god files.** The refactor took `tui/model.go` from 889 LOC to 197 and
  `tui/update.go` from 739 to 192.
- **Real integration seams.** `internal/registrytest` serves an actual HTTP registry,
  and `tests/` holds fixture Compose stacks, so the scanner is exercised end to end
  rather than mocked into agreement.
- **`go test -race` passes** on `scanner`, `registry`, `check` and `tui` today. The
  concurrency is correct; it is just unguarded (see CI-1).
- **Supply chain discipline.** GitHub Actions are pinned to full commit SHAs with the
  reasoning written down next to them, and the Go version is read from `go.mod`.

## Task list

Ordered by payoff. Each task states the problem, the fix, and how to tell it worked.

### CI & tooling — 65%

The weakest area, and the only one holding a live bug.

- [ ] **CI-1 — Fix the `justfile` on Unix.**
      `set windows-shell` only applies on Windows, but every recipe body is
      unconditional PowerShell. On macOS and Linux `just version`, `just build-release`,
      `just fmt-check` and `just clean` all fail, which also breaks `just ci` because it
      depends on `fmt-check`:

      ```
      $ just version
      sh: -c: line 0: `(Get-Content VERSION -Raw).Trim()'
      error: recipe `version` failed on line 68 with exit code 2
      ```

      Rewrite the bodies in POSIX `sh` and add Windows variants using `just`'s
      `[windows]` / `[unix]` recipe attributes. `_LDFLAGS` needs the same treatment —
      it interpolates `Get-Content` and `Get-Date`.
      **Done when:** `just ci` passes on macOS and on Windows.

- [ ] **CI-2 — Run the race detector in CI.**
      `scanner.walk` runs a bounded worker pool over a semaphore channel and
      `registry/probe.go` runs its own goroutine queue, but CI only runs plain
      `go test ./...`. It is clean locally, so this is about keeping it that way.
      Change the test step to `go test -race ./...`.
      **Done when:** the CI test step carries `-race` and is green.

- [ ] **CI-3 — Add `golangci-lint`.**
      There is no linter config at all. `go vet` catches a fraction of what
      `errcheck`, `revive`, `staticcheck`, `ineffassign` and `unused` catch. Add
      `.golangci.yml` with that set and wire it into CI and the `just ci` recipe.
      **Done when:** `golangci-lint run` is green and runs on every PR.

- [ ] **CI-4 — Add macOS to the test matrix.**
      The matrix is `ubuntu-latest` + `windows-latest`, yet macOS binaries ship in
      every release and it is the primary development platform. Add `macos-latest`.
      **Done when:** three OSes are green on a PR.

- [ ] **CI-5 — Add `dependabot.yml`.**
      Actions are deliberately pinned to SHAs, which is right — but a pinned SHA with
      no bot behind it just ages quietly. Configure weekly updates for both
      `gomod` and `github-actions`.
      **Done when:** `.github/dependabot.yml` exists and the first PRs arrive.

- [ ] **CI-6 — Enforce a coverage floor.**
      Coverage is 78.2% and nothing stops it from sliding. Upload the profile and
      fail the build below a threshold — start at the current number so it can only
      go up.
      **Done when:** dropping a covered test fails CI.

### Tests — 80%

Per-package coverage, worst first:

| Package | Coverage |
| ----------- | -------: |
| `logger`    |    0.0% |
| `main`      |    5.3% |
| `modes`     |   37.9% |
| `policy`    |   40.0% |
| `registry`  |   43.2% |
| `cli`       |   60.2% |
| `scanner`   |   60.3% |
| `compose`   |   80.5% |
| `versioning`|   81.4% |
| `check`     |   82.7% |
| `report`    |   83.0% |
| `config`    |   85.8% |
| `tui`       |   86.8% |
| **total**   | **78.2%** |

- [ ] **T-1 — Test `internal/logger` (188 LOC, 0%).**
      `colorizeChangedSegments`, `visibleLen` and `padRight` are ANSI escape-sequence
      string handling — exactly the kind of code that breaks silently and shows up as
      a smeared terminal. `ansiEscape` is already a package-level regexp, so the
      functions are directly testable.
      **Done when:** `logger` is above 70%, including a case where a colourised string
      is padded to a width.

- [ ] **T-2 — Cover `check/apply.go` `Restart` and `composeCommand` (0%).**
      This is the path that shells out to `docker compose` against real stacks — the
      most destructive thing the tool does, and the least tested. Inject the command
      runner so the invocation can be asserted without executing Docker.
      **Done when:** the argv handed to `docker compose` is asserted for both the
      `docker compose` and legacy `docker-compose` cases.

- [ ] **T-3 — Cover the scanner's pin path.**
      `scanner.ScanPins`, `scanner.CheckImage`, `scanner.checkerFor` and
      `checkFilePins` are all at 0% while the surrounding package sits at 60.3%.
      `registrytest.Server` plus the `tests/` fixtures already give everything needed.
      **Done when:** `scanner` is above 75%.

- [ ] **T-4 — Cover the small `policy` predicates.**
      `Level.Valid`, `Level.String`, `Level.Allows`, `Versioning.Valid`,
      `Versioning.String`, `Image.IsZero` and `BuiltInFloatingTags` are all at 0%.
      These are cheap table-driven tests and `policy` is the package everything else
      depends on, so a wrong `Allows` is a wrong answer everywhere.
      **Done when:** `policy` is above 85%.

- [ ] **T-5 — Add `t.Parallel()`.**
      There is not one `t.Parallel()` in the suite. `tui` alone takes 9.3 s and the
      full run is ~28 s, which is long enough that people stop running it locally.
      Most tests are pure table-driven cases with no shared state; the ones touching
      the filesystem already use `t.TempDir()`.
      **Done when:** the full suite runs in under half the current wall time, still
      green under `-race`.

- [ ] **T-6 — Cover `cli.usage` and `buildinfo.String` (both 0%).**
      Low stakes, but they are user-facing output and a golden test is a few lines.

### Code quality — 86%

- [ ] **Q-1 — Wrap errors consistently.**
      Only 16 of 35 `fmt.Errorf` calls use `%w` (46%). The rest flatten the error into
      a string, so `errors.Is` and `errors.As` stop working across that boundary —
      which matters most for the registry paths, where distinguishing "not found" from
      "auth failed" from "network down" drives what the user is told.
      **Done when:** every `fmt.Errorf` that carries a cause uses `%w`, enforced by the
      `errorlint` linter from CI-3.

- [ ] **Q-2 — Split the two longest functions.**
      `tui/input.go:handleKey` is 115 lines and `cli/flags.go:Parse` is 112. Both are
      flat dispatch, so neither is urgent — but `handleKey` is the single place where
      every TUI interaction is decided, and it is the file most likely to keep growing.
      Split it per mode (list / sidebar / edit).
      **Done when:** no function exceeds ~80 lines.

- [ ] **Q-3 — Tidy import grouping.**
      `internal/modes/default_test.go` mixes stdlib and module imports in one block.
      `gci` or `goimports -local` via CI-3 fixes this mechanically.

### Structure — 92%

Nothing is wrong here. One optional follow-up:

- [ ] **S-1 — Consider splitting `internal/tui` (optional).**
      It is 22 files and depends on 5 other packages — by far the widest package. The
      natural seam is rendering (`render*.go`, `styles.go`, `bar.go`, `view.go`) versus
      state (`model.go`, `update.go`, `input.go`, `cursor.go`, `rows.go`). Only worth
      doing if the package keeps growing; the file sizes are healthy today.

### Documentation — 75%

- [ ] **D-1 — Add `CLAUDE.md` / `CONTRIBUTING.md`.**
      There is no file describing the layout, the invariants, or how to run things.
      The `justfile` header is currently the only architectural note in the repo, and
      it is out of date — it lists "scanner, modes, tui, registry lookups, buildinfo"
      while the tree now also has `check`, `policy`, `compose`, `versioning`, `cli`,
      `config` and `report`.
      **Done when:** a new contributor can find the layer boundaries without reading
      the import graph.

- [ ] **D-2 — Document the remaining 9 undocumented exported declarations.**
      420 of 429 already carry a doc comment; finish the set so `revive`'s
      `exported` rule can be turned on without noise.

## Suggested order

1. **CI-1** — a real bug on the development platform, blocks `just ci`.
2. **CI-2, CI-3, CI-4, CI-5** — one PR of infrastructure; every later change lands
   behind a stronger gate.
3. **T-1, T-2, T-3, T-4** — the coverage holes that sit on destructive or
   widely-depended-on code.
4. **Q-1** — mechanical once `errorlint` is reporting.
5. **CI-6** — set the floor last, once the number has stopped moving.
6. **T-5, Q-2, D-1, D-2** — quality of life.
7. **S-1** — only if `internal/tui` keeps growing.
