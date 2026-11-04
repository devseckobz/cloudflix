import { test } from 'node:test'
import assert from 'node:assert/strict'
import { createVersionReloadMonitor } from '../src/lib/version-reload.js'

test('first server version establishes baseline and never reloads on mount', () => {
  let reloads = 0
  const watcher = createVersionReloadMonitor(() => { reloads++ })
  assert.equal(watcher.observe('v0.1.10'), false)
  assert.equal(watcher.observe('v0.1.10'), false)
  assert.equal(reloads, 0)
})

test('a new running version triggers exactly one full reload', () => {
  let reloads = 0
  const watcher = createVersionReloadMonitor(() => { reloads++ })
  watcher.observe('v0.1.10')
  assert.equal(watcher.observe('v0.1.11'), true)
  assert.equal(reloads, 1)
  assert.equal(watcher.observe('v0.1.11'), false)
  assert.equal(watcher.observe('v0.1.12'), false)
  assert.equal(reloads, 1)
})

test('invalid, missing and placeholder versions never force a reload', () => {
  let reloads = 0
  const watcher = createVersionReloadMonitor(() => { reloads++ })
  for (const value of ['', null, undefined, 42, 'loading']) assert.equal(watcher.observe(value), false)
  assert.equal(watcher.observe('v0.1.10'), false)
  assert.equal(reloads, 0)
})

test('app refresh and Settings update polling both feed the version monitor', () => {
  const app = (await import('node:fs')).readFileSync(new URL('../src/App.svelte', import.meta.url), 'utf8')
  assert.match(app, /createVersionReloadMonitor\(\(\)=>window\.location\.reload\(\)\)/)
  assert.match(app, /versionMonitor\.observe\(state\.version\)/)
  assert.match(app, /versionMonitor\.observe\(data\.currentVersion\)/)
})
