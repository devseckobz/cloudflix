import {readFileSync} from 'node:fs'
import {test} from 'node:test'
import assert from 'node:assert/strict'

const component=readFileSync(new URL('../src/App.svelte',import.meta.url),'utf8').replace(/\r\n/g,'\n')

// The trash button deletes the torrent *and its downloaded files*, so its label
// must not promise the milder "remove from queue". A tooltip that understates a
// destructive, irreversible action is a defect even when the action works.
test('the delete button is labelled as a delete, not a queue removal',()=>{
 assert.doesNotMatch(component,/Remove from queue/)
 assert.match(component,/Delete torrent and files/)
 assert.match(component,/title=\{'Delete '\+m\.title\+' and its files'\}/)
 assert.match(component,/aria-label=\{'Delete '\+m\.title\}/)
})

// The confirmation and the label must describe the same outcome: both delete the
// files, and the dialog offers no second "keep files" button (alternateLabel is
// the mechanism such a button would need).
test('the confirmation matches what the label promises',()=>{
 assert.match(component,/Its queue entry and downloaded files will be deleted from the server\. This cannot be undone\./)
 assert.doesNotMatch(component,/alternateLabel/)
})
