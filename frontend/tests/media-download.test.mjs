import {test} from 'node:test'
import assert from 'node:assert/strict'
import {downloadURL,saveMedia,mediaURL} from '../src/lib/media.js'

test('download URL targets the attachment endpoint, not the inline stream',()=>{
 const url=downloadURL('a'.repeat(40),'Film (2026)/Film & Escape.mkv')
 assert.ok(url.startsWith('/api/files/'+'a'.repeat(40)+'?path='))
 assert.equal(new URL(url,'http://localhost').searchParams.get('path'),'Film (2026)/Film & Escape.mkv')
 // The player endpoint stays separate: it must keep serving inline video.
 assert.ok(mediaURL('a'.repeat(40),'x.mp4').startsWith('/api/media/'))
 assert.ok(!url.includes('/api/media/'))
})

test('saveMedia clicks a temporary anchor and removes it again',()=>{
 const created=[]
 const body={
  appendChild(node){created.push(node)},
 }
 // Minimal DOM stand-ins: saveMedia only needs createElement plus a clickable node.
 const original={document:globalThis.document}
 const anchor={href:'',rel:'',download:'',clicked:0,click(){this.clicked++},remove(){this.removed=true}}
 globalThis.document={createElement:(tag)=>{
  assert.equal(tag,'a')
  return anchor
 },body}
 try{
  saveMedia('b'.repeat(40),'Film/Movie.mkv','Movie.mkv')
  assert.equal(anchor.href,'/api/files/'+'b'.repeat(40)+'?path=Film%2FMovie.mkv')
  assert.equal(anchor.download,'Movie.mkv')
  assert.equal(anchor.rel,'noopener')
  assert.equal(anchor.clicked,1)
  assert.equal(anchor.removed,true)
  assert.equal(created.length,1)
 }finally{
  globalThis.document=original.document
 }
})
