import { test } from 'node:test'
import assert from 'node:assert/strict'
import { selectLibraryPage,countDownloading } from '../src/lib/library.js'

const movies=Array.from({length:28},(_,i)=>({
 hash:String(i).padStart(2,'0'),
 title:'Movie '+String(i+1).padStart(2,'0'),
 year:String(2000+i),
 status:i<25?'completed':i===25?'downloading':i===26?'metadata':'paused'
}))

test('home shows only completed movies, with maximum 10 per page',()=>{
 const first=selectLibraryPage(movies,'',1)
 const second=selectLibraryPage(movies,'',2)
 const third=selectLibraryPage(movies,'',3)
 assert.equal(first.total,25)
 assert.equal(first.totalPages,3)
 assert.equal(first.movies.length,10)
 assert.equal(second.movies.length,10)
 assert.equal(third.movies.length,5)
 assert.ok(first.movies.every(m=>m.status==='completed'))
 assert.equal(new Set([...first.movies,...second.movies,...third.movies].map(m=>m.hash)).size,25)
})

test('search matches completed movie title and year and is case-insensitive',()=>{
 const title=selectLibraryPage(movies,'mOvIe 08',1)
 assert.equal(title.total,1)
 assert.equal(title.movies[0].title,'Movie 08')
 const year=selectLibraryPage(movies,'2009',1)
 assert.equal(year.total,1)
 assert.equal(year.movies[0].title,'Movie 10')
 const missing=selectLibraryPage(movies,'Movie 28',1)
 assert.equal(missing.total,0)
 assert.equal(missing.movies.length,0)
})

test('pagination clamps stale page number after search or download removal',()=>{
 assert.equal(selectLibraryPage(movies,'',999).page,3)
 assert.equal(selectLibraryPage(movies,'Movie 01',999).page,1)
 assert.equal(selectLibraryPage([], '', 1).page,1)
 assert.deepEqual(selectLibraryPage(movies,'',3).pages,[1,2,3])
})

test('downloader badge counts only active torrent transfers',()=>{
 assert.equal(countDownloading(movies),1)
 assert.equal(countDownloading(movies.filter(m=>m.status!=='downloading')),0)
})
