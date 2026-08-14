// A throwaway stand-in for Docker Hub, used only while recording the demo.
//
// Every repository and every tag below is invented. The recording must never
// depend on the real hub: its tag lists move, its rate limits bite, and a demo
// that renders different versions on every run cannot be re-recorded by anyone
// else. `CCU_REGISTRY_HOST` points ccu here instead (see internal/registry.go).
//
// Implements just enough of the OCI distribution API for ccu:
//   GET  /v2/                                — the version probe
//   GET  /v2/<repo>/tags/list                — the tag list
//   HEAD /v2/<repo>/manifests/<ref>          — the digest behind a reference
import http from 'node:http'
import crypto from 'node:crypto'

// repo -> tags, newest last. Docker Hub serves official images under library/.
export const REPOS = {
  'library/traefik': ['v2.9.0', 'v2.9.3', 'v2.10.7', 'v2.11.0', 'v2.11.4', 'v3.1.2', 'v3.2.0'],
  'library/postgres': ['15.4', '15.6', '15.8', '16.2', '16.4', '17.0'],
  'library/redis': ['7.2.3', '7.2.5', '7.4.0', '7.4.1'],
  'library/caddy': ['2.7.6', '2.8.4', '2.9.0', '2.9.1'],
  'grafana/grafana': ['10.2.3', '10.4.2', '11.1.0', '11.3.0'],
  'grafana/loki': ['2.9.4', '2.9.8', '3.2.1', '3.2.2'],
  'prom/prometheus': ['v2.48.1', 'v2.51.2', 'v2.54.1'],
  'jellyfin/jellyfin': ['10.8.13', '10.9.11', '10.10.0'],
  'linuxserver/qbittorrent': ['4.6.3', '4.6.7', '5.0.1'],
}

// Digests are derived from the reference, so they are stable across runs
// without being anything that ever existed in a real registry.
const digestFor = (repo, ref) =>
  'sha256:' + crypto.createHash('sha256').update(`ccu-demo/${repo}:${ref}`).digest('hex')

const MANIFEST_TYPE = 'application/vnd.docker.distribution.manifest.v2+json'

export function start(port = 0) {
  const server = http.createServer((req, res) => {
    const { pathname } = new URL(req.url, 'http://localhost')

    if (pathname === '/v2' || pathname === '/v2/') {
      res.writeHead(200, { 'Content-Type': 'application/json' })
      return res.end('{}')
    }

    const tags = pathname.match(/^\/v2\/(.+)\/tags\/list$/)
    if (tags) {
      const repo = tags[1]
      if (!REPOS[repo]) return notFound(res)
      const body = JSON.stringify({ name: repo, tags: REPOS[repo] })
      res.writeHead(200, { 'Content-Type': 'application/json', 'Content-Length': Buffer.byteLength(body) })
      return res.end(body)
    }

    const manifest = pathname.match(/^\/v2\/(.+)\/manifests\/(.+)$/)
    if (manifest) {
      const [, repo, ref] = manifest
      if (!REPOS[repo]) return notFound(res)
      // A digest reference resolves to itself; a tag resolves to its own digest.
      const known = ref.startsWith('sha256:') || REPOS[repo].includes(ref) || ref === 'latest'
      if (!known) return notFound(res)
      const dgst = ref.startsWith('sha256:') ? ref : digestFor(repo, ref === 'latest' ? REPOS[repo].at(-1) : ref)
      const body = JSON.stringify({ schemaVersion: 2, mediaType: MANIFEST_TYPE })
      res.writeHead(200, {
        'Content-Type': MANIFEST_TYPE,
        'Docker-Content-Digest': dgst,
        'Content-Length': Buffer.byteLength(body),
      })
      // A HEAD carries the headers only; a GET may as well answer too.
      return res.end(req.method === 'HEAD' ? undefined : body)
    }

    notFound(res)
  })

  return new Promise((resolve) => {
    server.listen(port, '127.0.0.1', () => resolve({ server, host: `127.0.0.1:${server.address().port}` }))
  })
}

function notFound(res) {
  const body = JSON.stringify({ errors: [{ code: 'NAME_UNKNOWN', message: 'unknown repository' }] })
  res.writeHead(404, { 'Content-Type': 'application/json', 'Content-Length': Buffer.byteLength(body) })
  res.end(body)
}

// Run standalone for poking at it by hand: node demo/fake-registry.mjs 5099
if (import.meta.url === `file://${process.argv[1]}`) {
  const { host } = await start(Number(process.argv[2] ?? 0))
  console.log(`fake registry on http://${host}`)
}
