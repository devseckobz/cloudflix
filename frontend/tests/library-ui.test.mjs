import { readFileSync } from 'node:fs'
import { test } from 'node:test'
import assert from 'node:assert/strict'

const component=readFileSync(new URL('../src/App.svelte',import.meta.url),'utf8')
const homeStart=component.indexOf("{#if page==='home'}")
const homeEnd=component.indexOf("{:else if page==='settings'}",homeStart)
const downloadStart=component.indexOf('<div class="dashboard-content download-page">')
assert.ok(homeStart>=0 && homeEnd>homeStart && downloadStart>homeEnd)
const home=component.slice(homeStart,homeEnd)
const topbar=component.slice(component.indexOf('<header class="topbar">'),component.indexOf('</header>',component.indexOf('<header class="topbar">'))+9)
const download=component.slice(downloadStart,component.indexOf('   {/if}\n   <footer',downloadStart))

test('Home shows completed movie cards with title, year and TMDB ratings',()=>{
 assert.match(component,/selectLibraryPage\(movies,search,libraryPage,10\)/)
 assert.match(home,/libraryView\.movies/)
 assert.match(home,/movie\.rating\.toFixed\(1\)/)
 assert.match(home,/movie\.year/)
 assert.match(topbar,/Search downloaded movies/)
 assert.doesNotMatch(home,/class="stat-grid"/)
 assert.doesNotMatch(home,/class="hero"/)
 assert.doesNotMatch(home,/YOUR PERSONAL MOVIE HUB/)
 assert.doesNotMatch(home,/PRIVATE SCREENING ROOM/)
 assert.doesNotMatch(home,/The collection/)
 assert.doesNotMatch(home,/shelf-divider/)
 assert.doesNotMatch(home,/class="movie-library-toolbar"/)
 assert.doesNotMatch(home,/movie-library-heading/)
 assert.doesNotMatch(home,/movie-library-search/)
 assert.doesNotMatch(home,/<h1>Movies<\/h1>/)
 assert.match(home,/class="movie-library-grid"/)
 assert.match(home,/class="movie-library-play"/)
 assert.match(home,/openMovie\(movie\)/)
})

test('Home has real previous, next, and numbered pages in the top navigation',()=>{
 assert.match(topbar,/class="top-library-pagination"/)
 assert.match(topbar,/libraryView\.totalPages/)
 assert.match(topbar,/libraryView\.pages/)
 assert.match(topbar,/libraryPage=libraryView\.page-1/)
 assert.match(topbar,/libraryPage=libraryView\.page\+1/)
 assert.match(topbar,/libraryPage=number/)
 assert.doesNotMatch(home,/movie-pagination-buttons|movie-library-pagination/)
 assert.match(home,/class="library-results-count"/)
 assert.match(home,/Showing \{libraryView\.from\}–\{libraryView\.to\} of \{libraryView\.total\}/)
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

test('movie search appears once in top navigation, not above the movie grid',()=>{
 assert.match(topbar,/class="nav-movie-search"/)
 assert.match(topbar,/value=\{search\}/)
 assert.match(topbar,/libraryPage=1/)
 assert.doesNotMatch(topbar,/class="top-right"/)
 assert.doesNotMatch(topbar,/class="avatar"/)
 assert.doesNotMatch(topbar,/class="version"/)
 assert.equal((component.match(/aria-label="Search downloaded movies"/g)||[]).length,1)
})
