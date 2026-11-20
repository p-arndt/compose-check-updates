# Command line

[← README](../README.md) · [CLI](cli.md) · [Configuration](configuration.md) · [Guides](guides.md) · [TUI](tui.md) · [JSON output](json.md) · [Troubleshooting](troubleshooting.md)

## Commands

`cd` into the directory holding your stacks and run `ccu`. Everything below it is
scanned recursively. Nothing is written unless you ask — `A` in the TUI, `-u` for
`check` — and every modified file gets a `.ccu` backup beside it.

| Command | What it does |
| ------- | ------------ |
| `ccu` | The TUI: browse what's outdated, pick rows, apply. Default. |
| `ccu check` | One-shot report for scripts, cron and CI. No UI. |
| `ccu config` | Show the resolved configuration and where it came from |
| `ccu config -image <name>` | Explain how one image's settings were resolved |
| `ccu self-update` | Download, verify and replace the running binary |
| `ccu check-update` | Only report whether a newer ccu exists |
| `ccu help` / `ccu version` | — |

> [!TIP]
> Piped or redirected, `ccu` runs the `check` report (as JSON) instead of the
> TUI and says so on stderr — old cron and CI entries keep working.

## `ccu check` flags

```bash
ccu check              # report only (patch updates by default)
ccu check -f           # consider every newer version, not just patches
ccu check -u -r        # write the new tags, then restart the services
ccu check -d ./stacks  # scan a different directory
ccu check -image traefik  # check one image and nothing else
```

| Flag | Description | Default |
| ---- | ----------- | ------- |
| `-d` | Directory to scan | `.` |
| `-exclude` | Directories to exclude, comma-separated | none |
| `-image` | Only check images matching this name or pattern, repeatable and comma-separated | all |
| `-config` | Read this config file instead of searching for one | none |
| `-u` | Update the Compose files with the new image tags | `false` |
| `-r` | Restart the services after updating | `false` |
| `-f` | Full mode — consider every newer version, not just patches | `false` |
| `-major` / `-minor` / `-patch` | Only suggest that level | `-patch` |
| `-versioning` | Default tag-reading scheme: `semver` or `loose` | `semver` |
| `-min-age` | Only offer tags published at least this long ago, e.g. `7d` or `36h` | none |
| `-format` | Output format: `auto`, `pretty` or `json` | `auto` |
| `-pin-floating` | Pin floating tags (`latest`, `main`, …) to the digest they resolve to | `false` |
| `-dockerfiles` | Also check the base images of Dockerfiles built by a compose service | `true` |
| `-refresh` | Ignore the cached registry answers and ask every registry again | `false` |

Only `-d`, `-exclude`, `-image`, `-config`, `-pin-floating`, `-dockerfiles`,
`-versioning`, `-min-age` and `-refresh` also apply to the TUI — it picks levels in the UI instead and always resolves
every level.

**Exit codes:** `0` nothing to do · `1` updates available, not applied ·
`2` something failed. So a CI gate needs no parsing:

```bash
ccu check -f || echo "images are behind"
```
