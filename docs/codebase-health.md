# Codebase health

Audit of `compose-check-updates`, started at commit `1fedc7b` (after the package
refactor) and updated as the work lands. 9,737 LOC of production code across 97 Go
files.

This file records where the codebase stands and what it takes to close the gap.
Tick the boxes as the work lands; the scores are meant to be re-measured, not
trusted forever.

## Scorecard

| Area | Was | Now |
| ------------------------ | ---: | ---: |
| Structure & architecture |  92% |  92% |
| Code quality             |  86% |  90% |
| Tests                    |  80% |  88% |
| Documentation            |  75% |  75% |
| Dependency hygiene       |  85% |  92% |
| CI & tooling             |  65% |  85% |
| **Overall**              | **81%** | **88%** |

### How each score was measured

- **Structure** — `go list` import graph (no cycles), largest file 393 LOC, longest
  function 115 lines.
- **Code quality** — `go vet` clean, `gofmt -l` empty, zero `TODO`/`FIXME`/`HACK`,
  zero `panic()` outside tests, zero discarded errors, 420 of 429 exported
  declarations documented.
- **Tests** — `go test -race ./... -coverprofile` at 82.1% total, green on every
  package.
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
- **Supply chain discipline.** GitHub Actions are pinned to full commit SHAs with the
  reasoning written down next to them, and the Go version is read from `go.mod`.

## Task list

Ordered by payoff. Each task states the problem, the fix, and how to tell it worked.

### CI & tooling — 65% → 85%

- [x] **CI-1 — Fix the `justfile` on Unix.** *(`dc84f3d`)*
      `set windows-shell` only applies on Windows, but every recipe body was
      unconditional PowerShell. On macOS and Linux `just version`, `just build-release`,
      `just fmt-check` and `just clean` all failed, which also broke `just ci` because
      it depends on `fmt-check`:

      ```
      $ just version
      sh: -c: line 0: `(Get-Content VERSION -Raw).Trim()'
      error: recipe `version` failed on line 68 with exit code 2
      ```

      Fixed by splitting the affected recipes into `[unix]`/`[windows]` pairs. The
      `_LDFLAGS` variable could not carry a recipe attribute, so it was dissolved and
      the ldflags are now built inside each per-OS `build-release`. The Unix binary is
      `ccu`, Windows keeps `ccu.exe`; both were already gitignored.
      **Verified:** `just --list`, `version`, `fmt-check`, `clean`, `build-release`
      and `ci` all pass on macOS.

- [x] **CI-2 — Run the race detector in CI.** *(`39f21ad`)*
      The test step was plain `go test ./...` despite `scanner.walk` running a bounded
      worker pool over a semaphore channel and `registry/probe.go` running its own
      goroutine queue. Now `go test -race ./...`, and green.

- [x] **CI-4 — Add macOS to the test matrix.** *(`39f21ad`)*
      Was `[ubuntu-latest, windows-latest]` while macOS binaries ship in every release
      and it is the primary development platform. Now all three.

- [x] **CI-5 — Add `dependabot.yml`.** *(`3deeba1`)*
      Actions are deliberately pinned to SHAs, which is right — but a pinned SHA with
      no bot behind it just ages quietly. Weekly updates for `gomod` and
      `github-actions`.

- [ ] **CI-3 — Add `golangci-lint`.**
      There is still no linter config. `go vet` catches a fraction of what `errcheck`,
      `revive`, `staticcheck`, `ineffassign` and `unused` catch. Add `.golangci.yml`
      with that set and wire it into CI and the `just ci` recipe. Left out of the first
      pass because a new linter on an existing codebase needs its findings triaged
      rather than bulk-silenced.
      **Done when:** `golangci-lint run` is green and runs on every PR.

- [ ] **CI-6 — Enforce a coverage floor.**
      Coverage is 82.1% and nothing stops it from sliding. Upload the profile and fail
      the build below a threshold — set it at the current number so it can only go up.
      Do this last, once the number has stopped moving.
      **Done when:** deleting a covered test fails CI.

### Tests — 80% → 88%

Per-package coverage, worst first:

| Package | Was | Now |
| ------------ | ----: | ----: |
| `main`       |  5.3% |  5.3% |
| `modes`      | 37.9% | 37.9% |
| `registry`   | 43.2% | 43.2% |
| `scanner`    | 60.3% | 60.3% |
| `compose`    | 80.5% | 80.5% |
| `versioning` | 81.4% | 81.4% |
| `check`      | 82.7% | 82.7% |
| `report`     | 83.0% | 83.0% |
| `config`     | 85.8% | 85.8% |
| `tui`        | 86.8% | 86.8% |
| `buildinfo`  |  0.0% | 100.0% |
| `cli`        | 60.2% | 100.0% |
| `logger`     |  0.0% | 100.0% |
| `policy`     | 40.0% | 100.0% |
| **total**    | **78.2%** | **82.1%** |

- [x] **T-1 — Test `internal/logger`.** *(`891a55c`, 0% → 100%)*
      188 LOC of ANSI escape-sequence handling — `colorizeChangedSegments`,
      `visibleLen`, `padRight` — the kind of code that breaks silently and shows up as
      a smeared terminal. `logger.go` was not modified; `Handle` turned out to be
      reachable through the `*os.File` seam in `NewCustomHandler`.

- [x] **T-4 — Cover the `policy` predicates.** *(`a25e94e`, 40% → 100%)*
      `policy` is the dependency-free core that `check`, `versioning`, `config`, `cli`,
      `scanner`, `report` and `tui` all import, so a wrong predicate is a wrong answer
      everywhere. `Level.Allows` is covered across the full level-vs-level matrix.
      **This is where the audit found a real bug — see Q-2.**

- [x] **T-6 — Cover `cli.usage` and `buildinfo.String`.** *(`9e12be8`, `b718084`)*
      `cli` went 60.2% → 100%, `buildinfo` 0% → 100%. The usage test asserts that every
      flag `Parse` accepts appears in the help text, rather than pinning the blob
      byte-for-byte, so a reworded description does not fail it.

- [ ] **T-2 — Cover `check/apply.go` `Restart` and `composeCommand` (0%).**
      This is the path that shells out to `docker compose` against real stacks — the
      most destructive thing the tool does, and the least tested. Needs the command
      runner injected so the invocation can be asserted without executing Docker, which
      is why it was not in the quick pass.
      **Done when:** the argv handed to `docker compose` is asserted for both the
      `docker compose` and legacy `docker-compose` cases.

- [ ] **T-3 — Cover the scanner's pin path.**
      `scanner.ScanPins`, `scanner.CheckImage`, `scanner.checkerFor` and
      `checkFilePins` are all at 0% while the package sits at 60.3%.
      `registrytest.Server` plus the `tests/` fixtures already give everything needed.
      **Done when:** `scanner` is above 75%.

- [ ] **T-5 — Add `t.Parallel()`.**
      There is still not one `t.Parallel()` in the suite; `tui` alone takes ~10 s. Most
      tests are pure table-driven cases and the filesystem ones already use
      `t.TempDir()`. Caveat found during T-6: `internal/cli` tests swap
      `flag.CommandLine` and `os.Args`, so that package must stay serial.
      **Done when:** the suite runs in under half the current wall time, still green
      under `-race`.

### Code quality — 86% → 90%

- [x] **Q-1 — ~~Wrap errors consistently.~~ Not a defect.**
      The original audit flagged that only 16 of 35 `fmt.Errorf` calls use `%w`.
      Reading all 19 of the others: every one constructs a *root* error — config
      validation, an unknown format name, a refusal to write — with no cause to wrap.
      Adding `%w` there would have nothing to point at. No action; recorded so the
      next audit does not re-raise it.

- [x] **Q-2 — `policy.Versionings` handed out its backing slice.** *(`0f1d91e`)*
      Found while writing the T-4 tests. `Versionings()` returned the package-level
      `versionings` slice directly, so any caller that sorted or wrote into the result
      permanently changed which schemes `Valid()` accepts — and `Valid()` is what
      rejects a bad `versioning:` key in user config. `BuiltInFloatingTags` next to it
      already copied. Now returns `slices.Clone`, with a regression test.

- [ ] **Q-3 — Split the two longest functions.**
      `tui/input.go:handleKey` is 115 lines and `cli/flags.go:Parse` is 112. Both are
      flat dispatch, so neither is urgent — but `handleKey` is the single place where
      every TUI interaction is decided, and the file most likely to keep growing. Split
      it per mode (list / sidebar / edit).
      **Done when:** no function exceeds ~80 lines.

- [ ] **Q-4 — Tidy import grouping.**
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
      There is no file describing the layout, the invariants, or how to run things. The
      `justfile` header is the only architectural note in the repo, and it is out of
      date — it lists "scanner, modes, tui, registry lookups, buildinfo" while the tree
      now also has `check`, `policy`, `compose`, `versioning`, `cli`, `config` and
      `report`.
      **Done when:** a new contributor can find the layer boundaries without reading
      the import graph.

- [ ] **D-2 — Document the remaining 9 undocumented exported declarations.**
      420 of 429 already carry a doc comment; finish the set so `revive`'s `exported`
      rule can be turned on without noise.

## What is left, in order

1. **T-2** — the `docker compose` shell-out is still untested, and it is the most
   destructive path in the tool.
2. **T-3** — the scanner's pin path.
3. **CI-3** — `golangci-lint`, with its first findings triaged rather than silenced.
4. **CI-6** — set the coverage floor once the number stops moving.
5. **D-1, D-2, Q-3, Q-4, T-5** — quality of life.
6. **S-1** — only if `internal/tui` keeps growing.
