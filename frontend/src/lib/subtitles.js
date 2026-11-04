/**
 * Show exactly one text track, or disable them all.
 * Setting a track to "showing" asks the browser to fetch its WebVTT data.
 * @param {{textTracks: ArrayLike<{mode:string}>}|null|undefined} video
 * @param {number} index A zero-based track index, or -1 for CC off.
 */
export function applySubtitleSelection(video,index){
 if(!video?.textTracks)return false
 const tracks=video.textTracks
 const selected=Number.isInteger(index)&&index>=0&&index<tracks.length?index:-1
 for(let i=0;i<tracks.length;i++){
  tracks[i].mode=i===selected?'showing':'disabled'
 }
 return selected>=0
}
