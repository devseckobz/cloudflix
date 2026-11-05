import { readFileSync } from 'node:fs'
import { test } from 'node:test'
import assert from 'node:assert/strict'

const component = readFileSync(new URL('../src/App.svelte', import.meta.url), 'utf8').replace(/\r\n/g, '\n')
const start = component.indexOf("{:else if auth==='login'}")
const end = component.indexOf('{:else}\n <div class="app-shell', start)
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

test('login stays minimal without any visible PIN label or heading', () => {
  assert.doesNotMatch(login, /Enter your PIN/)
  assert.doesNotMatch(login, /Unlock your movie library/)
  assert.doesNotMatch(login, /<label for="pin">/)
  assert.doesNotMatch(login, />6-digit access PIN</)
  assert.match(login, /aria-label="Access PIN"/)
  assert.match(login, /Continue/)
})

test('successful logout clears the session then navigates to the root URL', () => {
  const start = component.indexOf('const signOut=async()=>')
  const end = component.indexOf('const add=async()=>', start)
  assert.ok(start > 0 && end > start, 'logout handler must exist')
  const signOut = component.slice(start, end)
  assert.match(signOut, /await call\('auth\/logout','POST',\{\}\)/)
  assert.match(signOut, /window\.location\.replace\('\/'\)/)
  assert.doesNotMatch(signOut, /window\.history\.pushState/)
  assert.ok(signOut.indexOf("await call('auth/logout','POST',{})") < signOut.indexOf("window.location.replace('/')"), 'redirect only after server confirms logout')
  assert.match(signOut, /catch\(e\)/)
})
