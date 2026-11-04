const subtitleLanguages={
 en:['en','English'],eng:['en','English'],english:['en','English'],
 da:['da','Danish'],dan:['da','Danish'],danish:['da','Danish'],
 fi:['fi','Finnish'],fin:['fi','Finnish'],finnish:['fi','Finnish'],
 no:['no','Norwegian'],nor:['no','Norwegian'],norwegian:['no','Norwegian'],
 sv:['sv','Swedish'],swe:['sv','Swedish'],swedish:['sv','Swedish'],
 fr:['fr','French'],fre:['fr','French'],fra:['fr','French'],french:['fr','French'],
 es:['es','Spanish'],spa:['es','Spanish'],spanish:['es','Spanish'],
 de:['de','German'],ger:['de','German'],deu:['de','German'],german:['de','German'],
 pt:['pt','Portuguese'],por:['pt','Portuguese'],portuguese:['pt','Portuguese'],
 it:['it','Italian'],ita:['it','Italian'],italian:['it','Italian'],
 nl:['nl','Dutch'],dut:['nl','Dutch'],nld:['nl','Dutch'],dutch:['nl','Dutch'],
 ja:['ja','Japanese'],jpn:['ja','Japanese'],japanese:['ja','Japanese'],
 ko:['ko','Korean'],kor:['ko','Korean'],korean:['ko','Korean'],
 zh:['zh','Chinese'],chi:['zh','Chinese'],zho:['zh','Chinese'],chinese:['zh','Chinese'],
 ar:['ar','Arabic'],ara:['ar','Arabic'],arabic:['ar','Arabic'],
 hi:['hi','Hindi'],hin:['hi','Hindi'],hindi:['hi','Hindi'],
} 
/**
 * @param {string} name A subtitle filename without its .srt extension.
 */
function detectSubtitleLanguage(name){
 const tokens=name.toLowerCase().split(/[.\s_-]+/)
 for(let i=tokens.length-1;i>=0;i--){
  const lang=subtitleLanguages[tokens[i]]
  if(lang)return {language:lang[0],label:lang[1]}
 }
 return null
}

/**
 * Select the largest MP4 as the feature film. Prefer subtitles matching its
 * basename; accept language-suffixed companions like film.en.srt.
 * @param {{path:string,size:number,completed?:number}[]} files
 */
export function findPlayback(files){
 const videos=files.filter(f=>/\.mp4$/i.test(f.path)&&f.size>0)
   .sort((a,b)=>b.size-a.size||a.path.localeCompare(b.path))
 const video=videos[0]??null
 if(!video)return {video:null,subtitles:[]}
 const base=video.path.replace(/\.mp4$/i,'')
 const videoFile=base.split('/').pop()||base
 const videoDir=base.slice(0,base.length-videoFile.length)
 const subtitles=files.filter(f=>/\.srt$/i.test(f.path)&&f.size>0)
   .map(f=>{
     const stem=f.path.replace(/\.srt$/i,'')
     const file=stem.split('/').pop()||stem
     const directory=stem.slice(0,stem.length-file.length)
     const exact=stem.toLowerCase()===base.toLowerCase()
     const langSuffix=directory.toLowerCase()===videoDir.toLowerCase()&&file.toLowerCase().startsWith(videoFile.toLowerCase()+'.')
     const loose=directory.toLowerCase()===videoDir.toLowerCase()
     const score=exact?4:langSuffix?3:loose?2:1
     // Recognize language codes (including ISO-639-2 aliases such as
     // dan/fin/nor/swe) and descriptive names in nested Subs/ folders.
     const recognized=detectSubtitleLanguage(file)
     const language=recognized?.language||'en'
     const label=recognized?.label||(exact?'Default':file.replace(/[._-]/g,' ').slice(0,48)||'Subtitles')
     return {...f,language,label,score}
   })
   .sort((a,b)=>b.score-a.score||a.path.localeCompare(b.path))
 return {video,subtitles}
}

/** @param {string} hash @param {string} path */
export function mediaURL(hash,path){
 return '/api/media/'+encodeURIComponent(hash)+'?path='+encodeURIComponent(path)
}
