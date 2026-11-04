import {readFileSync} from 'node:fs'
import {test} from 'node:test'
import assert from 'node:assert/strict'

const component=readFileSync(new URL('../src/App.svelte',import.meta.url),'utf8')
const footer=component.slice(component.indexOf(' {#if selected}'))

test('movie click opens integrated MP4 theater with fullscreen and captions',()=>{
 assert.match(component,/onclick=\{\(\)=>openMovie\(movie\)\}/)
 assert.match(footer,/<video[\s\S]*?controls[\s\S]*?autoplay[\s\S]*?playsinline/)
 assert.match(footer,/type="video\/mp4"/)
 assert.match(footer,/<track kind="subtitles"/)
 assert.match(footer,/mediaURL\(selected\.hash,sub\.path\)/)
 assert.match(footer,/onclick=\{enterFullscreen\}/)
 assert.match(component,/playerVideo\?\.pause\(\)/)
})

test('download manager displays rejected non-video torrents',()=>{
 assert.match(component,/m\.status==='rejected'/)
 assert.match(component,/torrent contains no \.mp4|Only MP4 movies and SRT subtitles/)
 assert.match(component,/m\.error/)
})

test('queued notification automatically expires',()=>{
 assert.match(component,/noticeTimer=setTimeout\(\(\)=>\{notice='';noticeTimer=undefined\},4200\)/)
 assert.match(component,/notify\('Magnet queued/)
})

test('theater has just a compact title, video, subtitle info and fullscreen action',()=>{
 assert.match(footer,/class="cinema-player-heading"/)
 assert.match(footer,/class="cinema-video-frame"/)
 assert.match(footer,/class="cinema-player-toolbar"/)
 assert.match(footer,/class="cinema-fullscreen-button"/)
 assert.doesNotMatch(footer,/FILES ON YOUR SERVER/)
 assert.doesNotMatch(footer,/class="cinema-player-files"/)
 assert.doesNotMatch(footer,/NOW SHOWING/)
 assert.doesNotMatch(footer,/READY TO WATCH/)
})
