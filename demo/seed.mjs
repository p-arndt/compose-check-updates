// Seeds the throwaway world the demo records in: a directory of Compose stacks
// under $TMPDIR, plus a HOME to keep ccu's config out of the real one.
//
// Nothing here is anyone's actual infrastructure — the stacks, service names,
// ports and pinned tags are invented to pair with demo/fake-registry.mjs, which
// serves the versions they are checked against.
//
// The tags are pinned to give three images a major update, three a minor and
// three only a patch: the demo is about choosing a level, so all three badges
// have to be on screen for that choice to mean anything.
import fs from 'node:fs'
import path from 'node:path'

export const STACKS = {
  'proxy/compose.yaml': `services:
  traefik:
    image: traefik:v2.9.3
    ports: ["80:80", "443:443"]
    restart: unless-stopped

  whoami:
    image: caddy:2.9.0
    restart: unless-stopped
`,
  'monitoring/compose.yaml': `services:
  grafana:
    image: grafana/grafana:11.1.0
    ports: ["3000:3000"]

  prometheus:
    image: prom/prometheus:v2.48.1

  loki:
    image: grafana/loki:3.2.1
`,
  'media/compose.yaml': `services:
  jellyfin:
    image: jellyfin/jellyfin:10.9.11
    ports: ["8096:8096"]

  qbittorrent:
    image: linuxserver/qbittorrent:4.6.3
`,
  'data/docker-compose.yml': `services:
  db:
    image: postgres:15.4
    volumes: ["pgdata:/var/lib/postgresql/data"]

  cache:
    image: redis:7.4.0

volumes:
  pgdata:
`,
}

export function seed(root) {
  fs.rmSync(root, { recursive: true, force: true })
  for (const [rel, body] of Object.entries(STACKS)) {
    const file = path.join(root, rel)
    fs.mkdirSync(path.dirname(file), { recursive: true })
    fs.writeFileSync(file, body)
  }
  return root
}
