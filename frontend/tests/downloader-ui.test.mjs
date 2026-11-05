import { readFileSync } from 'node:fs'
import { test } from 'node:test'
import assert from 'node:assert/strict'

const component=readFileSync(new URL('../src/App.svelte',import.meta.url),'utf8').replace(/\r\n/g,'\n')
const css=readFileSync(new URL('../src/app.css',import.meta.url),'utf8')
const start=component.indexOf('<div class="dashboard-content download-page">')
const end=component.indexOf('   {/if}\n   <footer',start)
assert.ok(start>0 && end>start, 'Downloader section must exist')
const downloader=component.slice(start,end)
const header=component.slice(component.indexOf('<header class="topbar">'),component.indexOf('</header>',component.indexOf('<header class="topbar">'))+9)
const home=component.slice(component.indexOf("{#if page==='home'}"),component.indexOf("{:else if page==='settings'}"))

test('Downloader uses compact stats, a simple magnet form, and a filtered queue',()=>{
 assert.match(downloader,/class="downloader-heading"/)
 assert.match(downloader,/class="stat-grid downloader-stats"/)
 assert.match(downloader,/class="magnet-form downloader-magnet-form"/)
 assert.match(downloader,/class="download-queue-section"/)
 assert.match(downloader,/class="download-queue-filters"/)
 assert.match(component,/let queueFilter = \$state/)
 assert.match(component,/const queueItems = \$derived/)
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

test('Downloader queue actions are shadcn buttons and tokenised icon buttons',()=>{
 // Watch and Files use the shadcn Button; pause/copy/remove are token-driven icon buttons.
 assert.match(downloader,/<Button variant="outline" size="sm" class="queue-watch-button"/)
 assert.match(downloader,/<Button variant="ghost" size="sm" class="queue-files-button"/)
 assert.match(downloader,/class="queue-copy-button queue-icon-button"/)
 assert.match(downloader,/class="danger queue-icon-button"/)
 assert.match(css,/\.queue-icon-button\s*\{[\s\S]*?border:\s*1px solid var\(--border\)/)
 assert.match(css,/\.queue-icon-button\.danger:hover\s*\{[\s\S]*?var\(--destructive\)/)
})

test('Movie page buttons sit above the centered results count',()=>{
 assert.doesNotMatch(header,/top-library-pagination/)
 assert.match(home,/class="library-pagination-footer"/)
 assert.match(home,/class="library-bottom-pagination"/)
 assert.match(home,/class:active=\{libraryView\.page===number\}/)
 assert.match(home,/class="library-results-count"/)
 assert.match(css,/\.library-results-count\s*\{[\s\S]*?text-align:\s*center;[\s\S]*?border:\s*0;/)
 assert.ok(home.indexOf('class="library-bottom-pagination"')<home.indexOf('class="library-results-count"'))
 assert.doesNotMatch(home,/class="movie-library-pagination"/)
})

test('Footer credits Kobz',()=>{
 assert.match(component,/© Cloudflix · Made with ❤️ by Kobz/)
 assert.doesNotMatch(component,/Self-hosted with ❤️/)
})
