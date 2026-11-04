/**
 * Copy to clipboard on both HTTPS and plain-HTTP VPS origins.
 * Clipboard.writeText requires a secure context, so use a temporary
 * offscreen textarea as a fallback for http://IP installations.
 *
 * @param {string} value
 * @returns {Promise<boolean>}
 */
export async function copyToClipboard(value){
 if(typeof value!=='string'||!value.startsWith('magnet:?'))return false
 if(typeof navigator!=='undefined' && navigator.clipboard?.writeText){
  try{await navigator.clipboard.writeText(value);return true}catch{/* HTTP fallback */}
 }
 if(typeof document==='undefined'||!document.body)return false
 const text=document.createElement('textarea')
 text.value=value
 text.readOnly=true
 text.setAttribute('aria-hidden','true')
 text.style.position='fixed'
 text.style.left='-9999px'
 text.style.top='0'
 text.style.opacity='0'
 const previous=document.activeElement
 document.body.appendChild(text)
 let copied=false
 try{
  text.focus()
  text.select()
  copied=Boolean(document.execCommand('copy'))
 }catch{/* Browsers without clipboard permission may deny copying. */}
 finally{
  text.remove()
  if(previous && typeof previous.focus==='function')previous.focus()
 }
 return copied
}
