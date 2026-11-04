import { readFileSync } from 'node:fs'
import { test } from 'node:test'
import assert from 'node:assert/strict'

const source=readFileSync(new URL('../src/App.svelte',import.meta.url),'utf8')
const css=readFileSync(new URL('../src/app.css',import.meta.url),'utf8')
const start=source.indexOf('<div class="dashboard-content download-page">')
const end=source.indexOf('   {/if}\n   <footer',start)
assert.ok(start>0 && end>start, 'Downloader section must exist')
const downloader=source.slice(start,end)
const header=source.slice(source.indexOf('<header class="topbar">'),source.indexOf('</header>',source.indexOf('<header class="topbar">'))+9)
const home=source.slice(source.indexOf('{#if page===\'home\'}',source.indexOf('   {#if error}')),source.indexOf("{:else if page==='settings'}"))

test('Downloader uses compact stats, a simple magnet form, and a filtered queue',()=>{
 assert.match(downloader,/class="downloader-heading"/)
 assert.match(downloader,/class="stat-grid downloader-stats"/)
 assert.match(downloader,/class="magnet-form downloader-magnet-form"/)
 assert.match(downloader,/class="download-queue-section"/)
 assert.match(downloader,/class="download-queue-filters"/)
 assert.match(source,/let queueFilter = \$state/)
 assert.match(source,/const queueItems = \$derived/)
 assert.match(downloader,/\{#each queueItems as m \(m\.hash\)\}/)
 assert.doesNotMatch(downloader,/100\.0% complete/)
})

test('Completed downloads show a watch action, not an oversized progress bar',()=>{
 assert.match(downloader,/\{#if m\.status==='completed'\}/)
 assert.match(downloader,/class="queue-watch-button"/)
 assert.match(downloader,/Ready to watch/)
 assert.match(downloader,/\{#if m\.status==='downloading'\|\|m\.status==='paused'\}/)
 assert.match(downloader,/class="queue-progress"/)
})

test('Movie page buttons sit above the centered results count',()=>{
 assert.doesNotMatch(header,/top-library-pagination/)
 assert.match(home,/class="library-pagination-footer"/)
 assert.match(home,/class="library-bottom-pagination"/)
 assert.match(home,/class:active=\{libraryView\.page===number\}/)
 assert.match(home,/class="library-results-count"/)
 assert.match(css,/\.library-results-count\s*\{[\s\S]*?text-align:center;[\s\S]*?border:0;/)
 assert.ok(home.indexOf('class="library-bottom-pagination"')<home.indexOf('class="library-results-count"'))
 assert.doesNotMatch(home,/class="movie-library-pagination"/)
})

test('Footer credits Kobz',()=>{
 assert.match(source,/© Cloudflix · Made with ❤️ by Kobz/)
 assert.doesNotMatch(source,/Self-hosted with ❤️/)
})
