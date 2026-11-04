import {test} from 'node:test'
import assert from 'node:assert/strict'
import {readFileSync} from 'node:fs'
import {copyToClipboard} from '../src/lib/clipboard.js'

const component=readFileSync(new URL('../src/App.svelte',import.meta.url),'utf8')
const goRoute=readFileSync(new URL('../../cmd/cloudflix/main.go',import.meta.url),'utf8')
const downloader=component.slice(component.indexOf('<div class="dashboard-content download-page">'),component.indexOf('   {/if}\n   <footer'))

test('copy magnet sits directly before delete on every queue row',()=>{
 const copy=downloader.indexOf('class="queue-copy-button"')
 const remove=downloader.indexOf('class="danger" title={\'Remove \'+m.title')
 assert.ok(copy>0 && remove>copy,'Copy button must precede Delete')
 assert.match(downloader,/onclick=\{\(\)=>copyTorrentMagnet\(m\)\}/)
 assert.match(downloader,/aria-label=\{'Copy magnet link for '\+m\.title\}/)
})

test('magnet links are retrieved on demand via authenticated endpoint',()=>{
 assert.match(goRoute,/GET \/api\/torrents\/\{hash\}\/magnet",a\.require\(a\.torrentMagnet\)/)
 assert.match(component,/call\('torrents\/'\+encodeURIComponent\(movie\.hash\)\+'\/magnet'\)/)
 assert.match(component,/notify\('Magnet link copied'\)/)
})

test('copy validates magnet scheme',async()=>{
 assert.equal(await copyToClipboard('https://example.com'),false)
 assert.equal(await copyToClipboard(''),false)
})
