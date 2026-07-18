<p align="center">
  <img src="./logo.png" alt="Beschrapi Logo" width="200">
</p>

<h1 align="center">Compose-Check-Updates</h1>

<p align="center">
  <strong>
Easily update Docker Compose image tags to their latest versions.
  </strong>
</p>

`compose-check-updates` helps you manage and update images in Docker Compose files, similar to how `npm-check-updates` works for a `package.json`. This tool is heavily inspired by `npm-check-updates` and works in a similar way.

## Table of Contents

- [Table of Contents](#table-of-contents)
- [Installation](#installation)
  - [Quick](#quick)
  - [System-wide](#system-wide)
    - [Windows](#windows)
    - [Linux](#linux)
- [Usage](#usage)
- [Flags](#flags)
- [Interactive mode](#interactive-mode)
- [How does it work?](#how-does-it-work)
  - [Images without semver tags](#images-without-semver-tags)
- [Troubleshooting](#troubleshooting)
  - [Image tags with only x.y versions](#image-tags-with-only-xy-versions)
  - [No new versions found but there are newer versions available](#no-new-versions-found-but-there-are-newer-versions-available)

## Installation

### Quick

1. Download the latest Windows release from the [Releases](https://github.com/Padi2312/compose-check-updates/releases) page.
2. Run the following command to check current directory for docker compose image updates:

```bash
ccu-<YOUR_ARCHITECTURE>
```

Example for Windows:

```ps1
ccu-windows-amd64.exe
```

Example for Linux:

```bash
chmod +x ccu-linux-amd64
./ccu-linux-amd64
```

### System-wide

#### Windows

1. Download the latest Windows release from the [Releases](https://github.com/Padi2312/compose-check-updates/releases) page.
2. Rename the downloaded file to `ccu.exe` for easier usage.
   1. (Optional) Add the file's directory to your PATH environment variable. So you can run `ccu` from any directory.
3. Run `ccu.exe -v` from the command prompt to check if the installation was successful.

#### Linux

1. Download the latest Linux release from the [Releases](https://github.com/Padi2312/compose-check-updates/releases) page.
2. Rename the downloaded file to `ccu` for easier usage.
3. (Optional) Move the file to `/usr/local/bin` to make it available system-wide or just add it to your PATH.
4. Make the file executable by running `chmod +x ccu`.
5. Include the path to `ccu` in your PATH environment variable.
6. Run `ccu -v` from the terminal to check if the installation was successful.

## Usage

To check for updates in Docker Compose files in the current directory, run:

Check for updates only (default: only checking patch versions):

```bash
ccu
```

Check for updates and update the Docker Compose files:

> [!NOTE]
> When choosing this option, `ccu` will create backups of the original Docker Compose files with the `.ccu` extension.

```bash
ccu -u
```

Check for updates, update the Docker Compose files, and restart the services:

```bash
ccu -u -r
```

You can also control the update behavior by using the flags described below.

## Flags

> [!IMPORTANT]
> When using `-i` for interactive mode other arguments (except `-d` for directory and `-exclude`) will be ignored. See [Interactive mode](#interactive-mode).

| Flag       | Description                                                    | Default                 |
| ---------- | -------------------------------------------------------------- | ----------------------- |
| `-h`       | Show help message                                              | `false`                 |
| `-u`       | Update the Docker Compose files with the new image tags        | `false`                 |
| `-r`       | Restart the services after updating the Docker Compose files   | `false`                 |
| `-i`       | Launch the full-screen TUI to pick which images to update      | `false`                 |
| `-d`       | Specify the directory to scan for Docker Compose files         | `.` (current directory) |
| `-f`       | Full update mode, checks updates to latest semver version      | `false`                 |
| `-major`   | Only suggest major version updates                             | `false`                 |
| `-minor`   | Only suggest minor version updates                             | `false`                 |
| `-patch`   | Only suggest patch version updates                             | `true`                  |
| `-exclude` | Exclude specific services from being updated (comma-separated) | `none`                  |

## Interactive mode

```bash
ccu -i
```

This opens a full-screen terminal UI. Updates are grouped by the Compose file they
belong to and colour-coded by update level (major, minor, patch, digest), while a
progress bar shows how far the scan got — results appear as the registries answer,
so you can start selecting before the scan has finished.

| Key                  | Action                                              |
| -------------------- | --------------------------------------------------- |
| `↑`/`↓` or `k`/`j`   | Move the selection                                  |
| `space`              | Toggle the selected update                          |
| `a`                  | Select all updates                                  |
| `n`                  | Select none                                         |
| `f`                  | Cycle the display filter (which rows are shown)     |
| `t`                  | Cycle the target level for **all** rows             |
| `T`/`→` or `←`       | Cycle the target level for the **highlighted** row  |
| `d` or `tab`         | Toggle the detail pane                              |
| `enter`              | Apply the selected updates                          |
| `y` / `n`            | Answer the restart prompt                           |
| `?`                  | Show help                                           |
| `q`                  | Quit                                                |

### Filter vs. target

These are two different things, and the legend at the bottom names both:

- **`show`** (`f`) only decides which rows are *visible*. It never changes a version.
- **`target`** (`t`, `T`) decides which version `enter` actually *writes*.

The target defaults to `major`, so out of the box `ccu -i` offers the highest
available version, as it always has. Set it to `minor` or `patch` when you would
rather not jump a major release: an image on `traefik:v2.9.3` that is offered
`3.7.8` will re-point to the latest `2.11.x` at target `minor`, or `2.9.4` at
target `patch`.

Rows are re-pointed individually with `T`, which only cycles the levels that
image actually has — the `(+2)` after a version tells you two other levels are
available. The badge always shows the level of the version currently selected.
An image with nothing at the current target is shown as `[-] … no patch update`
and cannot be selected, so `enter` can never write a version you did not pick.

After applying, `ccu` asks once whether the affected Compose files should be
restarted with `docker compose up -d`.

> [!NOTE]
> The TUI always resolves and shows **all** update levels, regardless of `-patch`,
> `-minor`, `-major` or `-f` — use the in-UI filter (`f`) to narrow the list and
> the target (`t`) to choose which version gets written. Those flags still govern
> the non-interactive mode.

Interactive mode needs a real terminal. When stdout is piped or redirected, `ccu`
exits with an error message pointing you at the non-interactive mode.

## How does it work?

`compose-check-updates` scans the given directory for Docker Compose files. It then reads the images in the services and checks if there are newer versions available.

If newer versions are found, `compose-check-updates` will suggest the updated image tags. You can then choose to update the Docker Compose files with the new image tags.

> [!NOTE]
> All subdirectories are scanned recursively for Docker Compose files.

### Images without semver tags

Not every image publishes semantic versions. Some are pinned by digest, others tag
every build with the commit it was built from (for example `ghcr.io/vert-sh/vert`,
which publishes `sha-e1c83ba` style tags). For those, `ccu` compares the image
manifest digest instead of the version number:

| In your Compose file                     | What `ccu` does                                                            |
| ---------------------------------------- | -------------------------------------------------------------------------- |
| `image: vert:sha-438f91a`                | Moves the tag to the one currently matching `latest`, e.g. `sha-e1c83ba`   |
| `image: vert@sha256:abc…`                | Rewrites the digest to the one `latest` now resolves to                     |
| `image: vert:1.2.3@sha256:abc…`          | Bumps the tag **and** the digest together, so they stay consistent          |
| `image: vert:latest`                     | Skipped — a floating tag already resolves to the newest image               |

These updates are reported with the update level `digest`. Since a digest change
has no major/minor/patch level, it is always reported and is not affected by the
`-major`, `-minor` and `-patch` flags.

> [!NOTE]
> Finding which tag carries the newest digest requires querying tags individually,
> so the first check of such an image is noticeably slower than a semver lookup.
> At most 250 tags of the same naming scheme are inspected; `ccu` warns when an
> image has more tags than that.

## Troubleshooting

### Image tags with only x.y versions

Some images only have `x.y` versions and no `x.y.z` versions.
This can lead to the following scenario:

Alpine has the following tags:

- `3.14`
- `3.14.1`
- `3.14.0`

If you are using `3.14` in your Docker Compose file, `ccu` will suggest an update to `3.14.1`.

But for Postgres with the following tags:

- `13`
- `13.3`
- `13.4`

If you are using `13.2` in your Docker Compose file, `ccu` will not suggest an update to `13.4` because it's no valid semver version.

_(This might be changed in the future with an additional flag)_

### No new versions found but there are newer versions available

On default `ccu` checks for patch versions only.

---

Example:

- Current image tag: `1.0.0`
- Latest image tag: `1.1.0`

Result: No newer patch versions available

---

`ccu` on default will not suggest an update in this case.

To check for all newer versions, use the `-f` flag:

```bash
ccu -f
```

This will suggest the latest version `1.1.0` as an update.
