import {test} from 'node:test'
import assert from 'node:assert/strict'
import {findPlayback,mediaURL} from '../src/lib/media.js'

test('player chooses largest MP4 and detects matching and language subtitles',()=>{
 const result=findPlayback([
  {path:'Feature/sample.mp4',size:100},
  {path:'Feature/Feature.2026.mp4',size:20_000},
  {path:'Feature/Feature.2026.en.srt',size:220},
  {path:'Feature/Feature.2026.srt',size:120},
  {path:'Feature/other.srt',size:110},
  {path:'Extras/Feature.2026.fr.srt',size:200},
  {path:'Feature/Feature.2026.mkv',size:300_000},
 ])
 assert.equal(result.video?.path,'Feature/Feature.2026.mp4')
 assert.deepEqual(result.subtitles.slice(0,2).map(s=>s.path),['Feature/Feature.2026.srt','Feature/Feature.2026.en.srt'])
 assert.equal(result.subtitles.find(s=>s.path.includes('.en.srt'))?.language,'en')
})

test('MKV is chosen when no MP4 exists, with external SRT subtitles',()=>{
 const playback=findPlayback([{path:'movie.mkv',size:1500},{path:'movie.srt',size:400}])
 assert.equal(playback.video?.path,'movie.mkv')
 assert.equal(playback.subtitles[0]?.path,'movie.srt')
})
test('unsupported file types cannot be used as movie video',()=>{
 const playback=findPlayback([{path:'movie.avi',size:1500},{path:'movie.srt',size:400}])
 assert.equal(playback.video,null)
 assert.equal(playback.subtitles.length,0)
})

test('URLs encode hash and untrusted file names as query data',()=>{
 const url=mediaURL('a'.repeat(40),'Film (2026)/Film & Escape.mp4')
 assert.ok(url.startsWith('/api/media/'+ 'a'.repeat(40)+'?path='))
 assert.equal(new URL(url,'http://localhost').searchParams.get('path'),'Film (2026)/Film & Escape.mp4')
})

test('language shortcuts recognize nested subtitle names and ISO-639 aliases',()=>{
 const filenames=['Movie.mp4','Movie.srt','Subs/English.srt','Subs/dan.srt','Subs/fin.srt','Subs/nor.srt','Subs/swe.srt']
 const playback=findPlayback(filenames.map((path,i)=>({path,size:i===0?1000000:100})))
 const labels=new Map(playback.subtitles.map(sub=>[sub.path,[sub.language,sub.label]]))
 assert.deepEqual(labels.get('Subs/English.srt'),['en','English'])
 assert.deepEqual(labels.get('Subs/dan.srt'),['da','Danish'])
 assert.deepEqual(labels.get('Subs/fin.srt'),['fi','Finnish'])
 assert.deepEqual(labels.get('Subs/nor.srt'),['no','Norwegian'])
 assert.deepEqual(labels.get('Subs/swe.srt'),['sv','Swedish'])
 assert.deepEqual(labels.get('Movie.srt'),['en','Default'])
})
