<p align="center">
  <img src="./logo.png" alt="Compose-Check-Updates Logo" width="200">
</p>

<h1 align="center">Compose-Check-Updates</h1>

<p align="center">
  <strong>Keep your Docker Compose image tags up to date — like <code>npm-check-updates</code>, but for <code>compose.yaml</code>.</strong>
</p>

```bash
ccu        # show what's outdated
ccu -u     # write the new tags
ccu -i     # pick what to update in a full-screen TUI
```

## The interactive mode

`ccu -i` is the nicest way to use this tool.

<!-- TODO: drop a screenshot or asciinema gif of the TUI here -->

A full-screen terminal UI: updates grouped per Compose file, colour-coded by level,
streaming in as the registries answer. Arrow keys to move, `space` to select,
`A` to apply. Press `?` for everything else.

Nothing is written until you press `A`, and you decide per row which version gets
written — so a major bump never sneaks in.

<details>
<summary>All keys and the filter/target model</summary>

| Key                | Action                                             |
| ------------------ | -------------------------------------------------- |
| `↑`/`↓` or `k`/`j` | Move the selection                                 |
| `space` or `enter` | Toggle a row, or fold/unfold a file group          |
| `a` / `n`          | Select all / select none                           |
| `z`                | Fold/unfold the group under the cursor             |
| `C` / `E`          | Collapse all / expand all groups                   |
| `f`                | Cycle the display filter (which rows are shown)    |
| `t`                | Cycle the target level for **all** rows            |
| `T`/`→` or `←`     | Cycle the target level for the **highlighted** row |
| `d` or `tab`       | Toggle the detail pane                             |
| `i`                | Show the issues logged during the scan             |
| `A`                | Apply the **selected** updates                     |
| `u`                | Apply **only the highlighted row**                 |
| `y` / `n`          | Answer the restart prompt                          |
| `?` / `q`          | Help / quit                                        |

**Filter vs. target** — `show` (`f`) only decides which rows are _visible_;
`target` (`t`, `T`) decides which version actually gets _written_. The target
defaults to `major`, so out of the box you are offered the highest available
version. At target `minor`, an image on `traefik:v2.9.3` that has `3.7.8`
available re-points to the latest `2.11.x` instead; at `patch`, to `2.9.4`.
`T` only cycles the levels an image actually has — the `(+2)` after a version
means two other levels exist. A row with nothing at the current target shows as
`[-] … no patch update` and cannot be applied.

After applying, `ccu` asks once whether the affected Compose files should be
restarted with `docker compose up -d`.

The TUI always resolves **all** update levels, regardless of `-patch`, `-minor`,
`-major` or `-f`. Those flags govern the non-interactive mode only. Interactive
mode needs a real terminal — when stdout is piped, `ccu` exits with a hint to use
the non-interactive mode.

</details>

## Installation

Download the binary for your platform from the
[Releases](https://github.com/p-arndt/compose-check-updates/releases) page.

**Windows** — rename it to `ccu.exe`, optionally put its directory on your `PATH`,
then check with `ccu.exe -v`.

**Linux** — rename it to `ccu`, `chmod +x ccu`, optionally move it to
`/usr/local/bin`, then check with `ccu -v`.

You can also just run the downloaded file directly (`./ccu-linux-amd64`) without
installing anything.

## Usage

Run `ccu` in a directory — all subdirectories are scanned recursively for Compose
files, and the images in their services are checked against their registries.

```bash
ccu              # report only (patch updates by default)
ccu -u           # write the new tags
ccu -u -r        # write, then restart the affected services
ccu -f           # consider every newer version, not just patches
ccu -d ./stacks  # scan a different directory
```

> [!NOTE]
> `-u` creates a backup of every modified Compose file with a `.ccu` extension.

### Flags

> [!IMPORTANT]
> With `-i`, all flags except `-d` and `-exclude` are ignored.

| Flag       | Description                                                    | Default   |
| ---------- | -------------------------------------------------------------- | --------- |
| `-h`       | Show help message                                              | `false`   |
| `-u`       | Update the Compose files with the new image tags               | `false`   |
| `-r`       | Restart the services after updating                            | `false`   |
| `-i`       | Launch the full-screen TUI                                     | `false`   |
| `-d`       | Directory to scan                                              | `.`       |
| `-f`       | Full update mode — check up to the latest semver version       | `false`   |
| `-major`   | Only suggest major version updates                             | `false`   |
| `-minor`   | Only suggest minor version updates                             | `false`   |
| `-patch`   | Only suggest patch version updates                             | `true`    |
| `-exclude` | Exclude services from being updated (comma-separated)          | `none`    |

### Commands

These act on `ccu` itself and ignore the flags above.

```bash
ccu self-update    # download, verify and replace the running binary
ccu check-update   # only report whether something newer exists
```

A normal (non-interactive) run also checks **at most once every 24 hours** whether
a newer release exists and prints one line to stderr if so — stdout stays clean.
It never installs anything by itself.

<details>
<summary>Update-check details</summary>

The timestamp of the last check lives in `<user config dir>/ccu/update-check.json`
(`%AppData%\ccu\update-check.json` on Windows, `~/.config/ccu/update-check.json`
on Linux). Set `CCU_NO_UPDATE_CHECK=1` to disable the check entirely.

The older `-self-update` and `-check-update` flag spellings still work so existing
scripts keep running, but the subcommands above are the supported form.

</details>

## Images without semver tags

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

By default `ccu` only checks for **patch** versions. With a current tag of `1.0.0`
and a latest tag of `1.1.0`, there is no newer patch version, so nothing is
suggested. Use `ccu -f` to consider every newer version.

</details>

<details>
<summary>Image tags with only x.y versions</summary>

Some images only publish `x.y` tags. Alpine has `3.14`, `3.14.1` and `3.14.0` — if
you use `3.14`, `ccu` suggests `3.14.1`. But Postgres has `13`, `13.3` and `13.4`:
if you use `13.2`, `ccu` will not suggest `13.4`, because `13` is not a valid
semver version.

_(This might change in the future behind an additional flag.)_

</details>
