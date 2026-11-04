import { readFileSync } from 'node:fs'
import { test } from 'node:test'
import assert from 'node:assert/strict'

const component=readFileSync(new URL('../src/App.svelte',import.meta.url),'utf8')
const styles=readFileSync(new URL('../src/app.css',import.meta.url),'utf8')
const home=component.slice(component.indexOf('<section class="movie-library"'),component.indexOf("{:else if page==='settings'}"))

test('movie cards show release year and primary genre in accent color',()=>{
 assert.match(component,/genre:string/)
 assert.match(home,/class="movie-library-year-genre"/)
 assert.match(home,/\{movie\.year\|\|'Unknown year'\}/)
 assert.match(home,/\{#if movie\.genre\}/)
 assert.match(home,/\{movie\.genre\}/)
 assert.match(styles,/\.movie-library-year-genre,\.queue-year-genre,\.cinema-movie-year-genre\s*\{[\s\S]*?color:#ff6260/)
})

test('genres appear in theater and download queue without changing rating behavior',()=>{
 assert.match(component,/class="cinema-movie-year-genre"/)
 assert.match(component,/class="queue-year-genre"/)
 assert.match(component,/movie\.rating\.toFixed\(1\)/)
 assert.match(home,/class="movie-library-rating"/)
})

test('movie cards omit separator when TMDB has no known genre',()=>{
 assert.match(home,/\{#if movie\.genre\}<span class="movie-library-genre-sep"> · <\/span>\{movie\.genre\}\{\/if\}/)
})
