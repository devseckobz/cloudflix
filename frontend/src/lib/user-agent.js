// Turns a raw User-Agent into the short label the Settings → Active Sessions
// list shows, e.g. "Chrome 131 · macOS" or "Safari 18.1 · iPadOS 17.5".
//
// Detection order matters: every Chromium-family browser also claims "Chrome/",
// and Chrome also claims "Safari/". Edge, Opera, Samsung and the other Chromium
// forks are therefore matched before Chrome, and Chrome before Safari, so the
// most specific name always wins.

/**
 * @typedef {{browser:string,version:string,platform:string,device:string,label:string,initial:string}} SessionClient
 */

/** @param {string} value @param {RegExp} pattern */
function versionAfter(value,pattern){
 const found=value.match(pattern)
 return found?found[1].split('.')[0]:''
}

/** @param {string} value */
function detectBrowser(value){
 if(/curl\//i.test(value))return {browser:'curl',version:versionAfter(value,/curl\/(\d+(?:\.\d+)*)/i)}
 if(/PostmanRuntime/i.test(value))return {browser:'Postman',version:''}
 if(/HeadlessChrome\//i.test(value))return {browser:'Headless Chrome',version:versionAfter(value,/HeadlessChrome\/(\d+(?:\.\d+)*)/i)}
 if(/EdgA?\//.test(value))return {browser:'Edge',version:versionAfter(value,/EdgA?\/(\d+(?:\.\d+)*)/)}
 if(/OPR\//.test(value)||/\bOpera\b/.test(value))return {browser:'Opera',version:versionAfter(value,/OPR\/(\d+(?:\.\d+)*)/)}
 if(/SamsungBrowser\//.test(value))return {browser:'Samsung Internet',version:versionAfter(value,/SamsungBrowser\/(\d+(?:\.\d+)*)/)}
 if(/Vivaldi\//.test(value))return {browser:'Vivaldi',version:versionAfter(value,/Vivaldi\/(\d+(?:\.\d+)*)/)}
 if(/Firefox\/|FxiOS\//.test(value))return {browser:'Firefox',version:versionAfter(value,/(?:Firefox|FxiOS)\/(\d+(?:\.\d+)*)/)}
 if(/CriOS\//.test(value))return {browser:'Chrome',version:versionAfter(value,/CriOS\/(\d+(?:\.\d+)*)/)}
 if(/Chrome\//.test(value))return {browser:'Chrome',version:versionAfter(value,/Chrome\/(\d+(?:\.\d+)*)/)}
 if(/Safari\//.test(value))return {browser:'Safari',version:versionAfter(value,/Version\/(\d+(?:\.\d+)*)/)}
 return {browser:'Unknown browser',version:''}
}

/**
 * Platform, its version, and whatever genuinely separates look-alike devices:
 * iPad from iPhone, an Android WebView from real Chrome, and the Android model.
 * @param {string} value
 */
function detectPlatform(value){
 // Android before Linux: Android user agents also contain "Linux".
 if(/Android/.test(value)){
  const version=(value.match(/Android\s+([\d.]+)/)||[])[1]||''
  // "; wv)" marks an embedded WebView, which is a different client from Chrome
  // even though it reports the Chrome version.
  const webview=/;\s*wv\)/.test(value)
  const model=(value.match(/Android[^;)]*;\s*([^;)]+?)(?:\s+Build|\))/)||[])[1]||''
  const device=[webview?'WebView':'',model.replace(/Build\/.*/,'').trim()].filter(Boolean).join(' · ')
  return {platform:version?'Android '+version:'Android',device}
 }
 if(/iPad/.test(value)){
  const version=(value.match(/OS\s+(\d+(?:_\d+)*)/)||[])[1]
  return {platform:version?'iPadOS '+version.replace(/_/g,'.'):'iPadOS',device:''}
 }
 if(/iPhone/.test(value)){
  const version=(value.match(/OS\s+(\d+(?:_\d+)*)/)||[])[1]
  return {platform:version?'iOS '+version.replace(/_/g,'.'):'iOS',device:''}
 }
 if(/iPod/.test(value)){
  const version=(value.match(/OS\s+(\d+(?:_\d+)*)/)||[])[1]
  return {platform:version?'iOS '+version.replace(/_/g,'.'):'iOS',device:''}
 }
 if(/Windows/.test(value)){
  // Windows 10 and 11 both report "Windows NT 10.0" and there is no reliable UA
  // signal for the difference, so the label stays honest instead of guessing.
  return {platform:'Windows',device:''}
 }
 if(/Mac OS X|Macintosh/.test(value)){
  const version=(value.match(/Mac OS X\s+(\d+(?:_\d+)*)/)||[])[1]
  return {platform:'macOS',device:version?'macOS '+version.replace(/_/g,'.'):''}
 }
 if(/CrOS/.test(value))return {platform:'ChromeOS',device:''}
 if(/PlayStation/.test(value))return {platform:'PlayStation',device:''}
 if(/Linux|X11/.test(value))return {platform:'Linux',device:''}
 return {platform:'',device:''}
}

/**
 * @param {string} userAgent Raw User-Agent header, possibly empty.
 * @returns {SessionClient}
 */
export function describeSession(userAgent){
 const value=String(userAgent||'')
 const {browser,version}=detectBrowser(value)
 const {platform,device}=detectPlatform(value)
 const name=version?browser+' '+version:browser
 return {
  browser,
  version,
  platform,
  device,
  label:platform?name+' · '+platform:name,
  initial:(browser||'?').slice(0,1).toUpperCase(),
 }
}

/** Short label only: "Chrome 131 · macOS". */
export function browserLabel(userAgent){
 return describeSession(userAgent).label
}

/**
 * The extra detail that the label does not already carry: the Android model,
 * "WebView", or the macOS version. Empty when there is nothing to add, so the
 * row can omit the separator instead of rendering an empty span.
 * @param {string} userAgent
 */
export function clientDetail(userAgent){
 return describeSession(userAgent).device
}
