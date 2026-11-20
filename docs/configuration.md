# Configuration

[← README](../README.md) · [CLI](cli.md) · [Configuration](configuration.md) · [Guides](guides.md) · [TUI](tui.md) · [JSON output](json.md) · [Troubleshooting](troubleshooting.md)

Two files, neither required — `~/.config/ccu/config.yaml` for preferences across
every project, `.ccu.yaml` in the scan root or any parent for settings that
travel with the stacks. Project layers over global, flags over both.
`ccu config` prints what was read.

```yaml
# .ccu.yaml — every key is optional
exclude: [node_modules, services/legacy]
pin_floating: true
dockerfiles: false
versioning: semver
min_age: 7d
cache_ttl: 10m
floating_tags: [release, canary]

images:
  library/traefik:
    max: minor
    min_age: 3d
  nousresearch/hermes-agent:
    versioning: loose
  acme/dated:
    versioning: regex
    versioning_pattern: '^(?P<major>\d{4})-(?P<minor>\d{2})-(?P<patch>\d{2})$'
  internal/thing:
    reference_tag: stable
    floating_tags: [release]
```

## Top-level keys

| Key | Values | Default | Meaning |
| --- | ------ | ------- | ------- |
| `exclude` | list of paths | none | Directories never scanned. Unioned with `-exclude`, not replaced. |
| `pin_floating` | bool | `false` | Offer floating tags the digest they resolve to |
| `dockerfiles` | bool | `true` | Also check `FROM` lines of Dockerfiles built by a service |
| `versioning` | `semver`, `loose` | `semver` | How tags are read as versions, run-wide |
| `min_age` | duration (`7d`, `36h`) | none | Only offer tags published at least this long ago |
| `cache_ttl` | duration (`10m`, `1h`) | `10m` | How long a cached tag list stays usable. `0` never reads the cache back. See [The cache](guides.md#the-cache). |
| `floating_tags` | list of tags | see below | Extra tags treated as moving. **Added** to the built-in set. |
| `images` | map | — | Per-image settings, keyed by image name **without tag or digest** |

`exclude` matches `backup` at any depth, `services/legacy` only that path,
`/mnt/backups` that absolute location — all three with `*` wildcards.

## Per-image keys (under `images:`)

| Key | Values | Meaning |
| --- | ------ | ------- |
| `max` | `major`, `minor`, `patch` | Cap: never offer a bump above this level |
| `versioning` | `semver`, `loose`, `regex` | Tag-reading scheme for this image. Outranks the flag. |
| `versioning_pattern` | Go regex | Required by, and only valid with, `versioning: regex` |
| `reference_tag` | tag name | The moving tag digest mode compares against, instead of `latest` |
| `floating_tags` | list of tags | Extra moving tags for this repository only |
| `min_age` | duration (`7d`, `36h`) | Settling time for this image. Outranks the run-wide key and the flag. |

## Why `min_age`

A tag that went out an hour ago is the one most likely to be pulled, rebuilt or
followed by a `.1` before the day is out. `min_age` says how long a release has
to have been on the registry before ccu offers it: with `min_age: 7d`, a `1.4.2`
published yesterday is skipped and you are offered `1.4.1` instead — the newest
tag *at that level* that is old enough. Nothing is hidden permanently; the same
tag is offered as soon as it has settled.

The age comes from the `created` field of the image's config blob (falling back
to the `org.opencontainers.image.created` annotation). Registries that record
neither leave ccu with no age, and a tag whose age is unknown is offered as
usual — `min_age` skips tags known to be young, not ones it cannot date.

> [!TIP]
> A setting not arriving is almost always the key: lookup is **exact**, on the
> name as ccu reports it — `library/traefik`, never `traefik`. Ask about the one
> image with `ccu config -image library/traefik`; it names the layer and the file
> that decided each value, and offers a near-miss as a hint.
