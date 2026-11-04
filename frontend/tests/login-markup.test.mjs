import { readFileSync } from 'node:fs'
import { test } from 'node:test'
import assert from 'node:assert/strict'

const component = readFileSync(new URL('../src/App.svelte', import.meta.url), 'utf8')
const start = component.indexOf("{:else if auth==='login'}")
const end = component.indexOf('{:else}\n <div class="app-shell">', start)
assert.ok(start !== -1 && end > start, 'Login block should be present')
const login = component.slice(start, end)

test('PIN login uses app validation rather than native browser popup', () => {
  assert.match(login, /<form[^>]*\bnovalidate\b/)
  assert.doesNotMatch(login, /\bpattern=/)
  assert.doesNotMatch(login, /\bminlength=/)
  assert.doesNotMatch(login, /\brequired\b/)
  assert.match(login, /inputmode="numeric"/)
  assert.match(login, /maxlength="6"/)
})

test('login remains minimal, without heading or subtitle', () => {
  assert.doesNotMatch(login, /Enter your PIN/)
  assert.doesNotMatch(login, /Unlock your movie library/)
  assert.match(login, /6-digit access PIN/)
  assert.match(login, /Continue/)
})
