import {readFileSync} from 'node:fs'
import {test} from 'node:test'
import assert from 'node:assert/strict'

const component=readFileSync(new URL('../src/App.svelte',import.meta.url),'utf8').replace(/\r\n/g,'\n')
const css=readFileSync(new URL('../src/app.css',import.meta.url),'utf8')
const settingsStart=component.indexOf("{:else if page==='settings'}")
const settingsEnd=component.indexOf('<div class="dashboard-content download-page">',settingsStart)
assert.ok(settingsStart>=0 && settingsEnd>settingsStart,'Settings section must exist')
const settings=component.slice(settingsStart,settingsEnd)
const theater=component.slice(component.indexOf(' {#if selected}'))

test('theater saves watch progress against the frozen session contract',()=>{
 // GET /api/session reads the resume point; POST /api/session/watching writes it.
 assert.match(component,/const loadResume=async\(\)=>[\s\S]*?await call\('session'\)/)
 assert.match(component,/await call\('session\/watching','POST',\{hash,position:/)
 // Throttled by a timer, plus an immediate save on pause.
 assert.match(component,/watchingTimer=setInterval\(saveProgressNow,5000\)/)
 assert.match(theater,/onpause=\{handlePause\}/)
 assert.match(component,/const handlePause=\(\)=>\{if\(!closingTheater\)saveProgressNow\(\)\}/)
 // Closing the theater clears the server-side resume point with position 0.
 assert.match(component,/void postWatching\(movie\.hash,0\)/)
})

test('resume only seeks after metadata loads and only when the position is usable',()=>{
 assert.match(component,/if\(!resumeSeekPending\|\|!video\|\|!target\)return/)
 assert.match(component,/if\(video\.readyState<1\)\{/)
 assert.match(component,/video\.addEventListener\('loadedmetadata',done\)/)
 assert.match(component,/if\(!Number\.isFinite\(position\)\|\|position<=0\)return/)
 assert.match(component,/if\(!Number\.isFinite\(video\.duration\)\|\|video\.duration<=0\)return/)
 assert.match(component,/video\.currentTime=position/)
 // Metadata now routes through a handler that also keeps subtitle selection in sync.
 assert.match(theater,/onloadedmetadata=\{handleMetadata\}/)
 assert.match(component,/const handleMetadata=\(\)=>\{\n  syncSubtitleTracks\(\)/)
})

test('resume reopens the theater only for a movie that still exists and is completed',()=>{
 assert.match(component,/const movie=movies\.find\(m=>m\.hash===resume\.hash\)/)
 assert.match(component,/if\(!movie\|\|movie\.status!=='completed'\)return/)
 assert.match(component,/if\(selected\|\|fileViewer\)return/)
 assert.match(component,/openMovie\(movie,target\)/)
 assert.match(component,/type ResumeState = \{hash:string,position:number/)
})

test('Settings lists active sessions with IP, browser, dates, status and movie position',()=>{
 assert.match(settings,/<Card\.Root class="settings-panel sessions-settings-panel"/)
 assert.match(settings,/<div class="settings-panel-icon"><MonitorSmartphone size=\{21\}\/><\/div>/)
 assert.match(settings,/settings-panel-intro/)
 assert.match(settings,/<h2>Active Sessions<\/h2>/)
 assert.match(settings,/\{#each sessions as session \(session\.id\)\}/)
 assert.match(settings,/\{browserLabel\(session\.userAgent\)\}/)
 assert.match(settings,/\{session\.ip\|\|'Unknown IP'\}/)
 assert.match(settings,/Signed in \{formatDate\(session\.createdAt\)\}/)
 assert.match(settings,/Active \{formatRelative\(session\.lastSeen\)\}/)
 assert.match(settings,/class="status-pill" class:watching=\{session\.status==='watching'\}/)
 assert.match(settings,/\{session\.watching\.title\}/)
 assert.match(settings,/\{formatPosition\(session\.watching\.position\)\}/)
 assert.match(settings,/\{#if session\.current\}<Badge variant="secondary" class="session-badge">This device<\/Badge>\{\/if\}/)
 // Non-current sessions get a revoke action, current does not.
 assert.match(settings,/\{#if !session\.current\}[\s\S]*?revokeSession\(session\.id\)[\s\S]*?\{\/if\}/)
 assert.match(settings,/class="session-revoke"/)
})

test('sessions refresh on open, poll every 10s only on Settings, and can be revoked',()=>{
 assert.match(component,/const loadSessions=async\(quiet=false\)/)
 assert.match(component,/await call\('sessions'\)/)
 assert.match(component,/const revokeSession=async\(id:string\)/)
 assert.match(component,/await call\('sessions\/'\+encodeURIComponent\(id\),'DELETE'\)/)
 assert.match(component,/const sessionsPoll=setInterval\(\(\)=>\{\n     if\(page==='settings'&&auth==='ready'\)void loadSessions\(true\)\n   \},10000\)/)
 assert.match(component,/clearInterval\(sessionsPoll\)/)
 // Opening Settings (navigation, history, or auth bootstrap) loads the list.
 assert.match(component,/if\(destination==='settings'\)void loadSessions\(\)/)
 assert.match(component,/if\(page==='settings'\)void loadSessions\(\)/)
})

test('sessions card has an explicit loading and empty state',()=>{
 assert.match(settings,/\{#if sessionsLoading&&!sessionsLoaded\}/)
 assert.match(settings,/\{:else if sessions\.length\}/)
 assert.match(settings,/\{:else\}[\s\S]*?class="sessions-empty"/)
 assert.match(settings,/No other sessions are signed in/)
 assert.match(css,/\.sessions-empty\s*\{[\s\S]*?border:\s*1px dashed var\(--border\)/)
})

test('session styles use design tokens only',()=>{
 assert.match(css,/\.session-item\s*\{[\s\S]*?border:\s*1px solid var\(--border\)/)
 assert.match(css,/\.session-item\.current\s*\{[\s\S]*?var\(--primary\)/)
 assert.match(css,/\.session-meta\s*\{[\s\S]*?color:\s*var\(--muted-foreground\)/)
 assert.match(css,/\.session-watching svg\s*\{\s*flex:\s*0 0 auto;\s*color:\s*var\(--primary\)/)
 const start=css.indexOf('/* ---------------------------------------------------------- active sessions */')
 const end=css.indexOf('/* --------------------------------------------------------------- theatre */',start)
 assert.ok(start>0 && end>start,'session styles block must exist')
 const block=css.slice(start,end)
 assert.ok(block.includes('.session-item'),'block must contain the session styles')
 assert.doesNotMatch(block,/#[0-9a-fA-F]{3,8}\b/)
})
