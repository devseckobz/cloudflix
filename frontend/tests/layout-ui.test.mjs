import { readFileSync } from 'node:fs'
import { test } from 'node:test'
import assert from 'node:assert/strict'

const component=readFileSync(new URL('../src/App.svelte',import.meta.url),'utf8')
const theme=readFileSync(new URL('../src/theme.css',import.meta.url),'utf8')
const home=component.slice(component.indexOf("{#if page==='home'}"),component.indexOf("{:else if page==='settings'}"))

test('scrollbar is slim and dark with hover contrast',()=>{
 assert.match(theme,/::-webkit-scrollbar\s*\{[\s\S]*?width:\s*10px[\s\S]*?height:\s*10px/)
 assert.match(theme,/scrollbar-width:\s*thin/)
 assert.match(theme,/scrollbar-color:\s*var\(--border\)\s*var\(--background\)/)
 assert.match(theme,/::-webkit-scrollbar-thumb:hover/)
 assert.match(theme,/::-webkit-scrollbar-track\s*\{\s*background:\s*var\(--background\)/)
})

test('nav carries movie search, while Home is posters only',()=>{
 const topbar=component.slice(component.indexOf('<header class="topbar">'),component.indexOf('</header>',component.indexOf('<header class="topbar">'))+9)
 assert.match(topbar,/class="nav-movie-search"/)
 assert.doesNotMatch(topbar,/class="avatar"/)
 assert.doesNotMatch(topbar,/class="version"/)
 assert.doesNotMatch(home,/movie-library-toolbar|movie-library-heading|movie-library-search/)
 assert.match(home,/class="movie-library-grid"/)
})

test('Home keeps a screen-reader heading without adding visible page chrome',()=>{
 assert.match(home,/<h1 class="sr-only">Your movie library<\/h1>/)
 assert.doesNotMatch(home,/<h1>Movies<\/h1>/)
 assert.doesNotMatch(home,/class="hero"/)
 assert.doesNotMatch(home,/stat-grid/)
})

test('dark theme tokens drive the palette instead of hard-coded hex values',()=>{
 assert.match(theme,/@custom-variant dark/)
 assert.match(theme,/--background:\s*oklch\(/)
 assert.match(theme,/--primary:\s*oklch\(/)
 assert.match(theme,/--sidebar:\s*oklch\(/)
 assert.match(theme,/@theme inline/)
 assert.match(theme,/--color-primary:\s*var\(--primary\)/)
 assert.match(theme,/--color-sidebar:\s*var\(--sidebar\)/)
 // No literal brand palette should leak into the application layer.
 assert.doesNotMatch(theme,/color-scheme:\s*light/)
})
