import { readFileSync } from 'node:fs'
import { test } from 'node:test'
import assert from 'node:assert/strict'

const source=readFileSync(new URL('../src/App.svelte',import.meta.url),'utf8')
const html=readFileSync(new URL('../index.html',import.meta.url),'utf8')
const favicon=readFileSync(new URL('../public/favicon.svg',import.meta.url),'utf8')
const backend=readFileSync(new URL('../../cmd/cloudflix/main.go',import.meta.url),'utf8')
const dialog=source.slice(source.indexOf('{#if fileViewer}'),source.indexOf('{#if selected}',source.indexOf('{#if fileViewer}')))
const downloader=source.slice(source.indexOf('<div class="dashboard-content download-page">'),source.indexOf('   {/if}\n   <footer'))

test('browser favicon matches branded Cloudflix film icon',()=>{
 assert.match(html, /<link rel="icon" type="image\/svg\+xml" href="\/favicon\.svg\?v=1">/)
 assert.match(favicon, /<svg[^>]*viewBox="0 0 64 64"/)
 assert.match(favicon, /fill="url\(#cloudflix-red\)"/)
 assert.match(favicon, /stroke="#ffffff"/)
})

test('file inspection is available for all download statuses including rejected torrents',()=>{
 assert.match(downloader, /class="queue-files-button"/)
 assert.match(downloader, /onclick=\{\(\)=>showFiles\(m\)\}/)
 assert.match(downloader, /aria-label=\{'View files for '\+m\.title\}/)
 const actions=downloader.slice(downloader.indexOf('<div class="download-actions queue-actions">'))
 assert.ok(actions.indexOf('class="queue-files-button"')>actions.indexOf("{/if}"))
 assert.match(backend, /GET \/api\/torrents\/\{hash\}\/files",a\.require\(a\.torrentFiles\)/)
})

test('file inspector explains rejections and shows selected versus skipped file names',()=>{
 assert.match(dialog, /aria-label=\{'Torrent files for '\+fileViewer\.title\}/)
 assert.match(dialog, /\{#if fileViewer\.status==='rejected'\}/)
 assert.match(dialog, /inspectedFiles\.files as file/)
 assert.match(dialog, /file\.path/)
 assert.match(dialog, /file\.selected\?'Selected'/)
 assert.match(dialog, /No files downloaded/)
 assert.match(dialog, /inspectedFiles\?\.message/)
 assert.match(dialog, /inspectedFiles\.truncated/)
 assert.match(dialog, /onclick=\{closeFileViewer\}/)
 assert.doesNotMatch(dialog, /{@html/)
})

test('torrent file details are fetched on demand rather than in the live SSE snapshot',()=>{
 assert.match(source, /call\('torrents\/'\+encodeURIComponent\(hash\)\+'\/files'\)/)
 assert.match(source, /if\(fileViewer\?\.hash===hash\)/)
})

test('rejected files inspection shows metadata loading and retries without deleting torrent',()=>{
 assert.match(source,/type FileInspection = \{available:boolean,fetching\?:boolean/)
 assert.match(source,/const fileInspectionPoll=setInterval/)
 assert.match(source,/inspectedFiles\?\.fetching/)
 assert.match(source,/loadFileList\(fileViewer\.hash,false,true\)/)
 assert.match(source,/const retryFileInspection=async/)
 assert.match(source,/onclick=\{retryFileInspection\}/)
 assert.match(dialog,/\{:\else if inspectedFiles\?\.fetching\}/)
 assert.match(dialog,/Only torrent metadata is requested/)
 assert.doesNotMatch(dialog,/Remove this rejected entry and add the magnet again/)
})
