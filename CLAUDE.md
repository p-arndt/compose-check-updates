# CLAUDE.md

`ccu` — checks Docker Compose image tags against their registries and rewrites
them. Go, no codegen. [CONTRIBUTING.md](CONTRIBUTING.md) has the full package
table, the test conventions and the release flow; this file is the short form.

## Layering — a hard constraint

```
main → modes / tui → scanner → check → compose · registry · versioning → policy
```

- `policy` imports **nothing** (no repo package, no third-party). Types and
  predicates only. Everything else describes the user's intent in its terms, so
  an import out of `policy` closes a cycle. Do not add one.
- Imports point down, never back up. `check` may use `registry`; `registry` must
  not know a check exists. When you want an upward import, pass a value in
  instead — `registry.Fetcher` and `check`'s injected `policy.Set` are that
  pattern already.
- `cli` and `config` sit off the chain and depend only on `policy`
  (`config` also on `versioning`), so a parsed config *is* a policy.
- `versioning` depends on `policy` — it is a layer above it, not a peer.
- Verify after a structural change:
  `go list -f '{{join .Imports "\n"}}' ./internal/...`. Nothing enforces this.

## Verifying a change

```
just ci          # fmt-check + vet + test — the gate
just test        # go test ./...
just run <args>  # the CLI from source
```

CI additionally runs `go test -race ./...` on ubuntu, windows and macOS, so
anything touching the scanner's worker pool or the registry probe queue should
be checked under `-race` locally too.

Tests: table-driven with `testify`, `registrytest.Server` for a real fake
registry (not a hand-written stub), fixtures under `tests/`, `t.TempDir()` for
anything that writes. No test may reach the network.

## Conventions

- **English everywhere in the source** — identifiers, comments, commit
  messages, log and error strings. Only end-user-facing copy follows the
  product's language.
- **Commits: Conventional Commits, lowercase imperative subject.** Real
  examples: `fix(policy): hand Versionings callers a copy, not the backing
  slice`, `test(cli): cover the usage text and the flag rejection paths`,
  `docs(registry): say what Client is`. Subject says what changed and why it
  mattered, not which files moved.
- **Comments explain WHY.** The code says what it does. Write a comment to
  record a constraint, a registry quirk, or why the obvious alternative fails.
  Do not narrate. Exported declarations get a doc comment; everything else earns
  one.
- Do not edit `VERSION` or cut a tag by hand — `just release` does both.
