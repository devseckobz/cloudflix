import { test } from 'node:test'
import assert from 'node:assert/strict'
import { applySubtitleSelection } from '../src/lib/subtitles.js'

const mockVideo=(count)=>({textTracks:Array.from({length:count},()=>({mode:'disabled'}))})

test('CC starts off and disables all text tracks',()=>{
 const video=mockVideo(4)
 video.textTracks[0].mode='showing'
 video.textTracks[1].mode='hidden'
 assert.equal(applySubtitleSelection(video,-1),false)
 assert.deepEqual(video.textTracks.map(t=>t.mode),['disabled','disabled','disabled','disabled'])
})

test('enabling CC shows exactly one subtitle language',()=>{
 const video=mockVideo(5)
 assert.equal(applySubtitleSelection(video,0),true)
 assert.deepEqual(video.textTracks.map(t=>t.mode),['showing','disabled','disabled','disabled','disabled'])
 assert.equal(applySubtitleSelection(video,3),true)
 assert.deepEqual(video.textTracks.map(t=>t.mode),['disabled','disabled','disabled','showing','disabled'])
 assert.equal(applySubtitleSelection(video,-1),false)
 assert.ok(video.textTracks.every(t=>t.mode==='disabled'))
})

test('invalid selections do not accidentally enable subtitles',()=>{
 const video=mockVideo(2)
 for(const index of [-3,2,99,NaN,undefined,'0']){
  assert.equal(applySubtitleSelection(video,index),false)
  assert.ok(video.textTracks.every(t=>t.mode==='disabled'))
 }
 assert.equal(applySubtitleSelection(null,0),false)
 assert.equal(applySubtitleSelection(undefined,0),false)
})
