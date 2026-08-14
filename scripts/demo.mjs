#!/usr/bin/env node
// Records demo/ccu.tape into assets/demo.gif.
//
// The tape is never run directly. This driver stands up a throwaway world
// first — a freshly built binary, invented Compose stacks under $TMPDIR, a
// throwaway HOME, and a fake registry serving invented tags — so the recording
// never touches the real Docker Hub, the real config, or anyone's stacks.
//
// Usage:
//   node scripts/demo.mjs              record assets/demo.gif
//   node scripts/demo.mjs --keep       leave the throwaway world in place
//
// Note: the tape uses Copy/Paste, which writes to the real system clipboard.
// Recording will clobber whatever you had on it.
import { spawn, spawnSync } from 'node:child_process'
import fs from 'node:fs'
import os from 'node:os'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

import { start as startRegistry } from '../demo/fake-registry.mjs'
import { seed } from '../demo/seed.mjs'

const repo = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..')
const args = process.argv.slice(2)
const keep = args.includes('--keep')

const world = path.join(os.tmpdir(), 'ccu-demo')
const bin = path.join(world, 'bin')
const home = path.join(world, 'home')
const stacks = path.join(world, 'stacks')
// --tape lets a scratch tape borrow the same throwaway world while iterating.
const tapeFlag = args.indexOf('--tape')
const tape = tapeFlag === -1 ? path.join(repo, 'demo', 'ccu.tape') : path.resolve(args[tapeFlag + 1])

fs.rmSync(world, { recursive: true, force: true })
fs.mkdirSync(bin, { recursive: true })
fs.mkdirSync(home, { recursive: true })
seed(stacks)
log(`world     ${world}`)

// Built fresh every run: a demo recorded from a stale binary shows a UI that no
// longer exists. `Require ccu` in the tape is checked against vhs's own PATH,
// before any shell exists to source setup.sh — hence bin on PATH here too.
run('go', ['build', '-o', path.join(bin, 'ccu'), '.'], { cwd: repo })
log('binary    built')

const { server, host } = await startRegistry(0)
log(`registry  http://${host} (fake)`)

fs.mkdirSync(path.join(repo, 'assets'), { recursive: true })

const env = {
  ...process.env,
  PATH: `${bin}:${process.env.PATH}`,
  DEMO_SETUP: path.join(repo, 'demo', 'setup.sh'),
  CCU_DEMO_HOME: home,
  CCU_DEMO_BIN: bin,
  CCU_DEMO_STACKS: stacks,
  CCU_REGISTRY_HOST: host,
}

// Catches syntax errors in a second, instead of after a two-minute run.
run('vhs', ['validate', tape], { cwd: repo, env })
log('tape      valid')

log('recording …')
const code = await spawnAsync('vhs', [tape], { cwd: repo, env, stdio: 'inherit' })

server.close()
if (!keep) fs.rmSync(world, { recursive: true, force: true })
else log(`kept      ${world}`)

if (code !== 0) process.exit(code)
log('done      assets/demo.gif — now watch it, an exit code proves nothing')

function log(msg) {
  console.log(`\x1b[38;5;183m›\x1b[0m ${msg}`)
}

function run(cmd, argv, opts) {
  const r = spawnSync(cmd, argv, { stdio: 'inherit', ...opts })
  if (r.error) throw r.error
  if (r.status !== 0) {
    console.error(`\n${cmd} ${argv.join(' ')} failed (exit ${r.status})`)
    process.exit(r.status ?? 1)
  }
}

function spawnAsync(cmd, argv, opts) {
  return new Promise((resolve, reject) => {
    const p = spawn(cmd, argv, opts)
    p.on('error', reject)
    p.on('close', resolve)
  })
}
