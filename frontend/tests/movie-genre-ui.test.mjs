import { readFileSync } from 'node:fs'
import { test } from 'node:test'
import assert from 'node:assert/strict'

const component=readFileSync(new URL('../src/App.svelte',import.meta.url),'utf8')
const styles=readFileSync(new URL('../src/app.css',import.meta.url),'utf8')
const theme=readFileSync(new URL('../src/theme.css',import.meta.url),'utf8')
const home=component.slice(component.indexOf('<section class="movie-library"'),component.indexOf("{:else if page==='settings'}"))

test('movie cards show separate red year and warm-yellow genre badges',()=>{
 assert.match(component,/genre:string/)
 assert.match(home,/class="movie-library-tags"/)
 assert.match(home,/class="movie-year-badge"/)
 assert.match(home,/class="movie-genre-badge"/)
 assert.match(home,/\{movie\.year\|\|'Unknown year'\}/)
 assert.match(home,/\{#if movie\.genre\}/)
 assert.match(home,/\{movie\.genre\}/)
 // The two badge colours are semantic tokens, not literal hex values.
 assert.match(theme,/--year:\s*oklch\(/)
 assert.match(theme,/--genre:\s*oklch\(/)
 assert.match(styles,/\.movie-year-badge\s*\{[^}]*color:\s*var\(--year\)/)
 assert.match(styles,/\.movie-genre-badge\s*\{[^}]*color:\s*var\(--genre\)/)
})

test('genre badges appear in player and queue without changing ratings',()=>{
 assert.match(component,/class="movie-year-badge">\{selected\.year/)
 assert.match(component,/class="movie-genre-badge">\{selected\.genre/)
 assert.match(component,/class="movie-year-badge">\{m\.year/)
 assert.match(component,/class="movie-genre-badge">\{m\.genre/)
 assert.match(component,/movie\.rating\.toFixed\(1\)/)
})

test('genre badge is omitted when TMDB has no known genre',()=>{
 assert.match(home,/\{#if movie\.genre\}<span class="movie-genre-badge">\{movie\.genre\}<\/span>\{\/if\}/)
 assert.doesNotMatch(home,/movie-library-genre-sep/)
})
