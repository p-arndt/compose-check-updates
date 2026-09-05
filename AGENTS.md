# AGENTS.md

`ccu` checks Docker Compose image tags against their registries and rewrites
them. Go, no codegen. [CONTRIBUTING.md](CONTRIBUTING.md) has the package table,
test conventions and release flow.

## Layering — a hard constraint, nothing enforces it

```
main → modes / tui → scanner → check → compose · registry · versioning → policy
```

Imports point down. `policy` imports nothing, so an import out of it closes a
cycle. When you want an upward import, pass a value in instead; `registry.Fetcher`
is that pattern. After a structural change:
`go list -f '{{join .Imports "\n"}}' ./internal/...`.

## Verifying

`just ci` (fmt-check, vet, lint, test) is the gate. Anything touching the
scanner's worker pool or the registry probe queue: also `go test -race ./...`.

## Changelog and releases

- Every user-facing change gets a fragment, committed with the change:
  `just note <added|changed|deprecated|removed|fixed|security> "<text>"`.
  Write it for the release page: what the user can do now, or what stopped
  going wrong. Refactors, CI and docs get none.
- `just release` owns `VERSION`, `CHANGELOG.md` and the tag. Never edit them by
  hand. CI takes the release notes from the tag message.

## Conventions

- English everywhere in the source; only end-user copy follows the product's
  language.
- Commits: Conventional Commits, lowercase imperative subject, saying what changed
  and why — `fix(policy): hand Versionings callers a copy, not the backing slice`.
- Comments explain WHY: a constraint, a registry quirk, why the obvious
  alternative fails.
