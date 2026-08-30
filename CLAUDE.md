# CLAUDE.md

`ccu` — checks Docker Compose image tags against their registries and rewrites
them. Go, no codegen. [CONTRIBUTING.md](CONTRIBUTING.md) has the package table,
test conventions and release flow.

## Layering — a hard constraint, nothing enforces it

```
main → modes / tui → scanner → check → compose · registry · versioning → policy
```

Imports point down. `policy` imports nothing — not a repo package, not a
third-party one — so an import out of it closes a cycle. When you want an
upward import, pass a value in instead; `registry.Fetcher` is that pattern.
After a structural change: `go list -f '{{join .Imports "\n"}}' ./internal/...`.

## Verifying

`just ci` (fmt-check, vet, lint, test) is the gate. Anything touching the
scanner's worker pool or the registry probe queue: also `go test -race ./...`.

## Conventions

- **English everywhere in the source** — identifiers, comments, commits, log and
  error strings. Only end-user-facing copy follows the product's language.
- **Commits: Conventional Commits, lowercase imperative subject**, saying what
  changed and why it mattered — `fix(policy): hand Versionings callers a copy,
  not the backing slice`.
- **Comments explain WHY**: a constraint, a registry quirk, why the obvious
  alternative fails. The code already says what it does.
- `just release` owns `VERSION` and the tag. Do not edit either by hand.
