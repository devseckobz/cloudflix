import {readFileSync} from 'node:fs'
import {test} from 'node:test'
import assert from 'node:assert/strict'

const component=readFileSync(new URL('../src/App.svelte',import.meta.url),'utf8').replace(/\r\n/g,'\n')
const confirmSource=readFileSync(new URL('../src/lib/components/ui/alert-dialog/confirm.ts',import.meta.url),'utf8')
const hostSource=readFileSync(new URL('../src/lib/components/ui/alert-dialog/confirm-host.svelte',import.meta.url),'utf8')

test('removing a torrent asks whether to delete the files, instead of assuming',()=>{
 // Removing used to retain files silently, leaving no way to reclaim the disk
 // from the UI. The dialog must offer both outcomes.
 assert.match(component,/alternateLabel:'Delete files too'/)
 assert.match(component,/confirmLabel:'Remove from queue'/)
 assert.match(component,/description:'The queue entry is removed either way/)
 assert.match(component,/deleteFiles=choice==='alternate'/)
})

test('the delete-files choice is sent to the server as an explicit flag',()=>{
 assert.match(component,/await call\('torrents\/'\+encodeURIComponent\(m\.hash\)\+'\/'\+verb,'POST',verb==='remove'\?\{deleteFiles\}:\{\}\)/)
 // The two outcomes are reported distinctly, so the user can tell what happened.
 assert.match(component,/notify\(deleteFiles\?'Torrent and downloaded files deleted':'Torrent removed\. Files kept on the server\.'\)/)
})

test('a rejected torrent keeps the plain two-button dialog',()=>{
 // Nothing was ever downloaded, so offering "delete files" would be a lie.
 assert.match(component,/title:'Delete this rejected torrent\?'/)
 assert.match(component,/Its record and temporary file list will be removed\./)
})

test('confirmDialog supports a third choice and reports cancellation explicitly',()=>{
 assert.match(confirmSource,/alternateLabel\?: string/)
 assert.match(confirmSource,/export type ConfirmResult = 'cancel' \| 'confirm' \| 'alternate'/)
 // Cancelling the three-way dialog must not be mistaken for either action.
 assert.match(component,/if\(choice==='cancel'\|\|choice===false\)return/)
})

test('two-button call sites compare strictly, so a string answer cannot pass as yes',()=>{
 // `if(!ok)` would treat the truthy 'cancel' string as confirmation.
 assert.doesNotMatch(component,/if\(!ok\)return/)
 assert.match(component,/if\(ok!==true\)return/)
})

test('the confirm host renders the alternate action and settles a tri-state answer',()=>{
 assert.match(hostSource,/\{#if request\?\.alternateLabel\}/)
 assert.match(hostSource,/onclick=\{\(\)\s*=>\s*answer\('alternate'\)\}/)
 assert.match(hostSource,/onclick=\{\(\)\s*=>\s*answer\('cancel'\)\}/)
 // Escape / overlay dismissal must resolve as cancellation.
 assert.match(hostSource,/onOpenChange=\{\(next\) => \{ if \(!next\) answer\('cancel'\) \}\}/)
})

test('a plain two-button dialog still resolves a boolean',()=>{
 // settleConfirm must not leak the tri-state strings to existing callers.
 assert.match(confirmSource,/request\.alternateLabel\n\t\t\t\t\? answer\n\t\t\t\t: answer === 'confirm'/)
})
