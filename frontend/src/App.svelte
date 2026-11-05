<script lang="ts">
 import { onMount } from 'svelte'
 import { Button } from '$lib/components/ui/button/index.js'
 import * as Card from '$lib/components/ui/card/index.js'
 import { Badge } from '$lib/components/ui/badge/index.js'
 import { Input } from '$lib/components/ui/input/index.js'
 import { Textarea } from '$lib/components/ui/textarea/index.js'
 import { Progress } from '$lib/components/ui/progress/index.js'
 import { Label } from '$lib/components/ui/label/index.js'
 import { Spinner } from '$lib/components/ui/spinner/index.js'
 import * as Tooltip from '$lib/components/ui/tooltip/index.js'
 import * as Empty from '$lib/components/ui/empty/index.js'
 import * as Dialog from '$lib/components/ui/dialog/index.js'
 import * as Alert from '$lib/components/ui/alert/index.js'
 import * as InputOTP from '$lib/components/ui/input-otp/index.js'
 import * as Pagination from '$lib/components/ui/pagination/index.js'
 import * as ScrollArea from '$lib/components/ui/scroll-area/index.js'
 import * as Avatar from '$lib/components/ui/avatar/index.js'
 import { Skeleton } from '$lib/components/ui/skeleton/index.js'
 import { Toggle } from '$lib/components/ui/toggle/index.js'
 import { confirmDialog } from '$lib/components/ui/alert-dialog/index.js'
 import { Toaster, toast } from '$lib/components/ui/sonner/index.js'
 import { selectLibraryPage, countDownloading } from './lib/library.js'
 import {findPlayback,mediaURL} from './lib/media.js'
 import {createVersionReloadMonitor} from './lib/version-reload.js'
 import {applySubtitleSelection} from './lib/subtitles.js'
 import {copyToClipboard} from './lib/clipboard.js'
 import {browserLabel,clientDetail,describeSession} from './lib/user-agent.js'
 import { Film, House, Download, Search, Pause, Play, Trash2, HardDrive, HardDriveDownload, Database, Settings, LogOut, Plus, X, CheckCircle2, Shield, Clapperboard, AlertCircle, Star, ChevronLeft, ChevronRight, Maximize2, MonitorPlay, MonitorSmartphone, Users, RefreshCw, ArrowUpCircle, Files, Copy } from '@lucide/svelte'

 type Movie = {hash:string,title:string,year:string,genre:string,poster:string,backdrop:string,rating:number,size:number,completed:number,progress:number,status:string,error?:string,files:{path:string,size:number,completed?:number}[]}
 type InspectedFile = {path:string,size:number,selected:boolean}
 type FileInspection = {available:boolean,fetching?:boolean,files:InspectedFile[],total:number,truncated?:boolean,rejected:boolean,reason:string,message?:string}
 type MKVStatus = {state:string,message?:string,videoURL?:string,subtitles?:{index:number,language:string,label:string,url:string}[]}
 type ResumeState = {hash:string,position:number,title?:string,updatedAt?:number}
 type SessionWatching = {hash:string,title:string,position:number}
 type SessionInfo = {id:string,current:boolean,ip:string,userAgent:string,createdAt:number,lastSeen:number,status:'watching'|'idle'|string,watching:SessionWatching|null}
 let page = $state<'home'|'downloader'|'settings'>(typeof window!=='undefined'&&window.location.pathname==='/settings'?'settings':typeof window!=='undefined'&&window.location.pathname==='/downloader'?'downloader':'home')
 let auth = $state<'loading'|'setup'|'login'|'ready'>('loading')
 let pin = $state('')
 let movies = $state<Movie[]>([])
 type Storage = {known:boolean,total:number,free:number,reserved:number,available:number,headroom:number,error?:string}
 let storage = $state<Storage|null>(null)
 const storageUsed = $derived(storage?.known&&storage.total>0?Math.min(100,100*(storage.total-storage.free)/storage.total):0)
 let error = $state('')
 let magnet = $state('')
 let magnetDialog = $state<{title:string,magnet:string}|null>(null)
 let stateLoaded = $state(false)
 let submitting = $state(false)
 let search = $state('')
 let libraryPage = $state(1)
 let queueFilter = $state<'all'|'active'|'completed'>('all')
 let selected = $state<Movie|null>(null)
 let fileViewer = $state<{hash:string,title:string,status:string}|null>(null)
 let inspectedFiles = $state<FileInspection|null>(null)
 let fileViewerLoading = $state(false)
 let fileViewerError = $state('')
 let fileInspectionPolling=false
 let playerVideo = $state<HTMLVideoElement|null>(null)
 let playbackError = $state('')
 let mkvStatus = $state<MKVStatus|null>(null)
 let mkvPollActive=false
 let ccEnabled = $state(false)
 let selectedSubtitle = $state(0)
 let connected = $state(false)
 let version = $state('v0.1.0-alpha')
 let live:EventSource|null = null
 let tmdbConfigured = $state(false)
 let tmdbToken = $state('')
 let settingsBusy = $state(false)
 let settingsLoading = $state(false)
 let tmdbChecked = $state(false)
 type UpdateInfo = {currentVersion:string,latestVersion:string,updateAvailable:boolean,updaterReady:boolean,state:string,message:string,updatedAt:number,checkError?:string}
 let updateInfo = $state<UpdateInfo|null>(null)
 let updateChecking = $state(false)
 let updateBusy = $state(false)
 let updateError = $state('')
 // Per-browser resume: the server remembers where this session stopped watching.
 let resumeTarget = $state<ResumeState|null>(null)
 let resumeSeekPending = $state(false)
 let watchingTimer:ReturnType<typeof setInterval>|undefined
 let closingTheater = false
 // Settings → Active Sessions.
 let sessions = $state<SessionInfo[]>([])
 let sessionsLoading = $state(false)
 let sessionsError = $state('')
 let sessionsLoaded = $state(false)
 let revokingSession = $state('')
 // Ignore the initial server version; reload only when the running binary changes.
 const versionMonitor=createVersionReloadMonitor(()=>window.location.reload())
 const navigate=(destination:'home'|'downloader'|'settings')=>{
   if(destination==='settings'||destination==='downloader')void loadSettings()
   if(destination==='settings')void loadSessions()
   page=destination
   const path=destination==='home'?'/':destination==='settings'?'/settings':'/downloader'
   if(window.location.pathname!==path)window.history.pushState({},'',path)
 }
 const formatBytes=(v:number):string=>{
   if (!v) return '0 B'
   const units=['B','KB','GB','GB','TB']
   const rank=Math.min(4,Math.floor(Math.log(v)/Math.log(1024)))
   return (v/1024**rank).toFixed(rank?1:0)+' '+(rank===2?'MB':units[rank])
 }
 const percent=(m:Movie)=>Number.isFinite(m.progress)?Math.min(100,Math.max(0,m.progress)):0
 const formatPosition=(seconds:number):string=>{
  if(!Number.isFinite(seconds)||seconds<=0)return '0:00'
  const total=Math.floor(seconds)
  const hours=Math.floor(total/3600)
  const minutes=Math.floor((total%3600)/60)
  const secs=total%60
  return hours?hours+':'+String(minutes).padStart(2,'0')+':'+String(secs).padStart(2,'0'):minutes+':'+String(secs).padStart(2,'0')
 }
 const formatDate=(unix:number):string=>{
  if(!unix)return 'unknown'
  try{return new Date(unix*1000).toLocaleDateString(undefined,{year:'numeric',month:'short',day:'numeric'})}catch{return 'unknown'}
 }
 const formatRelative=(unix:number):string=>{
  if(!unix)return 'unknown'
  const diff=Math.max(0,Math.floor(Date.now()/1000)-unix)
  if(diff<60)return 'just now'
  if(diff<3600)return Math.floor(diff/60)+'m ago'
  if(diff<86400)return Math.floor(diff/3600)+'h ago'
  if(diff<604800)return Math.floor(diff/86400)+'d ago'
  return formatDate(unix)
 }
 const poster=(path:string)=>path?'https://image.tmdb.org/t/p/w500'+path:''
 const backdrop=(path:string)=>path?'https://image.tmdb.org/t/p/w1280'+path:''
 const completed = $derived(movies.filter(m=>m.status==='completed').length)
 const active = $derived(countDownloading(movies))
 const libraryView = $derived(selectLibraryPage(movies,search,libraryPage,10))
 const queueItems = $derived(movies.filter(m=>queueFilter==='all'||(queueFilter==='active'&&m.status!=='completed'&&m.status!=='rejected')||(queueFilter==='completed'&&m.status==='completed')))
 const selectedPlayback = $derived(selected?findPlayback(selected.files):{video:null,subtitles:[]})
 const selectedIsMKV = $derived(Boolean(selectedPlayback.video?.path.toLowerCase().endsWith('.mkv')))
 const playbackVideoURL = $derived(selectedIsMKV?mkvStatus?.videoURL:null)
 const playbackTracks = $derived([
   ...selectedPlayback.subtitles.map(s=>({path:s.path,label:s.label,language:s.language,src:selected?mediaURL(selected.hash,s.path):''})),
   ...(selectedIsMKV&&mkvStatus?.state==='ready'
      ?(mkvStatus.subtitles||[]).map(t=>({path:'embedded:'+t.index,label:t.label,language:t.language,src:t.url}))
      :[])
 ])
 const statusLabel=(status:string)=>status==='metadata'?'Checking files':status==='completed'?'Completed':status==='downloading'?'Downloading':status==='paused'?'Paused':status==='rejected'?'Rejected':status
 const call = async (path:string,method='GET',data?:object)=>{
   let response:Response
   try{
     response=await fetch('/api/'+path,{method,credentials:'same-origin',headers:data?{'Content-Type':'application/json'}:{},body:data?JSON.stringify(data):undefined})
   }catch{
     throw new Error('Cloudflix is unreachable. Check that the server is running and reload.')
   }
   const result=await response.json().catch(()=>({error:''}))
   if(!response.ok){
     if(response.status===429)throw new Error(result.error||'Too many attempts. Try again in 15 minutes.')
     if(response.status===403)throw new Error('Login request was rejected by the server. Reload this page and try again.')
     if(response.status===401 && path==='auth/login')throw new Error('Incorrect PIN. Check the six digits and try again.')
     throw new Error(result.error||'Server error (HTTP '+response.status+').')
   }
   return result
 }
 // Sonner owns the toast lifecycle (auto-dismiss, stacking, a11y live region).
 const notify=(message:string)=>toast.success(message)
 const fail=(message:string)=>toast.error(message)
 const syncSubtitleTracks=()=>{
   applySubtitleSelection(playerVideo,ccEnabled?selectedSubtitle:-1)
 }
 const toggleCaptions=()=>{
   if(!playbackTracks.length)return
   ccEnabled=!ccEnabled
   syncSubtitleTracks()
 }
 const selectCaptionTrack=(index:number)=>{
   if(!Number.isInteger(index)||index<0||index>=playbackTracks.length)return
   selectedSubtitle=index
   ccEnabled=true
   syncSubtitleTracks()
 }
 const loadMKVStatus=async(hash:string,path:string)=>{
   if(mkvPollActive)return
   mkvPollActive=true
   try{
     const data=await call('playback/'+encodeURIComponent(hash)+'/status?path='+encodeURIComponent(path))
     if(selected?.hash===hash && findPlayback(selected.files).video?.path===path)mkvStatus=data as MKVStatus
   }catch(e){
     if(selected?.hash===hash)mkvStatus={state:'error',message:(e as Error).message}
   }finally{mkvPollActive=false}
 }
 // ---------------------------------------------------------------- resume state
 const postWatching=async(hash:string,position:number)=>{
  try{await call('session/watching','POST',{hash,position:Math.max(0,Number(position)||0)})}catch{}
 }
 const saveProgressNow=()=>{
  const video=playerVideo
  const movie=selected
  if(closingTheater||!video||!movie||movie.status!=='completed')return
  if(video.readyState<1||!Number.isFinite(video.currentTime)||video.currentTime<=0)return
  void postWatching(movie.hash,video.currentTime)
 }
 const startWatchingTimer=()=>{
  stopWatchingTimer()
  watchingTimer=setInterval(saveProgressNow,5000)
 }
 const stopWatchingTimer=()=>{
  if(watchingTimer!==undefined){clearInterval(watchingTimer);watchingTimer=undefined}
 }
 // Only ever seek once the element has metadata, and never to 0 or past the end.
 const applyResumeSeek=async()=>{
  const video=playerVideo
  const target=resumeTarget
  if(!resumeSeekPending||!video||!target)return
  if(!selected||selected.hash!==target.hash||selected.status!=='completed'){resumeSeekPending=false;resumeTarget=null;return}
  const position=Number(target.position)
  resumeSeekPending=false
  resumeTarget=null
  if(!Number.isFinite(position)||position<=0)return
  try{
   if(video.readyState<1){
    await new Promise<void>((resolve)=>{
     const done=()=>{video.removeEventListener('loadedmetadata',done);resolve()}
     video.addEventListener('loadedmetadata',done)
     setTimeout(done,4000)
    })
   }
   if(!Number.isFinite(video.duration)||video.duration<=0)return
   if(position>=video.duration-1)return
   video.currentTime=position
   notify('Resumed from '+formatPosition(position))
  }catch{}
 }
 const handleMetadata=()=>{
  syncSubtitleTracks()
  void applyResumeSeek()
 }
 const handlePause=()=>{if(!closingTheater)saveProgressNow()}
 const loadResume=async()=>{
  try{
   const data=await call('session')
   const resume=data?.resume
   if(!resume||typeof resume.hash!=='string'||!resume.hash)return
   if(selected||fileViewer)return
   const movie=movies.find(m=>m.hash===resume.hash)
   if(!movie||movie.status!=='completed')return
   const position=Number(resume.position)
   if(!Number.isFinite(position)||position<=0)return
   const target:ResumeState={hash:resume.hash,position,title:resume.title,updatedAt:resume.updatedAt}
   openMovie(movie,target)
  }catch{}
 }
 const loadSessions=async(quiet=false)=>{
  if(!quiet)sessionsLoading=true
  try{
   const data=await call('sessions')
   sessions=Array.isArray(data?.sessions)?(data.sessions as SessionInfo[]):[]
   sessionsLoaded=true
   sessionsError=''
  }catch(e){
   sessionsError=(e as Error).message
  }finally{
   if(!quiet)sessionsLoading=false
  }
 }
 const revokeSession=async(id:string)=>{
  if(!id||revokingSession)return
  revokingSession=id
  sessionsError=''
  try{
   await call('sessions/'+encodeURIComponent(id),'DELETE')
   notify('That device has been signed out.')
   await loadSessions(true)
  }catch(e){
   sessionsError=(e as Error).message
  }finally{
   revokingSession=''
  }
 }
 const prepareMKV=async(movie:Movie)=>{
   const file=findPlayback(movie.files).video
   if(!file||!file.path.toLowerCase().endsWith('.mkv'))return
   mkvStatus={state:'queued',message:'Preparing MKV playback…'}
   try{
     const data=await call('playback/'+encodeURIComponent(movie.hash)+'/prepare','POST',{path:file.path})
     if(selected?.hash===movie.hash)mkvStatus=data as MKVStatus
   }catch(e){
     if(selected?.hash===movie.hash)mkvStatus={state:'error',message:(e as Error).message}
   }
 }
 const openMovie=(movie:Movie,resume?:ResumeState|null)=>{
   playbackError=''
   playerVideo=null
   selectedSubtitle=0
   ccEnabled=false
   mkvStatus=null
   closingTheater=false
   resumeTarget=resume??null
   resumeSeekPending=Boolean(resume)
   selected=movie
   if(movie.status==='completed'){
     startWatchingTimer()
     if(findPlayback(movie.files).video?.path.toLowerCase().endsWith('.mkv')){
       void prepareMKV(movie)
     }
   }
 }
 const closeMovie=()=>{
   const video=playerVideo
   const movie=selected
   stopWatchingTimer()
   // Clear the server-side resume point for this browser. The pause event that
   // follows must not re-post the old position, so suppress it for this tick.
   closingTheater=true
   if(movie&&movie.status==='completed'){
     void postWatching(movie.hash,0)
   }
   video?.pause()
   playerVideo=null
   selected=null
   mkvStatus=null
   selectedSubtitle=0
   ccEnabled=false
   playbackError=''
   resumeTarget=null
   resumeSeekPending=false
 }
 const closeFileViewer=()=>{
   fileViewer=null
   inspectedFiles=null
   fileViewerLoading=false
   fileViewerError=''
 }
 const loadFileList=async(hash:string,retry=false,quiet=false)=>{
   if(quiet && fileInspectionPolling)return
   if(quiet)fileInspectionPolling=true
   if(!quiet)fileViewerLoading=true
   fileViewerError=''
   try{
     const result=retry
       ?await call('torrents/'+encodeURIComponent(hash)+'/files?retry=1')
       :await call('torrents/'+encodeURIComponent(hash)+'/files')
     if(fileViewer?.hash===hash)inspectedFiles=result as FileInspection
   }catch(e){
     if(fileViewer?.hash===hash)fileViewerError=(e as Error).message
   }finally{
     if(quiet)fileInspectionPolling=false
     if(fileViewer?.hash===hash)fileViewerLoading=false
   }
 }
 const showFiles=async(movie:Pick<Movie,'hash'|'title'|'status'>)=>{
   // Rejected torrents may need their metadata fetched again after a restart.
   fileViewer={hash:movie.hash,title:movie.title,status:movie.status}
   inspectedFiles=null
   fileViewerError=''
   await loadFileList(movie.hash)
 }
 const retryFileInspection=async()=>{
   if(!fileViewer)return
   inspectedFiles=null
   await loadFileList(fileViewer.hash,true)
 }
 const enterFullscreen=async()=>{
   const video=playerVideo
   if(!video)return
   try{
     if(video.requestFullscreen)await video.requestFullscreen()
     else (video as HTMLVideoElement & {webkitEnterFullscreen?:()=>void}).webkitEnterFullscreen?.()
   }catch{playbackError='Fullscreen is unavailable in this browser. You can also use the video controls.'}
 }
 const loadUpdateInfo=async(quiet=false)=>{
   if(!quiet)updateChecking=true
   try{
     const data=await call('updates')
     // Settings polling detects a newly installed version even if SSE is offline.
     if(versionMonitor.observe(data.currentVersion))return
     updateInfo=data as UpdateInfo
     updateError=''
   }catch(e){
     if(!quiet)updateError=(e as Error).message
   }finally{
     if(!quiet)updateChecking=false
   }
 }
 const startUpdate=async()=>{
   if(!updateInfo?.updateAvailable||!updateInfo.updaterReady||updateBusy)return
   const ok=await confirmDialog({
     title:'Install Cloudflix '+updateInfo.latestVersion+'?',
     description:'Your server will restart briefly. Your movies, PIN and settings are preserved.',
     confirmLabel:'Install update',
   })
   if(ok!==true)return
   updateBusy=true
   updateError=''
   try{
     await call('updates','POST',{})
     notify('Update requested. Cloudflix will restart automatically.')
     await loadUpdateInfo(true)
   }catch(e){updateError=(e as Error).message}
   finally{updateBusy=false}
 }
 const loadSettings=async()=>{
   settingsLoading=true
   try{
     const result=await call('settings')
     tmdbConfigured=Boolean(result.tmdbConfigured)
     tmdbChecked=true
   }catch(e){fail((e as Error).message)}
   finally{
     settingsLoading=false
     if(page==='settings')void loadUpdateInfo(true)
   }
 }
 const saveTMDB=async()=>{
   if(!tmdbToken.trim()){fail('Paste a TMDB API key or Read Access Token first');return}
   settingsBusy=true
   try{
     const result=await call('settings','PUT',{tmdbApiKey:tmdbToken.trim()})
     tmdbConfigured=Boolean(result.tmdbConfigured)
     tmdbToken=''
     notify('TMDB API token verified and saved. Movie artwork will refresh automatically.')
   }catch(e){fail((e as Error).message)}
   finally{settingsBusy=false}
 }
 const clearTMDB=async()=>{
   const ok=await confirmDialog({
     title:'Remove the TMDB API token?',
     description:'Existing artwork will remain, but new movie metadata will not be fetched.',
     confirmLabel:'Remove token',
     destructive:true,
   })
   if(ok!==true)return
   settingsBusy=true
   try{
     await call('settings','PUT',{tmdbApiKey:''})
     tmdbConfigured=false
     tmdbToken=''
     notify('TMDB API token removed.')
   }catch(e){fail((e as Error).message)}
   finally{settingsBusy=false}
 }
 // The SSE snapshot arrives every 2s as freshly parsed JSON. Replacing the
 // array unconditionally would invalidate every {#each} block and repaint the
 // whole library while a movie is playing, so compare a cheap signature first.
 const movieSignature=(list:Movie[])=>list.map(m=>m.hash+'|'+m.status+'|'+m.progress+'|'+m.completed+'|'+m.title+'|'+m.year+'|'+m.genre+'|'+m.rating+'|'+m.size+'|'+(m.error||'')).join('\n')
 const setState=(state:{movies:Movie[],version?:string,storage?:Storage})=>{
   // SSE also covers terminal-based updates and all pages, not just Settings.
   if(state.version && versionMonitor.observe(state.version))return
   const nextMovies=state.movies||[]
   if(movieSignature(nextMovies)!==movieSignature(movies))movies=nextMovies
   const nextStorage=state.storage||null
   if(JSON.stringify(nextStorage)!==JSON.stringify(storage))storage=nextStorage
   if(state.version)version=state.version
   stateLoaded=true
   if(selected){
     const current=movies.find(m=>m.hash===selected?.hash)||null
     if(current!==selected)selected=current
     // A movie removed mid-playback unmounts the theater; stop tracking it.
     if(!selected)stopWatchingTimer()
   }
 }
 const connect=()=>{
   live?.close()
   live=new EventSource('/api/events')
   live.onopen=()=>connected=true
   live.onmessage=(event)=>{try{setState(JSON.parse(event.data))}catch{}}
   live.onerror=()=>connected=false
 }
 // A failed first load must not leave the skeleton shimmering forever.
 const refresh=async()=>{try{setState(await call('state'))}catch(e){fail(String(e))}finally{stateLoaded=true}}
 const checkAuth=async()=>{
   try{
     const state=await call('auth/status')
     auth=state.setupRequired?'setup':state.authenticated?'ready':'login'
     if(auth==='ready'){await refresh();connect();if(page==='settings'||page==='downloader')await loadSettings();if(page==='settings')void loadSessions();await loadResume()}
   }catch{error='Unable to connect to Cloudflix server';auth='login'}
 }
 onMount(()=>{
   const handleBack=()=>{page=window.location.pathname==='/settings'?'settings':window.location.pathname==='/downloader'?'downloader':'home';if(page==='settings'||page==='downloader')void loadSettings();if(page==='settings')void loadSessions()}
   // Escape dismisses the theater; the Files dialog handles this via Dialog.Root.
   const handleEscape=(event:KeyboardEvent)=>{
     if(event.key==='Escape'&&selected&&!fileViewer)closeMovie()
   }
   window.addEventListener('popstate',handleBack)
   window.addEventListener('keydown',handleEscape)
   checkAuth()
   const updaterPoll=setInterval(()=>{
     if(page==='settings'&&auth==='ready')void loadUpdateInfo(true)
   },4500)
   const fileInspectionPoll=setInterval(()=>{
     if(auth==='ready'&&fileViewer&&inspectedFiles?.fetching&&!fileViewerLoading){
       void loadFileList(fileViewer.hash,false,true)
     }
   },2000)
   const mkvPlaybackPoll=setInterval(()=>{
     if(auth==='ready'&&selected?.status==='completed'&&selectedIsMKV &&
        (mkvStatus?.state==='queued'||mkvStatus?.state==='processing')){
       const file=selectedPlayback.video
       if(file)void loadMKVStatus(selected.hash,file.path)
     }
   },2500)
   const sessionsPoll=setInterval(()=>{
     if(page==='settings'&&auth==='ready')void loadSessions(true)
   },10000)
   return ()=>{live?.close();stopWatchingTimer();clearInterval(updaterPoll);clearInterval(fileInspectionPoll);clearInterval(mkvPlaybackPoll);clearInterval(sessionsPoll);window.removeEventListener('popstate',handleBack);window.removeEventListener('keydown',handleEscape)}
 })
 const authenticate=async()=>{
   error=''
   if(!/^[0-9]{6}$/.test(pin)){error='Enter your 6-digit PIN';return}
   submitting=true
   try{
     await call('auth/login','POST',{pin})
     const status=await call('auth/status')
     if(!status.authenticated)throw new Error('Session cookie was not saved. Check your browser cookie settings.')
     pin=''
     auth='ready'
     await refresh()
     connect()
     if(page==='settings')void loadSessions()
     await loadResume()
   }catch(e){
     error=(e as Error).message
     pin=''
   }finally{submitting=false}
 }
 const signOut=async()=>{
   try{
     await call('auth/logout','POST',{})
     live?.close()
     closeMovie()
     connected=false
     movies=[]
     pin=''
     // Replace the deep-link history entry, then load the signed-out root.
     window.location.replace('/')
   }catch(e){
     fail((e as Error).message)
   }
 }
 const add=async()=>{
   submitting=true
   try{
     const settings=await call('settings')
     tmdbConfigured=Boolean(settings.tmdbConfigured)
     tmdbChecked=true
     if(!tmdbConfigured){
       fail('Set your TMDB API key in Settings before downloading a movie.')
       return
     }
     await call('magnets','POST',{magnet:magnet.trim()})
     magnet=''
     notify('Magnet queued. Checking its files for an MP4 or MKV movie before downloading.')
     await refresh()
   }catch(e){
     const message=(e as Error).message
     fail(message)
     if(message.includes('TMDB API key')){tmdbConfigured=false;tmdbChecked=true}
   }finally{submitting=false}
 }
 const copyTorrentMagnet=async(movie:Movie)=>{
   try{
     const result=await call('torrents/'+encodeURIComponent(movie.hash)+'/magnet')
     if(typeof result?.magnet!=='string'||!result.magnet.startsWith('magnet:?')){
       throw new Error('Magnet link unavailable for this download.')
     }
     if(!await copyToClipboard(result.magnet)){
       // Plain-HTTP VPS pages can be denied clipboard permissions. Fall back to
       // a read-only dialog with a selectable field and a retry button.
       magnetDialog={title:movie.title,magnet:result.magnet}
       return
     }
     notify('Magnet link copied')
   }catch(e){
     fail((e as Error).message)
   }
 }
 const copyMagnetAgain=async()=>{
   if(!magnetDialog)return
   if(await copyToClipboard(magnetDialog.magnet)){
     magnetDialog=null
     notify('Magnet link copied')
   }
 }
 const action=async(m:Movie,verb:'pause'|'resume'|'remove')=>{
   let deleteFiles=false
   if(verb==='remove'){
     if(m.status==='rejected'){
       // A rejected torrent never downloaded payload, so there is no third choice.
       const ok=await confirmDialog({title:'Delete this rejected torrent?',description:'Its record and temporary file list will be removed.',confirmLabel:'Delete',destructive:true})
       if(ok!==true)return
     }else{
       // Removing used to always keep the files, which left the disk full with
       // no way to clean up from the UI. Offer both outcomes explicitly.
       const choice=await confirmDialog({
         title:'Delete this torrent?',
         description:'The queue entry is removed either way. Choose whether the downloaded movie files on the server are deleted too.',
         cancelLabel:'Cancel',
         confirmLabel:'Remove from queue',
         alternateLabel:'Delete files too',
         alternateDestructive:true,
         alternateHint:'Deletes the downloaded movie and subtitle files from the server.',
       })
       if(choice==='cancel'||choice===false)return
       deleteFiles=choice==='alternate'
     }
   }
   try{
     await call('torrents/'+encodeURIComponent(m.hash)+'/'+verb,'POST',verb==='remove'?{deleteFiles}:{})
     if(verb==='remove' && selected?.hash===m.hash)closeMovie()
     if(verb==='remove' && fileViewer?.hash===m.hash)closeFileViewer()
     if(verb==='remove')notify(deleteFiles?'Torrent and downloaded files deleted':'Torrent removed. Files kept on the server.')
     await refresh()
   }
   catch(e){fail((e as Error).message)}
 }
</script>

<svelte:head>
 <title>{page==='home'?'Home':page==='settings'?'Settings':'Downloader'} · Cloudflix</title>
</svelte:head>

<Tooltip.Provider>
<Toaster position="bottom-right" closeButton richColors/>
{#if auth==='loading'}
 <div class="auth-screen">
  <div class="auth-wordmark"><span class="auth-logo"><Film size={22}/></span><span>cloud<strong>flix</strong></span></div>
  <Spinner class="size-5 text-muted-foreground"/>
  <span class="muted">Connecting to your server…</span>
 </div>
{:else if auth==='setup'}
 <div class="auth-screen">
  <div class="auth-card">
   <div class="brand" style="justify-content:center;margin-bottom:22px"><span class="brand-icon"><Film size={25}/></span><strong>cloud<span>flix</span></strong></div>
   <div class="auth-shield"><Shield size={26}/></div>
   <h1>Set up your PIN</h1>
   <p>Access is configured from the VPS terminal, not in the browser. Run the Cloudflix PIN helper as your server administrator, then reload this page.</p>
   <code>wget -qO- https://raw.githubusercontent.com/devseckobz/cloudflix/main/installer/pin.sh | sudo bash</code>
  </div>
 </div>
{:else if auth==='login'}
 <div class="auth-screen auth-minimal">
  <form class="auth-card auth-compact" novalidate onsubmit={(e)=>{e.preventDefault();void authenticate()}}>
   <div class="auth-wordmark"><span class="auth-logo"><Film size={21} strokeWidth={2.2}/></span><span>cloud<strong>flix</strong></span></div>
   <InputOTP.Root
    class="pin-otp{error?' pin-otp-invalid':''}"
    maxlength="6"
    numeric
    autofocus
    inputmode="numeric"
    autocomplete="off"
    aria-label="Access PIN"
    aria-invalid={Boolean(error)}
    bind:value={pin}
    disabled={submitting}
    onComplete={()=>{if(!submitting)void authenticate()}}
   >
    {#snippet children({ cells })}
     <InputOTP.Group class="pin-otp-group">
      {#each cells as cell, i (i)}
       <InputOTP.Slot {cell} class="pin-otp-slot"/>
      {/each}
     </InputOTP.Group>
    {/snippet}
   </InputOTP.Root>
   {#if error}<Alert.Root variant="destructive" class="auth-error"><AlertCircle size={15}/><Alert.Description>{error}</Alert.Description></Alert.Root>{/if}
   <Button class="auth-submit" type="submit" disabled={submitting||pin.length!==6}>
    {#if submitting}<Spinner class="size-4"/>Verifying…{:else}Continue<span aria-hidden="true">→</span>{/if}
   </Button>
   <span class="auth-footer">Your personal movie space</span>
  </form>
 </div>
{:else}
 <div class="app-shell{selected?' theater-open':''}" data-page={page}>
  <aside class="sidebar">
   <button class="brand brand-button" onclick={()=>navigate('home')} aria-label="Cloudflix homepage"><span class="brand-icon"><Film size={22}/></span><strong>cloud<span>flix</span></strong></button>
   <div class="side-label">Menu</div>
   <nav aria-label="Main navigation">
    <button class:active={page==='home'} aria-current={page==='home'?'page':undefined} onclick={()=>navigate('home')}><House size={18}/> Home</button>
    <button class:active={page==='downloader'} aria-current={page==='downloader'?'page':undefined} onclick={()=>navigate('downloader')}><Download size={18}/> Downloader {#if active>0}<span class="nav-count" aria-label={active+' active downloads'}>{active}</span>{/if}</button>
    <button class:active={page==='settings'} aria-current={page==='settings'?'page':undefined} onclick={()=>navigate('settings')}><Settings size={18}/> Settings</button>
   </nav>
   <div class="sidebar-bottom">
    {#if storage?.known}
     <div class="sidebar-storage">
      <div><HardDrive size={14}/><span>Server storage</span></div>
      <strong>{formatBytes(storage.free)} <span>free</span></strong>
      <Progress value={storageUsed} aria-label="Server disk usage"/>
      <small>{formatBytes(storage.total)} total capacity</small>
     </div>
    {/if}
    <div class="server-status" role="status"><span class:online={connected} class="status-dot" aria-hidden="true"></span><span>{connected?'Server connected':'Reconnecting…'}</span></div>
    <button type="button" onclick={signOut} class="signout"><LogOut size={17} aria-hidden="true"/> Sign out</button>
   </div>
  </aside>
  <main class="main">
   <header class="topbar">
    <div class="mobile-brand brand"><span class="brand-icon"><Film size={17}/></span><strong>cloud<span>flix</span></strong></div>
    {#if page==='home'}
     <label class="nav-movie-search">
      <Search size={16} aria-hidden="true"/>
      <input value={search} oninput={(event)=>{search=event.currentTarget.value;libraryPage=1}} placeholder="Search movies" aria-label="Search downloaded movies"/>
      {#if search.trim()}
       <button type="button" aria-label="Clear search" onclick={()=>{search='';libraryPage=1}}><X size={15} aria-hidden="true"/></button>
      {/if}
     </label>
    {:else}
     <span class="nav-page-caption">{page==='settings'?'Settings':'Downloader'}</span>
     <div class="topbar-status" role="status"><span class:online={connected} class="status-dot" aria-hidden="true"></span><span>{connected?'Connected':'Reconnecting'}</span></div>
    {/if}
   </header>
   {#if page==='home'}
    <section class="movie-library" aria-label="Downloaded movies">
     <!-- Screen-reader heading: the visible page is the poster grid only. -->
     <h1 class="sr-only">Your movie library</h1>
     {#if !stateLoaded}
      <div class="movie-library-grid" aria-hidden="true">
       {#each Array(10) as _, i (i)}
        <div class="movie-library-skeleton">
         <Skeleton class="movie-library-skeleton-poster"/>
         <Skeleton class="movie-library-skeleton-line"/>
         <Skeleton class="movie-library-skeleton-meta"/>
        </div>
       {/each}
      </div>
     {:else if libraryView.movies.length}
      <div class="movie-library-grid">
       {#each libraryView.movies as movie (movie.hash)}
        <button type="button" class="movie-library-card" title={'Play '+movie.title} onclick={()=>openMovie(movie)}>
         <div class="movie-library-poster">
          {#if movie.poster}
           <img src={poster(movie.poster)} alt={'Poster for '+movie.title} loading="lazy"/>
          {:else}
           <div class="movie-library-placeholder"><Film size={30} strokeWidth={1.5}/></div>
          {/if}
          <span class="movie-library-play" aria-hidden="true"><Play size={20} fill="currentColor"/></span>
         </div>
         <h3>{movie.title}</h3>
         <div class="movie-library-meta">
          <span class="movie-library-tags"><span class="movie-year-badge">{movie.year||'Unknown year'}</span>{#if movie.genre}<span class="movie-genre-badge">{movie.genre}</span>{/if}</span>
          <span class="movie-library-rating"><Star size={12} fill={movie.rating>0?'currentColor':'none'}/> {movie.rating>0?movie.rating.toFixed(1):'N/A'}</span>
         </div>
        </button>
       {/each}
      </div>
      <div class="library-pagination-footer">
       <Pagination.Root
        class="library-bottom-pagination"
        aria-label="Movie pages"
        count={libraryView.total}
        perPage={10}
        page={libraryView.page}
        onPageChange={(next)=>libraryPage=next}
       >
        <Pagination.Content>
         <Pagination.Item>
          <Pagination.PrevButton class="library-page-arrow" aria-label="Previous page" onclick={()=>libraryPage=libraryView.page-1}>
           {#snippet children()}<ChevronLeft size={15}/>{/snippet}
          </Pagination.PrevButton>
         </Pagination.Item>
         {#each libraryView.pages as number (number)}
          <Pagination.Item>
           <button type="button" class:active={libraryView.page===number} aria-label={'Page '+number} aria-current={libraryView.page===number?'page':undefined} onclick={()=>libraryPage=number}>{number}</button>
          </Pagination.Item>
         {/each}
         <Pagination.Item>
          <Pagination.NextButton class="library-page-arrow" aria-label="Next page" onclick={()=>libraryPage=libraryView.page+1}>
           {#snippet children()}<ChevronRight size={15}/>{/snippet}
          </Pagination.NextButton>
         </Pagination.Item>
        </Pagination.Content>
       </Pagination.Root>
       <p class="library-results-count">Showing {libraryView.from}–{libraryView.to} of {libraryView.total}</p>
      </div>
     {:else}
      <Empty.Root class="movie-library-empty">
       <Empty.Header>
        <Empty.Media variant="icon"><Film size={18}/></Empty.Media>
        <Empty.Title>{search.trim()?'No movies found':'No movies yet'}</Empty.Title>
        <Empty.Description>{search.trim()?'Try a different title or year.':'Your completed movie downloads will appear here as posters.'}</Empty.Description>
       </Empty.Header>
       <Empty.Content>
        {#if search.trim()}
         <Button variant="outline" size="sm" onclick={()=>{search='';libraryPage=1}}>Clear search</Button>
        {:else}
         <Button variant="outline" size="sm" onclick={()=>navigate('downloader')}>Open Downloader</Button>
        {/if}
       </Empty.Content>
      </Empty.Root>
     {/if}
    </section>
   {:else if page==='settings'}
    <div class="dashboard-content settings-page">
     <div class="page-title">
      <span class="eyebrow">Make it yours</span>
      <h1>Settings</h1>
      <p>A few essentials for your personal movie server.</p>
     </div>
     <Card.Root class="settings-panel">
      <div class="settings-panel-header">
       <div class="settings-panel-icon"><Film size={21}/></div>
       <div class="settings-panel-intro">
        <h2>TMDB Movie Metadata</h2>
        <p>Automatically find movie titles, posters, backdrop artwork, genres and release years.</p>
       </div>
       <Badge variant={tmdbConfigured?'secondary':'outline'} class="integration-status{tmdbConfigured?' configured':''}">{settingsLoading?'Checking…':tmdbConfigured?'● Connected':'○ Not configured'}</Badge>
      </div>
      <form class="settings-form" onsubmit={(event)=>{event.preventDefault();saveTMDB()}}>
       <Label for="tmdb-token">TMDB API key or Read Access Token</Label>
       <Input id="tmdb-token" type="password" bind:value={tmdbToken} autocomplete="off" autocapitalize="off" spellcheck={false} maxlength={2048} placeholder={tmdbConfigured?'Enter a new token to replace the saved one':'Paste your TMDB API key or Read Access Token'} />
       <p class="settings-hint">Your token is checked directly with TMDB and stored privately on this VPS. For security, the saved token is never displayed.</p>
       <div class="settings-actions">
        <Button type="submit" class="settings-save" disabled={settingsBusy||!tmdbToken.trim()}>{#if settingsBusy}<Spinner class="size-4"/>{:else}<CheckCircle2 size={16}/>{/if} Save &amp; verify token</Button>
        {#if tmdbConfigured}<Button type="button" variant="destructive" class="settings-remove" disabled={settingsBusy} onclick={clearTMDB}><Trash2 size={16}/> Remove token</Button>{/if}
       </div>
       <p class="settings-help">Get your token from <a href="https://www.themoviedb.org/settings/api" target="_blank" rel="noopener noreferrer">TMDB account settings</a>. Both the <strong>32-character API key</strong> and the <strong>API Read Access Token</strong> are supported.</p>
      </form>
     </Card.Root>
     <Card.Root class="settings-panel update-settings-panel" aria-label="Software updates">
      <div class="settings-panel-header">
       <div class="settings-panel-icon"><RefreshCw size={21}/></div>
       <div class="settings-panel-intro">
        <h2>Software Updates</h2>
        <p>Keep Cloudflix current with verified binaries from GitHub Releases. Your downloads and PIN stay intact.</p>
       </div>
       {#if updateInfo?.updateAvailable}
        <Badge variant="outline" class="integration-status update-available">● Update available</Badge>
       {:else if updateInfo && !updateInfo.checkError}
        <Badge variant="secondary" class="integration-status configured">● Up to date</Badge>
       {/if}
      </div>
      <div class="update-versions">
       <div><span>Installed version</span><strong>{updateInfo?.currentVersion||version}</strong></div>
       <div><span>Latest release</span><strong>{updateChecking?'Checking…':updateInfo?.latestVersion||'—'}</strong></div>
      </div>
      {#if updateInfo?.state==='running'||updateInfo?.state==='queued'}
       <Alert.Root class="update-progress"><Spinner class="size-4"/><Alert.Title>Updating Cloudflix</Alert.Title><Alert.Description>{updateInfo?.message||'Installing the latest release…'} This page will reconnect when the service restarts.</Alert.Description></Alert.Root>
      {:else if updateInfo?.state==='success'&&!updateInfo?.updateAvailable}
       <Alert.Root class="update-status-good"><CheckCircle2 size={15}/><Alert.Description>Your update finished successfully.</Alert.Description></Alert.Root>
      {:else if updateInfo?.state==='error'}
       <Alert.Root variant="destructive" class="update-status-error"><AlertCircle size={15}/><Alert.Description>{updateInfo.message}</Alert.Description></Alert.Root>
      {/if}
      {#if updateInfo?.checkError}
       <p class="settings-hint">{updateInfo.checkError}</p>
      {/if}
      {#if updateError}<Alert.Root variant="destructive" class="update-status-error"><AlertCircle size={15}/><Alert.Description>{updateError}</Alert.Description></Alert.Root>{/if}
      {#if updateInfo&&!updateInfo.updaterReady}
       <Alert.Root class="settings-warning"><AlertCircle size={15}/><Alert.Description>The web updater isn't installed. Run the one-line update command from your VPS terminal once to enable this feature.</Alert.Description></Alert.Root>
      {/if}
      <div class="settings-actions update-buttons">
       <Button type="button" variant="outline" disabled={updateChecking||updateBusy} onclick={()=>loadUpdateInfo()}><RefreshCw size={16}/> {updateChecking?'Checking…':'Check for updates'}</Button>
       {#if updateInfo?.updateAvailable}
        <Button type="button" disabled={updateBusy||!updateInfo.updaterReady||updateInfo.state==='running'||updateInfo.state==='queued'} onclick={startUpdate}>
         {#if updateBusy}<Spinner class="size-4"/>{:else}<ArrowUpCircle size={16}/>{/if}
         Update to {updateInfo.latestVersion}
        </Button>
       {/if}
      </div>
      <p class="settings-hint">Updates run through a restricted, root-owned systemd helper. The web app cannot run arbitrary commands.</p>
     </Card.Root>
     <Card.Root class="settings-panel sessions-settings-panel" aria-label="Active sessions">
      <div class="settings-panel-header">
       <div class="settings-panel-icon"><MonitorSmartphone size={21}/></div>
       <div class="settings-panel-intro">
        <h2>Active Sessions</h2>
        <p>Every browser signed in with your PIN. Sign out any device you don't recognise.</p>
       </div>
       {#if sessionsLoaded}
        <span class="integration-status">{sessions.length} {sessions.length===1?'device':'devices'}</span>
       {/if}
      </div>
      {#if sessionsError}
       <Alert.Root variant="destructive" class="update-status-error"><AlertCircle size={15}/><Alert.Description>{sessionsError}</Alert.Description></Alert.Root>
      {/if}
      {#if sessionsLoading&&!sessionsLoaded}
       <div class="sessions-empty" role="status"><Spinner class="size-5"/><p>Loading sessions…</p></div>
      {:else if sessions.length}
       <div class="session-list" role="list">
        {#each sessions as session (session.id)}
         {@const client=describeSession(session.userAgent)}
         {@const detail=clientDetail(session.userAgent)}
         <article class="session-item" class:current={session.current} role="listitem">
          <Avatar.Root class="session-avatar" aria-hidden="true">
           <Avatar.Fallback>{client.initial}</Avatar.Fallback>
          </Avatar.Root>
          <div class="session-item-main">
           <div class="session-item-heading">
            <h3 title={client.label}>{client.label}</h3>
            {#if session.current}<Badge variant="secondary" class="session-badge">This device</Badge>{/if}
            <!-- A row that still shows a saved position is "Paused", not "Idle":
                 the old label contradicted the progress line right beneath it. -->
            <span class="status-pill" class:watching={session.status==='watching'} class:paused={session.status!=='watching'&&session.watching}>{session.status==='watching'?'Watching':session.watching?'Paused':'Idle'}</span>
           </div>
           <p class="session-meta">
            <span>{session.ip||'Unknown IP'}</span>
            {#if detail}<span class="session-sep" aria-hidden="true">·</span><span class="session-client" title={session.userAgent||'No user agent recorded'}>{detail}</span>{/if}
            <span class="session-sep" aria-hidden="true">·</span>
            <span>Signed in {formatDate(session.createdAt)}</span>
            <span class="session-sep" aria-hidden="true">·</span>
            <span>Active {formatRelative(session.lastSeen)}</span>
           </p>
           {#if session.watching}
            <p class="session-watching"><MonitorPlay size={13} aria-hidden="true"/><span class="session-watching-title" title={session.watching.title}>{session.watching.title}</span><span class="session-sep" aria-hidden="true">·</span><span>{formatPosition(session.watching.position)}</span></p>
           {/if}
          </div>
          {#if !session.current}
           <Button variant="outline" size="sm" class="session-revoke" disabled={revokingSession===session.id} onclick={()=>revokeSession(session.id)} aria-label={'Sign out '+browserLabel(session.userAgent)+' session'}>
            {#if revokingSession===session.id}<Spinner class="size-4"/>{:else}<LogOut size={15}/>{/if} Sign out
           </Button>
          {/if}
         </article>
        {/each}
       </div>
      {:else}
       <div class="sessions-empty" role="status">
        <Users size={20} aria-hidden="true"/>
        <p>No other sessions are signed in. Signing in from another browser will list it here.</p>
       </div>
      {/if}
      <p class="settings-hint">Revoking a session signs that browser out immediately. Your current session stays signed in.</p>
     </Card.Root>
     <Card.Root class="settings-panel">
      <div class="settings-panel-header">
       <div class="settings-panel-icon"><HardDrive size={21}/></div>
       <div class="settings-panel-intro">
        <h2>Server Access</h2>
        <p>Access Cloudflix directly using your VPS IP address on default HTTP port 80.</p>
       </div>
      </div>
      <div class="settings-info">
       <span>Current server</span><strong>{typeof window!=='undefined'?window.location.host :'VPS_IP'}</strong>
      </div>
      <Alert.Root class="settings-warning"><AlertCircle size={15}/><Alert.Description>Direct HTTP access is unencrypted. On an untrusted network your PIN, session cookies, and any TMDB token submitted here may be intercepted. Restrict port 80 to trusted IP addresses where possible.</Alert.Description></Alert.Root>
     </Card.Root>
    </div>
   {:else}
    <div class="dashboard-content download-page">
     <div class="downloader-heading">
      <div class="page-title">
       <span class="eyebrow">Your queue</span>
       <h1>Downloads</h1>
       <p>Add movies and track their progress.</p>
      </div>
     </div>
     <div class="stat-grid downloader-stats">
      <Card.Root class="stat"><div class="stat-icon"><Clapperboard size={17}/></div><div><span>All downloads</span><strong>{movies.length}</strong></div></Card.Root>
      <Card.Root class="stat"><div class="stat-icon"><Download size={17}/></div><div><span>Downloading</span><strong>{active}</strong></div></Card.Root>
      <Card.Root class="stat"><div class="stat-icon"><CheckCircle2 size={17}/></div><div><span>Completed</span><strong>{completed}</strong></div></Card.Root>
      <Card.Root class="stat"><div class="stat-icon"><HardDriveDownload size={17}/></div><div><span>Downloaded</span><strong>{formatBytes(movies.reduce((a,m)=>a+m.completed,0))}</strong></div></Card.Root>
      <Card.Root class="stat"><div class="stat-icon"><Database size={17}/></div><div><span>Space for downloads</span><strong>{storage?.known?formatBytes(storage.available):'—'}</strong></div></Card.Root>
     </div>
     {#if storage&&!storage.known}
      <Alert.Root class="storage-overview">
       <Shield size={14}/>
       <Alert.Title>Storage check</Alert.Title>
       <Alert.Description class="storage-error">{storage.error||'Checking server storage before downloads can start.'}</Alert.Description>
      </Alert.Root>
     {/if}
     {#if tmdbChecked&&!tmdbConfigured&&!settingsLoading}
      <Alert.Root class="tmdb-required">
       <AlertCircle size={19}/>
       <Alert.Title>TMDB API key required</Alert.Title>
       <Alert.Description>Set your TMDB API key in Settings before downloading a movie.</Alert.Description>
       <Alert.Action>
        <Button variant="outline" size="sm" class="tmdb-settings-button" onclick={()=>navigate('settings')}><Settings size={15}/> Open Settings</Button>
       </Alert.Action>
      </Alert.Root>
     {/if}
     <section class="downloader-magnet-panel" aria-label="Add a movie download">
      <form class="magnet-form downloader-magnet-form" onsubmit={(e)=>{e.preventDefault();void add()}}>
       <div class="magnet-panel-heading">
        <div><h2>Add a download</h2><p>Paste a magnet link to add a movie to your queue.</p></div>
        <Badge variant="outline" class="magnet-media-hint">MP4 · MKV · SRT</Badge>
       </div>
       <Label for="magnet">Magnet link</Label>
       <div class="magnet-entry-row">
        <Textarea id="magnet" bind:value={magnet} placeholder="magnet:?xt=urn:btih:..." rows={1} required spellcheck={false}/>
        <Button class="magnet-add-button" type="submit" disabled={submitting||!magnet.trim()||(tmdbChecked&&!tmdbConfigured)}>
         {#if submitting}<Spinner class="size-4"/>{:else}<Plus size={17}/>{/if}
         Add download
        </Button>
       </div>
       <p class="magnet-disclaimer"><Shield size={12}/> File size and available space are checked before downloading. Only download content you're authorized to use.</p>
      </form>
     </section>
     <section class="download-queue-section" aria-label="Download queue">
      <div class="download-queue-toolbar">
       <div class="download-queue-heading">
        <h2>Queue <span>{movies.length}</span></h2>
        <p>{active} downloading · {completed} completed</p>
       </div>
       <div class="download-queue-filters" role="group" aria-label="Filter downloads">
        <Toggle class="queue-filter-toggle" pressed={queueFilter==='all'} onPressedChange={(pressed)=>{if(pressed)queueFilter='all'}} aria-label="Show all downloads">All</Toggle>
        <Toggle class="queue-filter-toggle" pressed={queueFilter==='active'} onPressedChange={(pressed)=>{if(pressed)queueFilter='active'}} aria-label="Show in-progress downloads">In progress</Toggle>
        <Toggle class="queue-filter-toggle" pressed={queueFilter==='completed'} onPressedChange={(pressed)=>{if(pressed)queueFilter='completed'}} aria-label="Show completed downloads">Completed</Toggle>
       </div>
      </div>
      {#if queueItems.length}
       <div class="download-list queue-list">
        {#each queueItems as m (m.hash)}
         <article class="download-item queue-item" class:download-rejected={m.status==='rejected'}>
          <div class="download-art queue-art">
           {#if m.poster}<img src={poster(m.poster)} alt="" loading="lazy"/>{:else}<Film size={24}/>{/if}
          </div>
          <div class="download-info queue-info">
           <div class="download-top queue-item-heading">
            <h3 title={m.title}>{m.title}</h3>
            <span class="status-pill" class:complete={m.status==='completed'} class:rejected={m.status==='rejected'}>{statusLabel(m.status)}</span>
           </div>
           {#if m.status==='rejected'}
            <p class="torrent-rejection"><AlertCircle size={13}/> {m.error||'Only MP4/MKV movies and SRT subtitles are supported.'}</p>
           {:else}
            <div class="queue-file-info">
             {#if m.year}<span class="movie-library-tags"><span class="movie-year-badge">{m.year}</span>{#if m.genre}<span class="movie-genre-badge">{m.genre}</span>{/if}</span>{/if}
             <span>{formatBytes(m.completed)} / {formatBytes(m.size)}</span>
             {#if m.status==='completed'}<span class="queue-ready"><CheckCircle2 size={12}/> Ready to watch</span>{/if}
            </div>
            {#if m.status==='downloading'||m.status==='paused'}
             <div class="queue-progress">
              <Progress class="queue-progress-bar" value={percent(m)} aria-label={m.title+' download progress'}/>
              <span>{percent(m).toFixed(1)}%</span>
             </div>
            {:else if m.status==='metadata'}
             <p class="queue-processing">Inspecting torrent metadata before downloading…</p>
            {/if}
           {/if}
          </div>
          <div class="download-actions queue-actions">
           {#if m.status==='paused'}
            <Tooltip.Root>
             <Tooltip.Trigger>
              {#snippet child({ props })}
               <button {...props} class="queue-icon-button" aria-label={'Resume '+m.title} onclick={()=>action(m,'resume')}><Play size={16}/></button>
              {/snippet}
             </Tooltip.Trigger>
             <Tooltip.Content>Resume download</Tooltip.Content>
            </Tooltip.Root>
           {:else if m.status==='downloading'}
            <Tooltip.Root>
             <Tooltip.Trigger>
              {#snippet child({ props })}
               <button {...props} class="queue-icon-button" aria-label={'Pause '+m.title} onclick={()=>action(m,'pause')}><Pause size={16}/></button>
              {/snippet}
             </Tooltip.Trigger>
             <Tooltip.Content>Pause download</Tooltip.Content>
            </Tooltip.Root>
           {/if}
           {#if m.status==='completed'}
            <Button variant="outline" size="sm" class="queue-watch-button" onclick={()=>openMovie(m)}><MonitorPlay size={15}/><span>Watch</span></Button>
           {/if}
           <Button variant="ghost" size="sm" class="queue-files-button" onclick={()=>showFiles(m)} aria-label={'View files for '+m.title}><Files size={15}/><span>Files</span></Button>
           <Tooltip.Root>
            <Tooltip.Trigger>
             {#snippet child({ props })}
              <button {...props} class="queue-copy-button queue-icon-button" aria-label={'Copy magnet link for '+m.title} onclick={()=>copyTorrentMagnet(m)}><Copy size={16}/></button>
             {/snippet}
            </Tooltip.Trigger>
            <Tooltip.Content>Copy magnet link</Tooltip.Content>
           </Tooltip.Root>
           <Tooltip.Root>
            <Tooltip.Trigger>
             {#snippet child({ props })}
              <button {...props} class="danger queue-icon-button" title={'Remove '+m.title+' from queue'} aria-label={'Remove '+m.title} onclick={()=>action(m,'remove')}><Trash2 size={16}/></button>
             {/snippet}
            </Tooltip.Trigger>
            <Tooltip.Content>Remove from queue</Tooltip.Content>
           </Tooltip.Root>
          </div>
         </article>
        {/each}
       </div>
      {:else}
       <Empty.Root class="queue-empty">
        <Empty.Header>
         <Empty.Media variant="icon"><Download size={18}/></Empty.Media>
         <Empty.Title>{movies.length?'No downloads in this view':'Your queue is empty'}</Empty.Title>
         <Empty.Description>{movies.length?'Try a different filter to see your other downloads.':'Add a magnet link above to start downloading.'}</Empty.Description>
        </Empty.Header>
        {#if movies.length}
         <Empty.Content><Button variant="outline" size="sm" onclick={()=>queueFilter='all'}>Show all downloads</Button></Empty.Content>
        {/if}
       </Empty.Root>
      {/if}
     </section>
    </div>
   {/if}
   <footer class="footer"><span>© Cloudflix · Made with ❤️ by Kobz</span><span>Movie data from <a href="https://www.themoviedb.org/" target="_blank" rel="noopener noreferrer">TMDB</a>; not endorsed or certified by TMDB.</span></footer>
  </main>
  <nav class="mobile-nav" aria-label="Mobile navigation">
   <button type="button" class:active={page==='home'} aria-current={page==='home'?'page':undefined} onclick={()=>navigate('home')}><House size={19} aria-hidden="true"/> Home</button>
   <button type="button" class:active={page==='downloader'} aria-current={page==='downloader'?'page':undefined} onclick={()=>navigate('downloader')}><Download size={19} aria-hidden="true"/> Downloader {#if active>0}<span class="mobile-download-count" aria-label={active+' active downloads'}>{active}</span>{/if}</button>
   <button type="button" class:active={page==='settings'} aria-current={page==='settings'?'page':undefined} onclick={()=>navigate('settings')}><Settings size={19} aria-hidden="true"/> Settings</button>
   <button type="button" onclick={signOut}><LogOut size={19} aria-hidden="true"/> Log out</button>
  </nav>
 </div>
 {#if fileViewer}
  <Dialog.Root open={Boolean(fileViewer)} onOpenChange={(open)=>{if(!open)closeFileViewer()}}>
   <Dialog.Content class="file-inspector" showCloseButton={false} onInteractOutside={(event)=>event.preventDefault()} aria-label={'Torrent files for '+fileViewer.title} aria-describedby={undefined}>
    <header class="file-inspector-header">
     <div class="file-inspector-heading">
      <Dialog.Title>Files</Dialog.Title>
      <p title={fileViewer.title}>{fileViewer.title}</p>
     </div>
     <Button variant="ghost" size="icon-sm" class="file-inspector-close" aria-label="Close files" onclick={closeFileViewer}><X size={17}/></Button>
    </header>
    {#if fileViewer.status==='rejected'}
     <Alert.Root variant="destructive" class="file-inspector-alert"><AlertCircle size={16}/><Alert.Description>{inspectedFiles?.reason||'Torrent rejected. No file content was downloaded.'}</Alert.Description></Alert.Root>
    {/if}
    {#if fileViewerLoading}
     <div class="file-inspector-empty" role="status">
      <div class="file-inspector-skeleton" aria-hidden="true">
       {#each Array(5) as _, i (i)}<Skeleton class="file-inspector-skeleton-row"/>{/each}
      </div>
      <p>Inspecting torrent file list…</p>
     </div>
    {:else if fileViewerError}
     <div class="file-inspector-empty" role="alert"><AlertCircle size={21}/><p>{fileViewerError}</p><Button variant="outline" size="sm" onclick={retryFileInspection}>Retry</Button></div>
    {:else if inspectedFiles?.fetching}
     <div class="file-inspector-empty" role="status">
      <Spinner class="size-5"/>
      <p>{inspectedFiles.message||'Fetching torrent filenames from peers…'}</p>
      <p class="file-inspector-note">Only torrent metadata is requested. No movie files will be downloaded.</p>
     </div>
    {:else if inspectedFiles?.available}
     <div class="file-inspector-summary">
      <span>{inspectedFiles.total} {inspectedFiles.total===1?'file':'files'} found</span>
      <Badge variant={inspectedFiles.rejected?'destructive':'secondary'} class="file-inspector-summary-badge">{inspectedFiles.rejected?'No files downloaded':'MP4 · MKV · SRT selected'}</Badge>
     </div>
     <ScrollArea.Root class="file-inspector-list" orientation="vertical">
      <div role="list">
       {#each inspectedFiles.files as file,i (i)}
        <div class="file-inspector-item" role="listitem">
         <span class="file-inspector-item-icon"><Files size={15}/></span>
         <span class="file-inspector-filepath" title={file.path}>{file.path}</span>
         <span class="file-inspector-filesize">{formatBytes(file.size)}</span>
         <Badge variant={file.selected?'secondary':'outline'} class="file-inspector-file-status{file.selected?' selected':''}">{file.selected?'Selected':inspectedFiles.rejected?'Not downloaded':'Skipped'}</Badge>
        </div>
       {/each}
      </div>
     </ScrollArea.Root>
     {#if inspectedFiles.truncated}
      <p class="file-inspector-note">Showing the first {inspectedFiles.files.length} files of {inspectedFiles.total}. The file list is limited for performance.</p>
     {/if}
    {:else}
     <div class="file-inspector-empty">
      <Files size={22}/>
      <p>{inspectedFiles?.message||'No file metadata available yet.'}</p>
      {#if fileViewer.status==='rejected'}
       <Button variant="outline" size="sm" onclick={retryFileInspection}>Retry file inspection</Button>
      {/if}
     </div>
    {/if}
   </Dialog.Content>
  </Dialog.Root>
 {/if}
 {#if magnetDialog}
  <Dialog.Root open={Boolean(magnetDialog)} onOpenChange={(open)=>{if(!open)magnetDialog=null}}>
   <Dialog.Content class="magnet-dialog" aria-describedby={undefined}>
    <Dialog.Header>
     <Dialog.Title>Copy magnet link</Dialog.Title>
     <Dialog.Description>Your browser blocked clipboard access. Select the link below and copy it manually.</Dialog.Description>
    </Dialog.Header>
    <Label for="magnet-copy-field">Magnet link for {magnetDialog.title}</Label>
    <Input id="magnet-copy-field" class="magnet-copy-field" readonly value={magnetDialog.magnet} onfocus={(event)=>event.currentTarget.select()} onclick={(event)=>event.currentTarget.select()} spellcheck={false}/>
    <Dialog.Footer>
     <Button variant="outline" onclick={()=>magnetDialog=null}>Close</Button>
     <Button onclick={copyMagnetAgain}><Copy size={15}/> Copy again</Button>
    </Dialog.Footer>
   </Dialog.Content>
  </Dialog.Root>
 {/if}
 {#if selected}
  <div class="modal-backdrop cinema-player-backdrop" role="presentation">
   <div class="cinema-player-modal" role="dialog" aria-modal="true" aria-label={'Watch '+selected.title}>
    <header class="cinema-player-heading">
     <div class="player-title-group">
      <h2>{selected.title}</h2>
      <p><span class="movie-library-tags"><span class="movie-year-badge">{selected.year||'Movie'}</span>{#if selected.genre}<span class="movie-genre-badge">{selected.genre}</span>{/if}</span> {#if selected.rating>0}<span aria-hidden="true">·</span><Star size={12} fill="currentColor"/> {selected.rating.toFixed(1)} / 10{/if}</p>
     </div>
     <Button variant="ghost" size="icon" class="cinema-player-close" onclick={closeMovie} aria-label="Close player" title="Close"><X size={18}/></Button>
    </header>
    {#if selected.status==='completed' && selectedPlayback.video}
     {#if !selectedIsMKV || mkvStatus?.state==='ready'}
     <div class="cinema-video-frame">
      {#key selected.hash+selectedPlayback.video.path+(playbackVideoURL||'')}
       <video
        bind:this={playerVideo}
        class="cinema-video"
        controls
        autoplay
        playsinline
        preload="metadata"
        onloadedmetadata={handleMetadata}
        onpause={handlePause}
        poster={selected.backdrop?backdrop(selected.backdrop):selected.poster?poster(selected.poster):undefined}
        onerror={()=>playbackError='Your browser cannot decode this video. Check that FFmpeg has prepared a compatible MP4.'}
        aria-label={'Playback of '+selected.title}
       >
        <source src={selectedIsMKV?(playbackVideoURL||''):mediaURL(selected.hash,selectedPlayback.video.path)} type="video/mp4"/>
        {#each playbackTracks as sub,i (sub.path)}
         <track kind="subtitles" src={sub.src} srclang={sub.language} label={sub.label} onload={syncSubtitleTracks}/>
        {/each}
        Your browser does not support HTML5 video.
       </video>
      {/key}
     </div>
     {#if playbackError}<p class="player-error" role="alert"><AlertCircle size={15}/>{playbackError}</p>{/if}
     <div class="cinema-player-toolbar">
      <div class="cinema-player-hint">
       <span>{formatBytes(selectedPlayback.video.size)}</span>
       {#if playbackTracks.length}
        <span class="player-toolbar-sep" aria-hidden="true">·</span>
        <span>{playbackTracks.length} {playbackTracks.length===1?'subtitle':'subtitles'} available</span>
       {/if}
      </div>
      <div class="cinema-player-actions">
       {#if playbackTracks.length}
        <Tooltip.Root>
         <Tooltip.Trigger>
          {#snippet child({ props })}
           <button
            {...props}
            class="cinema-cc-button"
            class:active={ccEnabled}
            type="button"
            aria-label={ccEnabled?'Disable captions':'Enable captions'}
            aria-pressed={ccEnabled}
            title={ccEnabled?'Turn captions off':'Turn captions on'}
            onclick={toggleCaptions}
           >CC</button>
          {/snippet}
         </Tooltip.Trigger>
         <Tooltip.Content>{ccEnabled?'Turn captions off':'Turn captions on'}</Tooltip.Content>
        </Tooltip.Root>
        {#if ccEnabled}
         <div class="cinema-caption-languages" role="group" aria-label="Available subtitles">
          {#each playbackTracks as sub,i (sub.path)}
           <button
            type="button"
            class:active={selectedSubtitle===i}
            aria-pressed={selectedSubtitle===i}
            title={'Use '+sub.label+' subtitles'}
            onclick={()=>selectCaptionTrack(i)}
           >{sub.label}</button>
          {/each}
         </div>
        {/if}
       {/if}
       <Tooltip.Root>
        <Tooltip.Trigger>
         {#snippet child({ props })}
          <Button {...props} variant="outline" size="sm" class="cinema-fullscreen-button" onclick={enterFullscreen} aria-label="Enter fullscreen"><Maximize2 size={15}/> Fullscreen</Button>
         {/snippet}
        </Tooltip.Trigger>
        <Tooltip.Content>Enter fullscreen</Tooltip.Content>
       </Tooltip.Root>
      </div>
     </div>
     {:else}
      <div class="cinema-player-preparing" role="status">
       {#if mkvStatus?.state==='error'}
        <AlertCircle size={30}/>
        <h3>Unable to prepare MKV</h3>
        <p>{mkvStatus.message||'Playback preparation failed.'}</p>
        <Button variant="outline" size="sm" onclick={()=>selected&&prepareMKV(selected)}>Try again</Button>
       {:else}
        <Spinner class="size-7"/>
        <h3>Preparing MKV for playback</h3>
        <p>{mkvStatus?.message||'This movie needs conversion to a browser-compatible format.'}</p>
        <span>Embedded text subtitles will be available in CC when ready.</span>
       {/if}
      </div>
     {/if}
    {:else}
     <div class="cinema-player-unavailable">
      <MonitorPlay size={34} strokeWidth={1.2}/>
      <h3>{selected.status==='rejected'?'Torrent was rejected':'Movie not ready yet'}</h3>
      <p>{selected.error||'Playback is available when the movie download finishes.'}</p>
     </div>
    {/if}
   </div>
  </div>
 {/if}
{/if}
</Tooltip.Provider>
