<p align="center">
  <img src="./logo.png" alt="Compose-Check-Updates Logo" width="200">
</p>

<h1 align="center">Compose-Check-Updates</h1>

<p align="center">
  <strong>Keep your Docker Compose image tags up to date — like <code>npm-check-updates</code>, but for <code>compose.yaml</code>.</strong>
</p>

```bash
ccu              # open the TUI and pick what to update
ccu check        # just print what's outdated
ccu check -u     # print and write the new tags
```

Point it at a directory, it scans every Compose file below it, asks each registry
what's newer, and — if you want — rewrites the tags for you.

---

# Install

Download the binary for your platform from the
[Releases](https://github.com/p-arndt/compose-check-updates/releases) page.

**Linux / macOS**

```bash
mv ccu-linux-amd64 ccu && chmod +x ccu
sudo mv ccu /usr/local/bin/     # optional
ccu version
```

**Windows** — rename it to `ccu.exe`, optionally put its directory on your `PATH`,
then check with `ccu.exe version`.

You can also just run the downloaded file directly (`./ccu-linux-amd64`) without
installing anything.

Later on, `ccu self-update` replaces the binary in place — see
[Updating ccu](#updating-ccu).

# Usage

`cd` into the directory holding your stacks and run `ccu`. All subdirectories are
scanned recursively for Compose files, and the images in their services are
checked against their registries.

There are two ways to use it:

| | |
| ---------------- | ---------------------------------------------------------- |
| **`ccu`**        | The TUI. Browse what's outdated, pick rows, apply. Default. |
| **`ccu check`**  | One-shot report for scripts, cron and CI. No UI.            |

Nothing is ever written unless you ask for it — `A` in the TUI, `-u` for `check`.

> [!NOTE]
> Writing creates a backup of every modified Compose file next to it, with a
> `.ccu` extension.

## The TUI (default)

```bash
ccu                # scan the current directory
ccu -d ./stacks    # scan somewhere else
```

<!-- TODO: drop a screenshot or asciinema gif of the TUI here -->

A full-screen terminal UI: updates grouped per Compose file, colour-coded by
level, streaming in as the registries answer. Arrow keys to move, `space` to
select, `A` to apply. Press `?` for everything else.

Nothing has to be memorised to get started. A bar across the top holds the
settings that apply to the whole run, and `tab` walks the keyboard through
everything there is to reach:

```
 show ‹ all ›   target ‹ major ›   [ issues 1 ]   [ apply 2 ]
```

One rule holds everywhere: **the arrows move, `space` and `enter` act on
whatever has the focus.** In the list that is the row, so they select it. On the
bar it is the stop, so they step the setting or press the button. In the detail
column it is the field. Nothing has to be remembered per pane.

Each stop also names the key that does the same thing, so the bar teaches the
shortcut rather than replacing it.

`tab` answers one question: what is the cursor on? On an image it opens the
detail column beside the list, and `tab` there comes straight back. On a file or
directory header — which describes no image — it goes up to the bar instead.

`→`/`l` and `←`/`h` are navigation: on an image, right opens the detail column
and left closes it again; on a header they walk the tree. On the bar they move
between stops. Each pane leaves in the direction it sits from the list — the
column is to its right, so `←` leaves it; the bar is above it, so `↓` does — and
`↑` at the top of the list goes back up onto it. `esc` works from either, and
only ever means "back": quitting is `q`, never `esc`.

`m` is the bar's own key: it reaches the bar from anywhere, and each further
press moves one stop along.

Nothing is written until you press `A`, and you decide per row which version gets
written — so a major bump never sneaks in. Afterwards `ccu` asks once whether the
affected Compose files should be restarted with `docker compose up -d`.

<details>
<summary><strong>All keys</strong></summary>

| Key                | Action                                            |
| ------------------ | ------------------------------------------------- |
| `↑`/`↓` or `k`/`j` | Move the cursor. At the top of the list, `↑` carries on into the bar; on the bar, `↓` comes back |
| `pgup`/`pgdn`      | Page up / down (`home`/`end` for first / last)    |
| `←`/`h`, `→`/`l`   | On a header: collapse / expand. On an image: close / open the details. On the bar: previous / next stop |
| `space` / `enter`  | Act on what has the focus: select the row, step a setting, press a button |
| `-`                | Step the focused setting backwards                |
| `z`                | Fold/unfold the node under the cursor             |
| `C` / `E`          | Collapse all / expand all                         |
| `a` / `n`          | Select / deselect everything under the cursor     |
| `ctrl+a` / `ctrl+n`| Select / deselect the whole list                  |
| `f`                | Cycle the display filter (which rows are shown)   |
| `t`                | Cycle the target level for **all** rows           |
| `tab`              | On an image: the details column (`tab` again returns). Otherwise: the top bar |
| `shift+tab`        | Step back along the bar                           |
| `m`                | The top bar, from anywhere; again for the next stop |
| `i`                | Show the issues logged during the scan            |
| `A`                | Apply the **selected** updates                    |
| `u`                | Apply **only the highlighted row**                |
| `y` / `n`          | Answer the restart prompt                         |
| `esc`              | Back out of whatever has the keyboard (never quits) |
| `?` / `q`          | Help / quit                                       |

</details>

<details>
<summary><strong>Filter vs. target</strong> — which version actually gets written</summary>

`show` only decides which rows are _visible_; `target` decides which version
actually gets _written_. Both sit on the top bar and both have a key (`f` and
`t`); the row's own target lives in the detail column. The target defaults to `major`, so out of
the box you are offered the highest available version. At target `minor`, an
image on `traefik:v2.9.3` that has `3.7.8` available re-points to the latest
`2.11.x` instead; at `patch`, to `2.9.4`.

The sidebar only offers the levels an image actually has — the `(+2)` after a version
means two other levels exist. A row with nothing at the current target shows as
`[-] … no patch update` and cannot be applied.

The TUI always resolves **all** update levels, regardless of `-patch`, `-minor`,
`-major` or `-f`. Those flags govern `ccu check` only.

</details>

> [!TIP]
> The TUI needs a real terminal. When stdout is piped or redirected, `ccu` runs
> the `check` report instead and says so on stderr — so an old cron entry or CI
> job keeps working either way.

## `ccu check` — the non-interactive report

```bash
ccu check              # report only (patch updates by default)
ccu check -u           # write the new tags
ccu check -u -r        # write, then restart the affected services
ccu check -f           # consider every newer version, not just patches
ccu check -d ./stacks  # scan a different directory
```

Every flag below applies to `ccu check`; only `-d`, `-exclude` and `-config`
also apply to the TUI, which picks levels in the UI instead.

| Flag       | Description                                              | Default |
| ---------- | -------------------------------------------------------- | ------- |
| `-d`       | Directory to scan                                        | `.`     |
| `-exclude` | Directories to exclude, comma-separated                  | none    |
| `-config`  | Read this config file instead of searching for one       | none    |
| `-u`       | Update the Compose files with the new image tags         | `false` |
| `-r`       | Restart the services after updating                      | `false` |
| `-f`       | Full mode — consider every newer version, not just patches | `false` |
| `-major`   | Only suggest major version updates                       | `false` |
| `-minor`   | Only suggest minor version updates                       | `false` |
| `-patch`   | Only suggest patch version updates                       | `true`  |

## All commands

```bash
ccu                # the TUI
ccu check          # the non-interactive report
ccu self-update    # download, verify and replace the running binary
ccu check-update   # only report whether a newer ccu exists
ccu config         # show the resolved configuration and where it came from
ccu help           # show the help message
ccu version        # show version information
```

`self-update`, `check-update`, `help` and `version` act on `ccu` itself and
ignore the flags above.

> [!NOTE]
> **Coming from v0.6.x?** The report used to be the default and `-i` opened the
> TUI. That is now the other way round. Both old spellings still work: `-i` is
> accepted as a no-op, and a report-only flag without `check` (`ccu -u`) still
> runs the report, printing a one-line hint about the new spelling.

## Configuration file

Directories you never want scanned, written down once:

```yaml
# .ccu.yaml
exclude:
  - node_modules
  - backup
  - services/legacy
```

| File                                      | For                                       |
| ----------------------------------------- | ----------------------------------------- |
| `~/.config/ccu/config.yaml`               | preferences across every project          |
| `.ccu.yaml` in the scan root or any parent | settings that travel with the stacks, in git |

Neither is required. Project layers over global, `-exclude` over both, and the
lists are **unioned** rather than replaced. `.yml` works too. `-config <path>`
reads one specific file instead of searching. `ccu config` shows what was read
and what the merged result is.

How an entry is written decides what it covers: `backup` matches that directory
name **at any depth**, `services/legacy` only that path below the scan root,
`/mnt/backups` that absolute location — all three with `*` wildcards. Excluded
directories are never descended into.

A malformed config file, or an unknown key in one, aborts the run instead of
being skipped.

# Updating ccu

```bash
ccu self-update    # download, verify and replace the running binary
ccu check-update   # only report whether something newer exists
```

A `ccu check` run also checks **at most once every 24 hours** whether a newer
release exists and prints one line to stderr if so — stdout stays clean. It never
installs anything by itself.

<details>
<summary>Update-check details</summary>

The timestamp of the last check lives in `<user config dir>/ccu/update-check.json`
(`%AppData%\ccu\update-check.json` on Windows, `~/.config/ccu/update-check.json`
on Linux). Set `CCU_NO_UPDATE_CHECK=1` to disable the check entirely.

The older `-self-update` and `-check-update` flag spellings still work so existing
scripts keep running, but the subcommands above are the supported form.

</details>

# Images without semver tags

Not every image publishes semantic versions. Some are pinned by digest, others tag
every build with its commit (e.g. `ghcr.io/vert-sh/vert` with `sha-e1c83ba` tags).
For those, `ccu` compares the image manifest digest instead of the version number:

| In your Compose file            | What `ccu` does                                                          |
| ------------------------------- | ------------------------------------------------------------------------ |
| `image: vert:sha-438f91a`       | Moves the tag to the one currently matching `latest`, e.g. `sha-e1c83ba` |
| `image: vert@sha256:abc…`       | Rewrites the digest to the one `latest` now resolves to                  |
| `image: vert:1.2.3@sha256:abc…` | Bumps the tag **and** the digest together, so they stay consistent       |
| `image: vert:latest`            | Skipped — a floating tag already resolves to the newest image            |

These are reported with the update level `digest`. A digest change has no
major/minor/patch level, so it is always reported and is unaffected by `-major`,
`-minor` and `-patch`.

> [!NOTE]
> Finding which tag carries the newest digest requires querying tags individually,
> so the first check of such an image is noticeably slower. At most 250 tags of the
> same naming scheme are inspected; `ccu` warns when an image has more.

## Troubleshooting

<details>
<summary>No new versions found, but newer versions exist</summary>

By default `ccu check` only checks for **patch** versions. With a current tag of
`1.0.0` and a latest tag of `1.1.0`, there is no newer patch version, so nothing
is suggested. Use `ccu check -f` to consider every newer version — or just use the
TUI, which always resolves every level.

</details>

<details>
<summary>Image tags with only x.y versions</summary>

Some images only publish `x.y` tags. Alpine has `3.14`, `3.14.1` and `3.14.0` — if
you use `3.14`, `ccu` suggests `3.14.1`. But Postgres has `13`, `13.3` and `13.4`:
if you use `13.2`, `ccu` will not suggest `13.4`, because `13` is not a valid
semver version.

_(This might change in the future behind an additional flag.)_

</details>

<details>
<summary>The TUI does not open</summary>

`ccu` falls back to the `check` report when stdout is not a terminal — inside a
pipe (`ccu | less`), a redirect (`ccu > out.txt`), or a CI job. The stderr line
tells you when that happened. Run `ccu` with its output attached to the terminal
to get the UI.

</details>
