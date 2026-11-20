# JSON output

[← README](../README.md) · [CLI](cli.md) · [Configuration](configuration.md) · [Guides](guides.md) · [TUI](tui.md) · [JSON output](json.md) · [Troubleshooting](troubleshooting.md)

On a terminal the report is the aligned, colour-coded listing; in a pipe it is
**JSON Lines** — one object per line, written as the scan resolves, so `jq` reads
it streaming. `--format=pretty` / `--format=json` force either one. Only the
report goes to stdout; warnings and notices go to **stderr**, so a pipe stays
parseable.

```json
{"kind":"update","image":"library/traefik","reference":"traefik:v2.9.3","services":["proxy"],"file":"proxy/compose.yaml","current":"v2.9.3","latest":"v3.2.0","level":"major","published":"2024-11-12T14:03:27Z","targets":{"minor":"v2.11.4","major":"v3.2.0"},"release_url":"https://github.com/traefik/traefik/releases/tag/v3.2.0","source_url":"https://github.com/traefik/traefik"}
{"kind":"unreadable","image":"ghcr.io/vert-sh/vert","reference":"ghcr.io/vert-sh/vert:sha-e1c83ba","file":"vert/compose.yaml","current":"sha-e1c83ba","level":"unreadable","reason":"no-tag-for-digest","message":"none of this image's tags matches its newest digest…"}
{"kind":"error","file":"data/docker-compose.yml","error":"fetching tags: 429"}
```

## Every key

| Key | On | Meaning |
| --- | -- | ------- |
| `kind` | every line | `update`, `unreadable` or `error` — dispatch on this |
| `image` / `reference` | update | the image name, and the reference as the Compose file writes it |
| `services` | update | the Compose services that declare it; a list, because identical references are reported once |
| `file` | both | the Compose file, or the Dockerfile when the update sits on a `FROM` line |
| `compose_file` | Dockerfile updates | the Compose file that builds it, i.e. the one to hand `docker compose -f` |
| `current` / `latest` | update | the tag now, and the one this run picked |
| `published` | update | when the image behind `latest` was built, RFC 3339 — absent when the registry does not say |
| `level` | update | `major`, `minor`, `patch`, `digest` or `pin` |
| `current_digest` / `latest_digest` | digest-pinned images | only present when the digest actually moved |
| `targets` | update | the tag available at each level, so you can pick a different one |
| `release_url` | updates whose image names a GitHub or GitLab source | where the notes for `latest` would be — constructed from the tag, not checked, so a project that tags its releases differently gives you a 404 |
| `source_url` | updates whose image records a source | the repository the image says it is built from (`org.opencontainers.image.source`) |
| `cap` | capped images | the ceiling recorded in your config |
| `applied` / `restarted` | with `-u` / `-r` | whether the write or restart succeeded — `false` means it was asked for and failed |
| `reason` / `message` | unreadable | why nothing could be resolved: a stable name to dispatch on, and the same thing in a sentence |
| `error` | error | the failure, as text |
