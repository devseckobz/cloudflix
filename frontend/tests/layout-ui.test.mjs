import { readFileSync } from 'node:fs'
import { test } from 'node:test'
import assert from 'node:assert/strict'

const css=readFileSync(new URL('../src/app.css',import.meta.url),'utf8')
const component=readFileSync(new URL('../src/App.svelte',import.meta.url),'utf8')
const home=component.slice(component.indexOf("{#if page==='home'}"),component.indexOf("{:else if page==='settings'}"))

test('scrollbar is slim and dark with hover contrast',()=>{
 assert.match(css,/::-webkit-scrollbar\s*\{width:9px;height:9px\}/)
 assert.match(css,/scrollbar-width:thin/)
 assert.match(css,/::-webkit-scrollbar-thumb:hover/)
 assert.match(css,/::-webkit-scrollbar-track\s*\{background:#0b1119\}/)
})

test('nav carries movie search, while Home is posters only',()=>{
 const topbar=component.slice(component.indexOf('<header class="topbar">'),component.indexOf('</header>',component.indexOf('<header class="topbar">'))+9)
 assert.match(topbar,/class="nav-movie-search"/)
 assert.doesNotMatch(topbar,/class="avatar"/)
 assert.doesNotMatch(topbar,/class="version"/)
 assert.doesNotMatch(home,/movie-library-toolbar|movie-library-heading|movie-library-search/)
 assert.match(home,/class="movie-library-grid"/)
})
