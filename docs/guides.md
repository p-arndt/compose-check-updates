# Guides

[← README](../README.md) · [CLI](cli.md) · [Configuration](configuration.md) · [Guides](guides.md) · [TUI](tui.md) · [JSON output](json.md) · [Troubleshooting](troubleshooting.md)

Recipes for the cases a first run tends to run into.

## Check a single image

`-image` narrows a run to the images it names, and nothing else is looked up —
no registry sees a request for the rest:

```bash
ccu check -f -image traefik              # one image, every level
ccu check -u -image 'ghcr.io/immich-app/*'   # a whole namespace, applied
```

A pattern **without** a `/` is matched against the last part of the name, so
`traefik` finds `library/traefik` and `immich-server` finds
`ghcr.io/immich-app/immich-server`. One **with** a `/` is matched against the
full name as ccu reports it. Both take `*`, and unlike a shell glob it spans
`/`: `ghcr.io/*` reaches every repository under that host. Matching is
case-sensitive.

Repeat the flag or separate the patterns with commas — `-image traefik,nginx`
is `-image traefik -image nginx`. If nothing matches, ccu says so on stderr and
exits `0`. The flag applies to the TUI as well (`ccu -image traefik`), and there
is no config key for it: it selects what *this* run looks at.

## Private registries and rate limits

Any OCI registry works — Docker Hub, GHCR, Quay, Harbor, ECR, a self-hosted
`registry:2`. Private repos need no setup: `ccu` reads the credentials
`docker login` already stored. Logging in also lifts Docker Hub's anonymous rate
limit, which starts to matter past a few dozen images.

The other half of that problem is asking twice for the same answer: **the
cache** below keeps registry answers for ten minutes, so reopening the TUI or
running a `check` straight after one costs no requests at all.

## The cache

`ccu` keeps registry answers on disk, in
`~/.cache/ccu` (`~/Library/Caches/ccu` on macOS, `%LocalAppData%\ccu` on
Windows). It exists so that reopening the TUI, or running `ccu check` right
after a TUI session, does not pay Docker Hub's rate limit twice — and it is
built so that it can never hide a release from you.

Two kinds of answer, two policies:

| Cached | For how long | Why |
| ------ | ------------ | --- |
| Tag lists, and what a tag currently points at | **`cache_ttl`, 10 minutes by default** | These change whenever something is published. Anything older is fetched again. |
| The build date and source label of a **digest** | 30 days | A digest names its content: what it was built from cannot change. Which digest a tag points at is still checked every time. |

So a release published a minute ago is found by the next run: the tag list is
minutes old at most, and the step from tag to digest is never cached for longer.

- **`-refresh`** ignores every cached entry for this run and asks the registries
  about everything — `ccu -refresh`, `ccu check -refresh`. This is the "I want to
  update *right now*" switch.
- **`CCU_NO_CACHE=1`** reads and writes nothing, for a CI runner that throws the
  directory away anyway.
- **`cache_ttl`** sets the first row's lifetime, globally or per project;
  `cache_ttl: 0` writes the cache but never reads it back.
- **`CCU_CACHE_DIR`** puts the directory somewhere else. `ccu config` prints
  where it is and how long entries are trusted.

If a registry answers with a rate limit or cannot be reached at all, `ccu` falls
back to an expired entry rather than failing, and says so on stderr with the age
of the data. That only happens on failure — an answering registry always wins
over the cache. `ccu check` reports what the cache saved on stderr:

```
served 12 of 40 lookups from cache (~/.cache/ccu, 10m0s ttl; -refresh to bypass)
```

Entries older than 30 days are dropped at the end of a run, so the directory
does not grow forever.

## Tags kept in a variable (`${IMMICH_VERSION}`)

Plenty of stacks keep the release in the `.env` next to the Compose file rather
than in the file itself:

```yaml
# docker-compose.yml
services:
  server:
    image: ghcr.io/immich-app/immich-server:${IMMICH_VERSION}
```

```bash
# .env
IMMICH_VERSION=v1.119.0
```

`ccu` interpolates the reference the way `docker compose` does — your shell
environment first, then the `.env`, then a default written into the reference
itself (`${IMMICH_VERSION:-release}`) — so it checks the tag your stack actually
runs on. `${VAR}`, `$VAR`, `${VAR:-default}`, `${VAR-default}` and the `$$`
escape are all understood.

Updates are written back **where the version lives**. For the stack above that
is the `.env` line, with the Compose file left untouched:

```diff
-IMMICH_VERSION=v1.119.0
+IMMICH_VERSION=v1.120.1
```

Quoting, spacing and trailing comments on that line survive, and the `.env` gets
its own `.ccu` backup. Only the variable's share of the tag moves: on
`postgres:${PG_VERSION}-alpine` the `-alpine` stays in the Compose file and just
`PG_VERSION` is raised.

Two cases are reported but not written, because there is no file to write them
to: a variable set in the environment `ccu` itself runs with, and a tag assembled
from more than one variable. A variable nothing defines is reported as
`unresolved-variable`, naming the variable and the `.env` it belongs in.

## "no tag matches the newest digest" — tags that aren't semver

Plenty of images publish something that is a version but not a *semantic* one:
`nousresearch/hermes-agent` tags by date and sometimes rebuilds the same day —
`v2026.7.7` and `v2026.7.7.2` side by side. A fourth segment is not semver, so by
default those tags are not read as versions at all. Loosen the rule for that
image:

```yaml
images:
  nousresearch/hermes-agent:
    versioning: loose
```

| Scheme | Reads |
| ------ | ----- |
| `semver` | up to three segments, no leading zeros — the default |
| `loose` | up to six numeric segments, leading zeros, any suffix |
| `regex` | whatever your pattern names — per image only |

The first three segments stay major, minor and patch; anything past the third
orders the version but is reported as a **patch**. The suffix rule holds either
way: `3.19-alpine` only ever moves to another `-alpine` tag.

Three places can set the scheme, most specific first: a per-image entry, then
`--versioning=loose` for one run, then a run-wide `versioning:` in either config
file. A per-image entry outranks both defaults **including the flag** — a flag
meant as a quick try should not silently undo a preference written down on
purpose.

## Tags no fixed rule can read — `versioning: regex`

A dashed date — `2024-01-01` — reads under both schemes above as release `2024`
with `-01-01` mistaken for a prerelease, which orders `2024-12-31` before
`2024-02-01`: the alphabet, not the calendar. For those, say what a tag looks
like:

```yaml
images:
  acme/dated:
    versioning: regex
    versioning_pattern: '^(?P<major>\d{4})-(?P<minor>\d{2})-(?P<patch>\d{2})$'
```

A Go regular expression with **named groups**. Four carry numbers — `major`,
`minor`, `patch`, `build` — plus `suffix` for whatever trails them, separator
included. A group you leave out is `0`, so naming only `major` is enough; a group
named anything else is ignored, which lets you match parts you don't need. The
four order exactly as under `loose`.

The pattern must describe the **whole** tag — it is anchored, so a date buried in
`build-2024-01-01-x` is not one — and a tag it does not match is simply not a
version. There is no run-wide `versioning: regex` and no `--versioning=regex`: a
pattern fitting one repository's tags is meaningless for the next one's.

Patterns are checked when the config is read, so a typo fails the run and names
the image rather than quietly reading no tags:

```console
$ ccu config
ERROR  Error reading config  error=/repo/.ccu.yaml: image "acme/dated": versioning_pattern: "^(?P<major>\\d+$" is not a valid regular expression
```

## Digest-pinned and commit-tagged images

Some images are pinned by digest, others tag every build with its commit
(`sha-e1c83ba`). For those `ccu` compares the manifest digest instead of the
version number and reports level `digest`:

| In your Compose file | What `ccu` does |
| -------------------- | --------------- |
| `image: vert:sha-438f91a` | Moves the tag to the one currently matching `latest`, e.g. `sha-e1c83ba` |
| `image: vert@sha256:abc…` | Rewrites the digest to the one `latest` now resolves to |
| `image: vert:1.2.3@sha256:abc…` | Bumps tag **and** digest together, so they stay consistent |
| `image: vert:latest` | Pinned to today's digest, with `-pin-floating` |

All of it hangs on one tag: `latest`, whose digest is what "newest" means for an
image with no readable version. A repository publishing no `latest` is skipped
with `no latest tag to compare against`. Name the moving tag it *does* publish
and it is back in the game:

```yaml
images:
  internal/thing:
    reference_tag: stable
```

The reference tag itself is never offered as the new tag — trading a fixed
reference for a moving one is not an update.

> [!NOTE]
> This queries tags individually, so the first check of such an image is
> noticeably slower. At most 250 tags of the same naming scheme are inspected.

## Pinning floating tags (`latest`, `main`, …)

Floating tags always resolve to whatever is newest, so there is never a newer tag
to offer — and nothing in the Compose file to tell you the image behind the tag
changed. `-pin-floating` writes that down:

```yaml
-  image: nginx:latest
+  image: nginx:latest@sha256:b34848eff6db…
```

The tag still reads `latest`, but the digest now decides: `docker compose pull`
gets **that exact build** and stops following the tag. In exchange `ccu` can
*see* the drift — every later run compares the pinned digest against what
`latest` resolves to and reports a `digest` update. You trade automatic pulls for
a reviewable bump.

```bash
ccu check -pin-floating        # report them (level: pin)
ccu check -pin-floating -u     # and write them
```

```yaml
pin_floating: true
```

Built-in floating tags: `latest`, `main`, `master`, `edge`, `stable`, `nightly`,
`dev`, `develop`. If your registry moves a differently spelled one, add it —
globally is usually right, since a registry spells its moving tag the same way
across repositories:

```yaml
floating_tags: [release, canary]      # every image
images:
  internal/thing:
    floating_tags: [release, canary]  # this one only
```

Everything **adds up** rather than replacing, and nothing ever takes a name away:
the built-in names are a fact about how registries work, not a preference. A repo
publishing `release` almost certainly publishes `latest` beside it, and if naming
one made `ccu` forget the others, that `latest` would turn back into an ordinary,
pinnable tag.

In the TUI they sit behind the bar's `floating` stop (`p`); `pin_floating`
decides which way it starts. If the run wasn't asked to pin, the first press
fetches the digests then and there. Caps do not apply — pinning moves no version.

> [!NOTE]
> Once a digest is in the file, the image is that exact build until `ccu` moves
> it — this suits stacks you update deliberately, not ones relying on a nightly
> `pull`. It also costs one registry request per floating image, the other reason
> it's off by default. `-pin-floating=false` overrides `pin_floating: true` for a
> single run.

## Images you build yourself (`build:` + Dockerfile)

A service with a `build:` has no image tag to check — the tag deciding what it
runs sits on the `FROM` line of its Dockerfile. Those are scanned too:

```dockerfile
-FROM quay.io/keycloak/keycloak:26.0.7 AS builder
+FROM quay.io/keycloak/keycloak:26.7.2 AS builder
```

The Dockerfile appears in the list under its own path, next to the compose file
that builds it, and behaves like any other row: levels, caps, targets, a `.ccu`
backup. Two specifics:

- **Every stage moves together.** A multi-stage build names its base as builder
  and as runtime; both are rewritten as one update — a runtime left a release
  behind its builder is a broken image, not a partial one.
- **A restart rebuilds.** `docker compose up -d --build`, since a new base image
  reaches the container only through a build.

Skipped, because no registry can answer for them: `FROM scratch`, a stage
referring to an earlier one (`FROM builder`), a reference assembled from build
args (`FROM ${BASE}`), `dockerfile_inline:`, and non-local contexts.
`-dockerfiles=false` or `dockerfiles: false` turns it off.

## The update notice

A `ccu check` run also checks **at most once every 24 hours** whether a newer
release exists and prints one line to stderr. It never installs anything by
itself; `CCU_NO_UPDATE_CHECK=1` turns the check off.
