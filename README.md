<p align="center">
  <img src="./logo.png" alt="Compose-Check-Updates Logo" width="200">
</p>

<h1 align="center">Compose-Check-Updates</h1>

<p align="center">
  <strong>Keep your Docker Compose image tags up to date — like <code>npm-check-updates</code>, but for <code>compose.yaml</code>.</strong>
</p>

<p align="center">
  <a href="https://github.com/p-arndt/compose-check-updates/actions/workflows/ci.yml"><img src="https://github.com/p-arndt/compose-check-updates/actions/workflows/ci.yml/badge.svg" alt="CI"></a>
  <a href="https://github.com/p-arndt/compose-check-updates/releases/latest"><img src="https://img.shields.io/github/v/release/p-arndt/compose-check-updates?label=release" alt="Latest release"></a>
  <a href="LICENSE"><img src="https://img.shields.io/github/license/p-arndt/compose-check-updates" alt="MIT license"></a>
  <img src="https://img.shields.io/badge/platforms-linux%20%7C%20macOS%20%7C%20windows-informational" alt="Platforms">
</p>

```bash
ccu              # open the TUI and pick what to update
ccu check        # just print what's outdated
ccu check -u     # print and write the new tags
```

Point it at a directory, it scans every Compose file below it, asks each registry
what's newer, and — if you want — rewrites the tags for you. One static binary,
no daemon, no runtime.

<p align="center">
  <img src="./assets/demo.gif" alt="ccu scanning four Compose stacks, retargeting traefik from a major bump to the latest 2.11.x, and writing the new tags" width="900">
</p>

Four stacks at once, `traefik` pointed at the newest `2.11.x` instead of the `v3`
it was offered — and a `.ccu` backup next to every file that was touched.

## Install

Grab the binary for your platform from
[Releases](https://github.com/p-arndt/compose-check-updates/releases):

```bash
curl -fsSLO https://github.com/p-arndt/compose-check-updates/releases/latest/download/ccu-linux-amd64
mv ccu-linux-amd64 ccu && chmod +x ccu && sudo mv ccu /usr/local/bin/
ccu version
```

Builds for linux/macOS/windows on `amd64`, `arm64`, `arm` and `386`; Windows just
needs the `.exe` on your `PATH`. Every release ships a `checksums.txt`.
Later, `ccu self-update` replaces the binary in place.

## Usage

`cd` into the directory holding your stacks and run `ccu`. Everything below it is
scanned recursively.

| | |
| ---------------- | ---------------------------------------------------------- |
| **`ccu`**        | The TUI. Browse what's outdated, pick rows, apply. Default. |
| **`ccu check`**  | One-shot report for scripts, cron and CI. No UI.            |

Nothing is written unless you ask — `A` in the TUI, `-u` for `check` — and every
modified file gets a `.ccu` backup beside it.

### The TUI

Updates grouped per Compose file, colour-coded, streaming in as registries answer.
**Arrows move; `space`/`enter` act on whatever has the focus.** `tab` reaches the
detail column on an image and the settings bar anywhere else. `A` applies, `?`
shows every key.

```
 show ‹ all ›   target ‹ major ›   [ issues 1 ]   [ apply 2 ]
```

You decide per row which version gets written, so a major bump never sneaks in.
Afterwards `ccu` offers to `docker compose up -d` the affected files.

<details>
<summary><strong>All keys</strong></summary>

| Key                | Action                                            |
| ------------------ | ------------------------------------------------- |
| `↑`/`↓` or `k`/`j` | Move the cursor. At the top of the list, `↑` carries on into the bar; on the bar, `↓` comes back |
| `pgup`/`pgdn`      | Page up / down (`home`/`end` for first / last)    |
| `←`/`h`, `→`/`l`   | On a header: collapse / expand. On an image: open the details. In the details column: previous / next option. On the bar: previous / next stop |
| `space` / `enter`  | Act on what has the focus: select the row, step a setting, press a button |
| `-`                | Step the focused setting backwards                |
| `z`                | Fold/unfold the node under the cursor             |
| `C` / `E`          | Collapse all / expand all                         |
| `a` / `n`          | Select / deselect everything under the cursor     |
| `ctrl+a` / `ctrl+n`| Select / deselect the whole list                  |
| `f`                | Cycle the display filter (which rows are shown)   |
| `t`                | Cycle the target level for **all** rows           |
| `p`                | List or hide the floating tags                    |
| `tab`              | On an image: the details column (`tab` or `esc` returns). Otherwise: the top bar |
| `shift+tab`        | Step back along the bar                           |
| `m`                | The top bar, from anywhere; again for the next stop |
| `i`                | Show the issues logged during the scan            |
| `A`                | Apply the **selected** updates                    |
| `u`                | Apply **only the highlighted row**                |
| `y` / `n`          | Answer the restart prompt                         |
| `esc`              | Back out of whatever has the keyboard (never quits) |
| `?` / `q`          | Help / quit                                       |

On a terminal too narrow for two columns the detail column moves *below* the list
rather than disappearing — the per-image target and cap have no keys of their own.

</details>

<details>
<summary><strong>Filter vs. target</strong> — which version actually gets written</summary>

`show` only decides which rows are _visible_; `target` decides which version
actually gets _written_. Both sit on the top bar and both have a key (`f` and
`t`); the row's own target lives in the detail column. The target defaults to
`major`, so out of the box you are offered the highest available version. At
target `minor`, an image on `traefik:v2.9.3` that has `3.7.8` available re-points
to the latest `2.11.x` instead; at `patch`, to `2.9.4`.

The sidebar only offers the levels an image actually has — the `(+2)` after a
version means two other levels exist. A row with nothing at the current target
shows as `[-] … no patch update` and cannot be applied.

The TUI always resolves **all** update levels, regardless of `-patch`, `-minor`,
`-major` or `-f`. Those flags govern `ccu check` only.

</details>

> [!TIP]
> The TUI needs a real terminal. Piped or redirected, `ccu` runs the `check`
> report instead (as JSON) and says so on stderr — so old cron and CI entries
> keep working.

### `ccu check` — the non-interactive report

```bash
ccu check              # report only (patch updates by default)
ccu check -u -r        # write the new tags, then restart the services
ccu check -f           # consider every newer version, not just patches
ccu check -d ./stacks  # scan a different directory
```

| Flag       | Description                                              | Default |
| ---------- | -------------------------------------------------------- | ------- |
| `-d`       | Directory to scan                                        | `.`     |
| `-exclude` | Directories to exclude, comma-separated                  | none    |
| `-config`  | Read this config file instead of searching for one       | none    |
| `-u`       | Update the Compose files with the new image tags         | `false` |
| `-r`       | Restart the services after updating                      | `false` |
| `-f`       | Full mode — consider every newer version, not just patches | `false` |
| `-major` / `-minor` / `-patch` | Only suggest that level                | `-patch`|
| `-format`  | Output format: `auto`, `pretty` or `json`                | `auto`  |
| `-pin-floating` | Pin floating tags (`latest`, `main`, …) to the digest they resolve to | `false` |
| `-dockerfiles` | Also check the base images of Dockerfiles built by a compose service | `true` |

Only `-d`, `-exclude`, `-config`, `-pin-floating` and `-dockerfiles` also apply to the TUI, which
picks levels in the UI instead.

**Exit codes:** `0` nothing left to do · `1` updates available, not applied ·
`2` something failed. So CI gates without parsing anything:

```bash
ccu check -f || echo "images are behind"
```

<details>
<summary><strong>Output format</strong> — JSON Lines when piped</summary>

On a terminal the report is the aligned, colour-coded listing; in a pipe it is
**JSON Lines** — one object per line, written as the scan resolves, so `jq` reads
it streaming. `--format=pretty` / `--format=json` force either one.

```json
{"kind":"update","image":"library/traefik","reference":"traefik:v2.9.3","services":["proxy"],"file":"proxy/compose.yaml","current":"v2.9.3","latest":"v3.2.0","level":"major","targets":{"minor":"v2.11.4","major":"v3.2.0"}}
{"kind":"error","file":"data/docker-compose.yml","error":"fetching tags: 429"}
```

| Key | On | Meaning |
| --- | -- | ------- |
| `kind` | every line | `update` or `error` — dispatch on this |
| `image` / `reference` | update | the image name, and the reference as the Compose file writes it |
| `services` | update | the Compose services that declare it; a list, because identical references are reported once |
| `file` | both | the file involved — the Compose file, or the Dockerfile when the update sits on a `FROM` line |
| `compose_file` | Dockerfile updates | the Compose file that builds it, i.e. the one to hand `docker compose -f` |
| `current` / `latest` | update | the tag now, and the one this run picked |
| `level` | update | `major`, `minor`, `patch`, `digest` or `pin` |
| `current_digest` / `latest_digest` | digest-pinned images | only present when the digest actually moved |
| `targets` | update | the tag available at each level, so you can pick a different one |
| `cap` | capped images | the ceiling recorded in your config |
| `applied` / `restarted` | with `-u` / `-r` | whether the write or restart succeeded — `false` says it was asked for and failed |
| `error` | error | the failure, as text |

Only the report goes to stdout — warnings, lookup failures and the "a newer ccu
exists" notice go to **stderr**, so a pipe stays parseable.

</details>

### All commands

```bash
ccu                # the TUI
ccu check          # the non-interactive report
ccu self-update    # download, verify and replace the running binary
ccu check-update   # only report whether a newer ccu exists
ccu config         # show the resolved configuration and where it came from
ccu help / version
```

A `ccu check` run also checks **at most once every 24 hours** whether a newer
release exists and prints one line to stderr. It never installs anything by
itself; `CCU_NO_UPDATE_CHECK=1` turns the check off.

## Configuration

Directories you never want scanned, written down once:

```yaml
# .ccu.yaml
exclude:
  - node_modules
  - services/legacy

# Offer floating tags (latest, main, …) the digest they resolve to.
pin_floating: true

# Check the base images of Dockerfiles built by a compose service. On by default.
dockerfiles: false

# How tags are read as versions, for images whose tags are not semver.
versioning: semver
images:
  nousresearch/hermes-agent:
    versioning: loose
  acme/dated:
    versioning: regex
    versioning_pattern: '^(?P<major>\d{4})-(?P<minor>\d{2})-(?P<patch>\d{2})$'
```

`~/.config/ccu/config.yaml` for preferences across every project, `.ccu.yaml` in
the scan root or any parent for settings that travel with the stacks. Neither is
required; project layers over global, `-exclude` over both, and the lists are
**unioned** rather than replaced. `backup` matches that directory name at any
depth, `services/legacy` only that path, `/mnt/backups` that absolute location —
all three with `*` wildcards. `ccu config` shows what was read.

When a setting does not seem to arrive, ask about the one image:

```bash
ccu config -image library/traefik
```

It prints the versioning scheme and the cap in effect **and the layer that
produced each** — the per-image entry, `-versioning`, a global `versioning:`, or
the built-in default — naming the file it was read from. And if nothing matched,
it says so: the key is the image name **without tag or digest**, the way `ccu`
reports it, so an entry spelled `traefik` never reaches `library/traefik`. An
entry that nearly matches what you typed is offered as a hint:

```
No config entry names "traefik:1.2".
  Lookup is exact and on the image name without tag or digest, as ccu
  reports it — e.g. "library/traefik", not "traefik:1.2".
  Did you mean "library/traefik"? (images.library/traefik in /srv/stacks/.ccu.yaml)
```

## Registries

Any OCI registry works — Docker Hub, GHCR, Quay, Harbor, ECR, a self-hosted
`registry:2`. Private repos need no setup: `ccu` reads the credentials
`docker login` already stored. Logging in also lifts Docker Hub's anonymous rate
limit, which starts to matter past a few dozen images.

## Images you build yourself

A service with a `build:` has no image tag for `ccu` to check — the tag that
decides what it runs sits on the `FROM` line of its Dockerfile. So those are
scanned too:

```yaml
services:
  keycloak:
    build:
      context: "./"
      dockerfile: Dockerfile
```

```dockerfile
-FROM quay.io/keycloak/keycloak:26.0.7 AS builder
+FROM quay.io/keycloak/keycloak:26.7.2 AS builder
...
-FROM quay.io/keycloak/keycloak:26.0.7
+FROM quay.io/keycloak/keycloak:26.7.2
```

The Dockerfile shows up in the list under its own path, next to the compose file
that builds it, and behaves like any other row: levels, caps, targets, a `.ccu`
backup beside the file it rewrites. Two details are specific to it:

- **Every stage moves together.** A multi-stage build usually names its base once
  as the builder and once as the runtime, and both are rewritten as one update —
  a runtime left a release behind its builder is a broken image, not a partial one.
- **A restart rebuilds.** `docker compose up -d --build` for these, since a new
  base image reaches the container only through a build.

Skipped, because no registry can answer for them: `FROM scratch`, a stage
referring to an earlier one (`FROM builder`), a reference assembled from build
args (`FROM ${BASE}`), `dockerfile_inline:`, and contexts that are not a local
path. `-dockerfiles=false` (or `dockerfiles: false` in the config) turns the
whole thing off.

## Versioning schemes

Docker tags are a wild west: plenty of images publish something that is a version
but not a *semantic* one. `nousresearch/hermes-agent`, for instance, tags by date
and sometimes rebuilds the same day — `v2026.7.7` and `v2026.7.7.2` side by side.
A fourth segment is not semver, so by default `ccu` does not read those tags as
versions at all and falls back to comparing digests.

Rather than loosen the rule for every image, the rule is a setting:

| Scheme          | Reads                                                                  |
| --------------- | ---------------------------------------------------------------------- |
| `semver`        | up to three segments, no leading zeros — the default, unchanged        |
| `loose`         | up to six numeric segments, leading zeros, any suffix                  |
| `regex`         | whatever a pattern you write next to it names — per image only         |

Turn it on for the image that needs it:

```yaml
# .ccu.yaml
images:
  nousresearch/hermes-agent:
    versioning: loose
```

Now `v2026.7.7` orders before `v2026.7.7.2` before `v2026.7.30`, and `ccu` offers
the update instead of giving up. The first three segments stay **major, minor and
patch**; anything past the third orders the version but names no level of its own,
so a fourth segment advancing is reported as a **patch**. The level filters, the
caps and the TUI are unaffected.

The suffix rule holds either way: `3.19-alpine` only ever moves to another
`-alpine` tag, and `2026.7.7-cuda` only to another `-cuda` one.

Three places can set it, most specific first:

```yaml
images:
  nousresearch/hermes-agent:
    versioning: loose   # 1. this image, wherever it is scanned
```

```bash
ccu check --versioning=loose   # 2. every image, this run only
```

```yaml
versioning: loose   # 3. every image, in .ccu.yaml or ~/.config/ccu/config.yaml
```

An entry naming an image outranks both defaults, **including the flag** — a flag
meant as a quick try should not silently undo a preference written down on
purpose. `ccu config` shows what was resolved, and `ccu config -image <name>`
which of the three decided it.

> [!TIP]
> If `ccu` says `no tag matches the newest digest` for an image whose tags look
> like versions to you, `versioning: loose` is what it is asking for.

### Tags no fixed rule can read: `regex`

Some repositories tag by dashed date — `2024-01-01`. Both schemes above read that
as release **2024** with `-01-01` mistaken for a prerelease, which orders
`2024-12-31` before `2024-02-01`: the alphabet, not the calendar. For those, say
what a tag looks like:

```yaml
# .ccu.yaml
images:
  acme/dated:
    versioning: regex
    versioning_pattern: '^(?P<major>\d{4})-(?P<minor>\d{2})-(?P<patch>\d{2})$'
```

The pattern is a Go regular expression with **named groups**, `(?P<name>…)`. Four
of them carry numbers — `major`, `minor`, `patch` and `build` — plus `suffix` for
whatever trails them, separator included. A group you leave out is `0`, so a
pattern naming only `major` is enough; a group named anything else is ignored,
which lets you name the parts you only need to match. The four order exactly as
under `loose`: the first three are the levels, and `build` moving on its own is
reported as a **patch**.

The pattern has to describe the **whole** tag — it is anchored, so a date buried
in `build-2024-01-01-x` is not one — and a tag it does not match is simply not a
version, exactly as under the other schemes: `ccu` falls back to comparing
digests for it.

`regex` is a per-image scheme. There is no global `versioning: regex` and no
`--versioning=regex`, because a pattern that fits one repository's tags is
meaningless for the next one's. The pattern is checked when the config is read,
so a typo fails the run and names the image rather than quietly reading no tags:

```console
$ ccu config
ERROR  Error reading config  error=/repo/.ccu.yaml: image "acme/dated": versioning_pattern: "^(?P<major>\\d+$" is not a valid regular expression: error parsing regexp: missing closing ): `^(?P<major>\d+$`
```

A pattern set on an image that is not on `regex`, and `versioning: regex` with no
pattern at all, fail the same way. `ccu config` lists the pattern beside the
scheme for every image that has one.

## Images without semver tags

Some images are pinned by digest, others tag every build with its commit
(`sha-e1c83ba`). For those `ccu` compares the manifest digest instead of the
version number, and reports the update as level `digest`:

| In your Compose file            | What `ccu` does                                                          |
| ------------------------------- | ------------------------------------------------------------------------ |
| `image: vert:sha-438f91a`       | Moves the tag to the one currently matching `latest`, e.g. `sha-e1c83ba` |
| `image: vert@sha256:abc…`       | Rewrites the digest to the one `latest` now resolves to                  |
| `image: vert:1.2.3@sha256:abc…` | Bumps the tag **and** the digest together, so they stay consistent       |
| `image: vert:latest`            | Pinned to the digest it resolves to today, with `-pin-floating` (see below) |

### The reference tag

All of that hangs on one tag: `latest`, whose digest is what "newest" means for
an image with no readable version. A repository that publishes no `latest` has
nothing to be compared against, so `ccu` skips it entirely — it says
`no latest tag to compare against` and moves on. Name the moving tag it *does*
publish and it is back in the game:

```yaml
# .ccu.yaml or ~/.config/ccu/config.yaml
images:
  internal/thing:
    reference_tag: stable
```

Now `internal/thing:sha-e1c83ba` is compared against the digest of
`internal/thing:stable`, and the commit tag carrying that digest is the update.
The reference tag itself is never offered as the new tag — trading a fixed
reference for a moving one is not an update — and it applies to that image only:
everything else keeps comparing against `latest`.

### Floating tags

`latest`, `main`, `edge`, `nightly` and friends always resolve to whatever is
newest, so there is never a newer tag to offer — and nothing in the Compose file
to tell you the image behind the tag has changed. `-pin-floating` writes that
down:

```yaml
-  image: nginx:latest
+  image: nginx:latest@sha256:b34848eff6db…
```

The tag is still spelled `latest`, but the digest now decides: `docker compose
pull` gets **that exact build** and stops following the tag. In exchange `ccu` can
*see* the drift — every later run compares the pinned digest against what
`latest` resolves to and reports a `digest` update when they differ, exactly like
a digest-pinned image. You trade automatic pulls for a reviewable bump.

These rows are reported as level `pin`, and only when asked for:

```bash
ccu check -pin-floating        # report them
ccu check -pin-floating -u     # and write them
```

```yaml
# .ccu.yaml or ~/.config/ccu/config.yaml
pin_floating: true
```

`latest`, `main`, `master`, `edge`, `stable`, `nightly`, `dev` and `develop` are
the tags `ccu` treats as floating. If your registry moves a differently spelled
one — `release`, `prod`, `canary` — say so and it is pinned like the rest:

```yaml
# .ccu.yaml or ~/.config/ccu/config.yaml — for every image
floating_tags: [release, canary]

# ...or for one image only
images:
  internal/thing:
    floating_tags: [release, canary]
```

A registry usually spells its moving tag the same way across all of its
repositories, so the global list is the one to reach for; the per-image list is
there for the odd repository that differs.

Everything **adds up** rather than replacing: the built-in names, the global
list, the two config files, and the per-image list. Nothing ever takes a name
away. The reason is that the built-in names are a fact about how registries
work rather than a preference of yours — a repository that publishes `release`
almost certainly publishes `latest` beside it, and if naming one made `ccu`
forget the others, that `latest` would turn back into an ordinary tag, pinnable
and offered as an update target, which is precisely what the built-in list
prevents. For the same reason the two config files union here the way `exclude`
does, instead of the project file replacing the global one the way a per-image
cap does.

In the TUI they sit behind the bar's `floating` stop (`p`), which lists and hides
them; `pin_floating` decides which way it starts. If the run was not asked to pin,
the first press fetches the digests then and there — nothing is spent on a
registry until you ask. Caps do not apply: pinning moves no version, so an image
capped at `patch` can still be pinned.

> [!NOTE]
> Once a digest is in the file, the image is that exact build until `ccu` moves
> it — so this suits stacks you update deliberately, not ones relying on a
> nightly `pull`. A pin also costs one registry request per floating image,
> which is the other reason it is off by default. `-pin-floating=false` overrides
> `pin_floating: true` for a single run.

> [!NOTE]
> This requires querying tags individually, so the first check of such an image
> is noticeably slower. At most 250 tags of the same naming scheme are inspected.

## Troubleshooting

<details>
<summary>No new versions found, but newer versions exist</summary>

`ccu check` only looks for **patch** versions by default. With `1.0.0` current
and `1.1.0` latest there is no newer patch, so nothing is suggested. Use
`ccu check -f`, or the TUI, which always resolves every level.

</details>

<details>
<summary>Image tags with only x.y versions</summary>

Alpine has `3.14`, `3.14.1` and `3.14.0` — if you use `3.14`, `ccu` suggests
`3.14.1`. But Postgres has `13`, `13.3` and `13.4`: on `13.2`, `ccu` will not
suggest `13.4`, because `13` is not a valid semver version.

</details>

<details>
<summary>Coming from v0.6.x?</summary>

The report used to be the default and `-i` opened the TUI; that is now the other
way round. Both old spellings still work — `-i` is accepted as a no-op, and a
report-only flag without `check` (`ccu -u`) still runs the report with a one-line
hint. The old `-self-update` / `-check-update` flags work too.

</details>

## Contributing

Issues and PRs welcome — registry quirks and real-world Compose files that trip
the parser especially. Plain Go, no codegen: `go test ./...` and `go vet ./...`
is what CI runs.

## License

[MIT](LICENSE) © P. Arndt
