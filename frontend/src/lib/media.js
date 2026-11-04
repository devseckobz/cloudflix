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
     let language='en'
     let label='Subtitles'
     if(langSuffix){
       const suffix=file.slice(videoFile.length+1)
       const codes=suffix.toLowerCase().split('.')
       const code=codes.find(c=>/^[a-z]{2,3}(-[a-z]{2})?$/.test(c))
       if(code){
         language=code
         label=code.toUpperCase()
       }
     }else if(exact){label='Default'}
     else if(file){label=file.replace(/[_-]/g,' ')}
     return {...f,language,label,score}
   })
   .sort((a,b)=>b.score-a.score||a.path.localeCompare(b.path))
 return {video,subtitles}
}

/** @param {string} hash @param {string} path */
export function mediaURL(hash,path){
 return '/api/media/'+encodeURIComponent(hash)+'?path='+encodeURIComponent(path)
}
