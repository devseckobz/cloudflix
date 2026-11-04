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
 assert.match(footer,/src=\{sub\.src\}/)
 assert.match(component,/mediaURL\(selected\.hash,s\.path\)/)
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

test('theater ignores backdrop clicks and only its close button dismisses it',()=>{
 const backdrop=footer.match(/<div class="modal-backdrop cinema-player-backdrop"[^>]*>/)?.[0]
 assert.ok(backdrop,'theater backdrop must be present')
 assert.doesNotMatch(backdrop,/onclick=/)
 assert.match(footer,/<button class="cinema-player-close" onclick=\{closeMovie\} aria-label="Close player"/)
 assert.equal((footer.match(/onclick=\{closeMovie\}/g)||[]).length,1)
})

test('CC toggle reveals subtitle shortcuts and only one text track is active',()=>{
 assert.match(component,/let ccEnabled = \$state\(false\)/)
 assert.match(component,/let selectedSubtitle = \$state\(0\)/)
 assert.match(component,/applySubtitleSelection\(playerVideo,ccEnabled\?selectedSubtitle:-1\)/)
 assert.match(component,/const toggleCaptions=/)
 assert.match(component,/const selectCaptionTrack=/)
 assert.match(footer,/class="cinema-cc-button"/)
 assert.match(footer,/aria-pressed=\{ccEnabled\}/)
 assert.match(footer,/onclick=\{toggleCaptions\}/)
 assert.match(footer,/\{#if ccEnabled\}[\s\S]*class="cinema-caption-languages"/)
 assert.match(footer,/role="group" aria-label="Available subtitles"/)
 assert.match(footer,/onclick=\{\(\)=>selectCaptionTrack\(i\)\}/)
 assert.match(footer,/onloadedmetadata=\{syncSubtitleTracks\}/)
 assert.match(footer,/onload=\{syncSubtitleTracks\}/)
 assert.doesNotMatch(footer,/default=\{i===0\}/)
})

test('MKV playback prepares a browser-compatible source and makes embedded CC tracks available',()=>{
 assert.match(component,/const prepareMKV=async\(movie:Movie\)/)
 assert.ok(component.includes("call('playback/'+encodeURIComponent(movie.hash)+'/prepare'"))
 assert.match(component,/class="cinema-player-preparing"/)
 assert.match(component,/selectedIsMKV/)
 assert.match(component,/playbackVideoURL/)
 assert.match(component,/mkvPlaybackPoll=setInterval/)
 assert.match(footer,/\{#each playbackTracks as sub,i \(sub\.path\)\}/)
})
