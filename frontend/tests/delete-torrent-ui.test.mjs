import {readFileSync} from 'node:fs'
import {test} from 'node:test'
import assert from 'node:assert/strict'

const component=readFileSync(new URL('../src/App.svelte',import.meta.url),'utf8').replace(/\r\n/g,'\n')

test('deleting a torrent is a single confirmed action with total cleanup',()=>{
 // One button. No "Remove from queue" / "Delete files too" split to choose from.
 assert.match(component,/confirmLabel:'Delete'/)
 assert.doesNotMatch(component,/alternateLabel:'Delete files too'/)
 assert.doesNotMatch(component,/confirmLabel:'Remove from queue'/)
})

test('the confirmation says the files go too, and cannot be undone',()=>{
 assert.match(component,/Its queue entry and downloaded files will be deleted from the server\. This cannot be undone\./)
})

test('a bodyless delete still requests file removal explicitly',()=>{
 assert.match(component,/await call\('torrents\/'\+encodeURIComponent\(m\.hash\)\+'\/'\+verb,'POST',verb==='remove'\?\{deleteFiles:true\}:\{\}\)/)
 assert.match(component,/notify\('Torrent and downloaded files deleted'\)/)
})

test('a rejected torrent states there is nothing to delete',()=>{
 assert.match(component,/Its queue entry and temporary file list will be removed\./)
})

test('cancelling deletes nothing',()=>{
 assert.match(component,/if\(ok!==true\)return/)
})
