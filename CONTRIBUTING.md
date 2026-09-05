# Contributing

Plain Go, no codegen, no generated files to keep in sync. `git clone`, then
`go test ./...` should be green on a fresh checkout — the fixture stacks under
`tests/` and the fake registry in `internal/registrytest` mean nothing in the
suite needs the network or a Docker daemon.

For what `ccu` does and how it is used, read the [README](README.md). This file
is about working on it.

## Build, test, lint

Everything goes through [`just`](https://github.com/casey/just); `just` on its
own lists the recipes.

| Recipe             | What it does                                          |
| ------------------ | ----------------------------------------------------- |
| `just run <args>`  | `go run . <args>` — the CLI straight from source       |
| `just build`       | dev binary (`ccu`, `ccu.exe` on Windows), version `dev`|
| `just test`        | `go test ./...`                                        |
| `just vet`         | `go vet ./...`                                         |
| `just fmt`         | `gofmt -w .`                                           |
| `just fmt-check`   | fails if anything is unformatted                       |
| `just lint`        | `golangci-lint run` (installs the pinned version once) |
| `just cover`       | prints coverage per package and in total               |
| `just ci`          | `fmt-check` + `vet` + `lint` + `test` — run before a PR|

The recipes work on Unix and on Windows. `just` only applies
`set windows-shell` on Windows, so the handful of recipes that need real shell
syntax exist twice, once `[unix]` and once `[windows]`; if you add one that
does more than call `go`, add both halves rather than assuming `sh`.

`golangci-lint` is configured in `.golangci.yml`. Where a rule is off, the
reason is written next to it — turn one back on by arguing with that comment,
not by sprinkling `nolint`. Nothing gates on coverage; `just cover` prints the
number when you want it.

## Package layout

The `ccu` entry point is `main.go` at the repo root. Everything else is
`internal/`:

| Package        | Owns                                                                        |
| -------------- | --------------------------------------------------------------------------- |
| `policy`       | the vocabulary of what a user recorded about an image: level caps, versioning scheme, reference and floating tags |
| `versioning`   | reading a tag as a version and ordering them — the `semver`, `loose` and `regex` schemes |
| `compose`      | finding compose files below a directory, the images they declare, the `.env` variables those references interpolate, and the Dockerfiles their services build |
| `registry`     | talking to OCI registries: listing tags, resolving a reference to a digest  |
| `check`        | resolving one file's images against a registry, and writing the new tags back |
| `scanner`      | walking a directory and checking every file found, streaming events as they resolve |
| `report`       | rendering a non-interactive run, pretty for a terminal and JSON Lines for a pipe |
| `tui`          | the Bubble Tea interface: model, key handling, rendering, applying           |
| `cli`          | parsing the command line and printing usage                                 |
| `modes`        | the non-interactive run: wire flags to a scan to a report, and report the outcome |
| `config`       | reading `.ccu.yaml` and the global config, and explaining how a setting resolved |
| `logger`       | the `slog` handler that colours ccu's own output                            |
| `buildinfo`    | the version stamped in at release time                                      |
| `registrytest` | a real HTTP registry for tests to talk to (test-only)                       |

## The layering rule

Imports run one way, from the entry point down to `policy`:

```
main → modes / tui → scanner → check → compose · registry · versioning → policy
```

`config` and `cli` sit off to the side: both are read early, and both depend on
`policy` (and `config` on `versioning`) so that a parsed config *is* a policy
rather than something the layers below have to translate.

The invariant that keeps this acyclic is that **`policy` imports nothing**, not
from this repo and not from outside it. It is only types and predicates —
`Level`, `Versioning`, `Image`, `Set`. Every other package needs to talk about
what the user asked for, so if `policy` ever imported one of them, that package
could no longer be described in `policy`'s terms and the graph would close a
loop. Keep it a leaf: no I/O, no registry calls, no YAML.

The same reasoning downwards: `check` may use `registry`, but `registry` must
never know what a check is. If you find yourself wanting an import that points
back up, the thing you want is usually a value passed *in* — that is what
`registry.Fetcher` and `check`'s injected policies already are.

`go list -f '{{join .Imports "\n"}}' ./internal/...` is the check; there is no
lint rule enforcing it.

## Tests

- **Table-driven with `testify`.** `assert` for anything the test can carry on
  past, `require` for the failures that make the rest meaningless. Most packages
  are already in this shape — copy the nearest neighbour.
- **`registrytest` instead of a mock.** `registrytest.Server` serves the
  slice of an OCI registry a check talks to, over real HTTP. Prefer it to a
  hand-written `Fetcher` stub whenever the thing under test is *about* registry
  behaviour — tag lists, digests, fallbacks — because a stub can only agree
  with whatever you already believed.
- **Fixtures under `tests/`.** Invented compose stacks and a Dockerfile,
  exercised end to end by `go test ./...`. Add a stack there rather than
  building YAML inline when the point is how a real file parses.
- **`t.TempDir()` for anything that writes.** `check` rewrites files and leaves
  `.ccu` backups; the config package writes YAML. Never let a test touch the
  fixtures in place — they are shared.
- **No network.** A test that would reach the real Docker Hub is a bug; CI runs
  on three OSes and would flake on rate limits alone.

## CI

`.github/workflows/ci.yml` runs on every push to `main` and every PR, across
**ubuntu-latest, windows-latest and macOS-latest**: `go vet`, then
`go test -race ./...`, then `go build ./...`. Windows is in the matrix because
`ccu` walks the filesystem and renders a TUI — both have path- and terminal
behaviour a Linux-only run never exercises. `-race` is there because the
scanner's worker pool and the registry probe queue share state across
goroutines, where a race would otherwise surface as a rare, unreproducible
failure.

Third-party actions are pinned to full commit SHAs, not tags. Keep it that way
when you touch a workflow.

## Releases

`VERSION` at the repo root is the single source of truth. It is stamped into
the binary with `-ldflags` by `just build-release`, and read back through
`internal/buildinfo`.

`just release [patch|minor|major|x.y.z]` bumps `VERSION`, commits, tags and
pushes. The **tag push** is what triggers `.github/workflows/release.yml`,
which builds the binaries for every platform and publishes them with a
`checksums.txt`. Nothing is released by merging to `main`.

`just set-version` stamps `VERSION` without committing, for when you want to
look at the diff first.

## Style

Comments explain **why**, not what — the code already says what it does. A
comment earns its place by recording a constraint, a registry quirk, or the
reason an obvious-looking alternative does not work. Everything in the source is
English: identifiers, comments, commit messages, log and error messages. Only
text an end user reads follows the product's language.

Commit messages are Conventional Commits with a lowercase, imperative subject:
`fix(policy): hand Versionings callers a copy, not the backing slice`.
