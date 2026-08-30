# Codebase health

Audit of `compose-check-updates`, started at commit `1fedc7b` (after the package
refactor) and worked through in two passes. Every task below is closed; this file
now serves as the record of what was found and the baseline the next audit measures
against.

## Scorecard

| Area | Before | After |
| ------------------------ | ----: | ----: |
| Structure & architecture |  92% |  94% |
| Code quality             |  86% |  95% |
| Tests                    |  80% |  92% |
| Documentation            |  75% |  92% |
| Dependency hygiene       |  85% |  92% |
| CI & tooling             |  65% |  95% |
| **Overall**              | **81%** | **93%** |

### How each score is measured

- **Structure** — `go list` import graph (no cycles), largest file, longest function.
- **Code quality** — `golangci-lint run` clean, `go vet` clean, `gofmt -l` empty, zero
  `TODO`/`FIXME`/`HACK`, zero `panic()` outside tests, zero unchecked errors, every
  exported declaration documented.
- **Tests** — `go test -race ./... -coverprofile`, currently 83.4%.
- **CI & tooling** — reading `.github/workflows/ci.yml` and running every `justfile`
  recipe on macOS.

## The invariants worth keeping

- **No import cycles, and `policy` is dependency-free** — of this repo's packages and
  of third-party code. It is the bottom of the graph on purpose.
- **No god files.** Largest is 393 LOC, longest function 93 lines.
- **Real integration seams, not mocks that agree with you.** `internal/registrytest`
  serves an actual HTTP registry; `tests/` holds fixture Compose stacks.
- **Actions pinned to full commit SHAs**, with the reasoning next to them, and
  Dependabot behind them so the pins do not rot.
- **CI is the same as `just ci`** — format, vet, lint, test. What passes locally
  passes in CI.
- **No coverage gate.** `just cover` prints the number when you want it; nothing
  fails a build over it. See "Deliberately not done".

## What each pass found

### Pass 1 — CI, coverage, one real bug

| Task | Result | Commit |
| --- | --- | --- |
| CI-1 | `justfile` was broken on macOS/Linux — `set windows-shell` only applies on Windows, but every recipe body was unconditional PowerShell, so `version`, `build-release`, `fmt-check`, `clean` and therefore `ci` all failed. Split into `[unix]`/`[windows]` pairs; `_LDFLAGS` dissolved into each `build-release`. | `dc84f3d` |
| CI-2 | CI now runs `go test -race`. The scanner's worker pool and the registry probe queue both share state across goroutines. | `39f21ad` |
| CI-4 | `macos-latest` added — it ships a release binary and is the primary dev platform. | `39f21ad` |
| CI-5 | Dependabot for `gomod` and `github-actions`. | `3deeba1` |
| T-1 | `logger` 0% → 100%. 188 LOC of ANSI escape handling, the kind that breaks silently into a smeared terminal. | `891a55c` |
| T-4 | `policy` 40% → 100%. | `a25e94e` |
| T-6 | `cli` 60% → 100%, `buildinfo` 0% → 100%. The usage test asserts every flag `Parse` accepts appears in the help text, rather than pinning the blob. | `9e12be8`, `b718084` |

**Q-2 — a real bug, found while writing the T-4 tests.** `policy.Versionings()` returned
the package-level slice directly, so any caller that sorted or wrote into the result
permanently changed which schemes `Valid()` accepts — and `Valid()` is what rejects a
bad `versioning:` key in user config. The `BuiltInFloatingTags` function directly
below it already copied. Now returns `slices.Clone`, with a regression test. (`0f1d91e`)

**Q-1 — a finding that turned out not to be one.** The first draft of this audit
flagged that only 16 of 35 `fmt.Errorf` calls use `%w`. Reading all 19 others: every
one constructs a *root* error — config validation, an unknown format name, a refusal
to write — with no cause to wrap. `errorlint` later agreed, reporting nothing. No
action; recorded so the next audit does not re-raise it.

### Pass 2 — the destructive path, the linter, and the clock

| Task | Result | Commit |
| --- | --- | --- |
| T-2 | `check` 82.7% → 87.9%. `Restart` and `composeCommand` — the path that shells out to `docker compose` against real stacks, the most destructive thing the tool does — went from 0% to 100%. A package-level `execCommand`/`lookPath` seam lets tests assert the argv without executing Docker: modern and legacy invocation, neither-on-PATH, `-f` plus `--build`, and non-zero exit surfacing as an error. | `6f976db`, `349fd96` |
| T-3 | `scanner` 60.3% → 88.5%. `ScanPins`, `CheckImage`, `checkerFor`, `checkFilePins` all 0% → 100%, driven against `registrytest`. | `196fd4e`, `f0a02c1` |
| Q-3 | `tui.handleKey` (115 lines) now only routes and delegates to `handleListKey`; `cli.Parse` (113 lines) went to 40, with `registerFlags`, `applySubcommand`, `inferMode`, `expandFlags` split out. No test was edited and coverage did not move — the refactor is behaviour-preserving. | `2c69a7f`, `44878c0` |
| D-1 | `CONTRIBUTING.md` and `CLAUDE.md` written against the verified import graph; the stale `justfile` header now lists the packages that exist. | `8a5ab4d`, `ce859b0` |
| D-2 | The last 9 undocumented exported declarations. | `1c2cc5e`, `6ff3453` |
| CI-3 | `golangci-lint` v2.13.2: errcheck, govet, staticcheck, ineffassign, unused, revive (18 rules), errorlint, gofmt + gci. Three revive rules and QF1001 are disabled with the reason written in the config rather than silenced with `nolint`. | `68db182`, `a22577d`, `78f77ad`, `1c68c1e` |
| T-5 | Suite from 10.19s to 6.82s (`tui` 9.37s → 3.31s). | `d79b7d3` |
| Q-4 | Import grouping, 28 test files, via the repo's own gci sections. | `1398e08` |

**Three genuinely dead functions**, found by the `unused` linter and deleted:
`Model.recordPin`, `Theme.rule`, `Theme.sideField`. (`a22577d`)

**A portability bug caught before it fired.** The new `scanner` test made a file
unreadable with `os.Chmod(f, 0000)`. On Windows that only touches the read-only bit —
the file stays readable — and `os.Getuid()` returns -1, so the existing root guard
would not have fired either. It would have failed the Windows job that pass 1 had just
made mandatory. Now guarded on `runtime.GOOS`, matching the pattern already used in
`internal/compose/files_test.go`. (`62be726`)

## Coverage

| Package | Before | After |
| ------------ | ----: | ----: |
| `main`       |  5.3% |  5.3% |
| `modes`      | 37.9% | 37.9% |
| `registry`   | 43.2% | 43.2% |
| `compose`    | 80.5% | 80.5% |
| `versioning` | 81.4% | 81.4% |
| `report`     | 83.0% | 83.0% |
| `config`     | 85.8% | 85.8% |
| `tui`        | 86.8% | 87.1% |
| `check`      | 82.7% | 87.9% |
| `scanner`    | 60.3% | 88.5% |
| `buildinfo`  |  0.0% | 100.0% |
| `cli`        | 60.2% | 100.0% |
| `logger`     |  0.0% | 100.0% |
| `policy`     | 40.0% | 100.0% |
| **total**    | **78.2%** | **83.4%** |

`main` stays low by design — it is argument plumbing around `run`, and `exitCode` (the
only logic in it) is tested. `modes` and `registry` are the honest remaining gaps.

## Deliberately not done

- **CI-6 — a coverage floor in CI.** Built, then removed on the maintainer's call
  (`8b6581e`, reverted). A percentage gate fails builds over test-shaped code rather
  than tested code, and the number here is already carried by a suite that covers the
  paths that matter. `just cover` still prints it on demand.

- **S-1 — splitting `internal/tui`.** It is the widest package (22 files, 5
  dependencies) and the natural seam is rendering versus state. But no file is over
  393 LOC and no function over 93 lines, so the split would be churn today. Revisit if
  the package keeps growing.

## Where to look next

1. **`internal/modes` (37.9%)** and **`internal/registry` (43.2%)** are the last real
   coverage gaps. `modes.Default` is the orchestrator every CLI run goes through.
2. **`tui/render_row.go:RowLine` (93 lines)** and **`tui/update.go:Update` (85)** are
   now the longest functions. Neither is urgent; both are flat dispatch.
3. **Coverage is a signal, not a target.** `just cover` when you want the number;
   83.4% is the baseline this audit left behind.
