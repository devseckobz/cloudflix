import {readFileSync} from 'node:fs'
import {test} from 'node:test'
import assert from 'node:assert/strict'

const component=readFileSync(new URL('../src/App.svelte',import.meta.url),'utf8').replace(/\r\n/g,'\n')
const css=readFileSync(new URL('../src/app.css',import.meta.url),'utf8')
const start=component.indexOf('<div class="dashboard-content download-page">')
const end=component.indexOf('   {/if}\n   <footer',start)
assert.ok(start>0 && end>start,'Downloader section must exist')
const downloader=component.slice(start,end)

test('the Downloader offers a cleanup action for files no torrent owns',()=>{
 assert.match(component,/const scanLeftovers=async\(\)=>/)
 assert.match(component,/await call\('storage\/leftovers'\)/)
 assert.match(component,/const cleanUp=async\(\)=>/)
 assert.match(component,/await call\('storage\/cleanup','POST',\{\}\)/)
 // The button is always present and disabled when nothing is reclaimable: a
 // failed delete leaves files behind, and hiding the action then would leave no
 // way to reclaim the space.
 assert.match(downloader,/class="cleanup-button"/)
 assert.match(downloader,/disabled=\{cleanupBusy\|\|leftoverCount===0\}/)
 assert.match(downloader,/Clean up\{#if leftoverCount>0\} \{formatBytes\(leftoverBytes\)\}\{\/if\}/)
 assert.doesNotMatch(downloader,/\{#if leftoversChecked && leftoverCount>0\}/)
})

test('Clean up sits to the left of the filter toggles in one toolbar group',()=>{
 const tools=downloader.indexOf('class="download-queue-tools"')
 const cleanup=downloader.indexOf('class="cleanup-button"')
 const filters=downloader.indexOf('class="download-queue-filters"')
 assert.ok(tools>0 && cleanup>tools && filters>cleanup,'Clean up must precede the filters inside the tools group')
 assert.match(css,/\.download-queue-tools\s*\{[\s\S]*?display:\s*inline-flex/)
})

test('cleanup always confirms before deleting and reports what it freed',()=>{
 assert.match(component,/title:'Delete '\+leftoverCount\+' unused '/)
 assert.match(component,/no longer belong to any download in your queue/)
 assert.match(component,/if\(ok!==true\)return/)
 assert.match(component,/notify\('Deleted '\+deleted\+' '/)
})

test('the scan runs when the Downloader opens, and is read-only',()=>{
 assert.match(component,/if\(destination==='downloader'\)void scanLeftovers\(\)/)
 // A refused scan (queue still loading metadata) must not surface an error.
 assert.match(component,/catch\{\/\* A queue still loading metadata refuses the scan/)
})

test('the cleanup button is styled as a compact, non-stretching control',()=>{
 assert.match(css,/\.cleanup-button\s*\{[\s\S]*?flex:\s*0 0 auto/)
 assert.match(css,/\.cleanup-button\s*\{[\s\S]*?height:\s*30px/)
})

test('the magnet Add button cannot grow when the textarea is resized',()=>{
 // `align-items: stretch` used to make the button follow the resizable field.
 assert.match(css,/\.magnet-entry-row\s*\{\s*display:\s*flex;\s*align-items:\s*flex-start;/)
 assert.match(css,/\.magnet-add-button\s*\{[\s\S]*?height:\s*42px/)
 assert.match(css,/\.magnet-add-button\s*\{[\s\S]*?align-self:\s*flex-start/)
 // The base rule must not stretch. The phone breakpoint stacks the row into a
 // column, where stretching sets width, not height, so it is allowed there.
 const baseRule=css.slice(css.indexOf('.magnet-add-button {'),css.indexOf('.magnet-add-button {')+240)
 assert.doesNotMatch(baseRule,/align-self:\s*stretch/)
 assert.doesNotMatch(baseRule,/height:\s*auto/)
})
