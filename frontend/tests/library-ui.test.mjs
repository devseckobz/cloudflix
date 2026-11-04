import { readFileSync } from 'node:fs'
import { test } from 'node:test'
import assert from 'node:assert/strict'

const component=readFileSync(new URL('../src/App.svelte',import.meta.url),'utf8')
const homeStart=component.indexOf("{#if page==='home'}")
const homeEnd=component.indexOf("{:else if page==='settings'}",homeStart)
const downloadStart=component.indexOf('<div class="dashboard-content download-page">')
assert.ok(homeStart>=0 && homeEnd>homeStart && downloadStart>homeEnd)
const home=component.slice(homeStart,homeEnd)
const download=component.slice(downloadStart,component.indexOf('   {/if}\n   <footer',downloadStart))

test('Home shows only completed movie cards with TMDB ratings, year and search',()=>{
 assert.match(component,/selectLibraryPage\(movies,search,libraryPage,10\)/)
 assert.match(home,/libraryView\.movies/)
 assert.match(home,/m\.rating\.toFixed\(1\)/)
 assert.match(home,/m\.year/)
 assert.match(home,/Search downloaded movies/)
 assert.doesNotMatch(home,/class="stat-grid"/)
 assert.doesNotMatch(home,/class="hero"/)
 assert.doesNotMatch(home,/YOUR PERSONAL MOVIE HUB/)
})

test('Home has real next, previous, and numbered pages',()=>{
 assert.match(home,/libraryView\.totalPages/)
 assert.match(home,/libraryView\.pages/)
 assert.match(home,/libraryPage=libraryView\.page-1/)
 assert.match(home,/libraryPage=libraryView\.page\+1/)
 assert.match(home,/libraryPage=number/)
})

test('Stats moved to Downloader and sidebar has no extra library section',()=>{
 assert.match(download,/class="stat-grid downloader-stats"/)
 assert.doesNotMatch(component,/YOUR LIBRARY/)
 assert.doesNotMatch(component,/All movies <span/)
 assert.match(component,/active>0/)
})

test('Downloader warns when TMDB is missing and links to settings',()=>{
 assert.match(download,/TMDB API key required/)
 assert.match(download,/navigate\('settings'\)/)
 assert.match(component,/tmdbChecked&&!tmdbConfigured/)
 assert.match(component,/call\('settings'\)/)
})
