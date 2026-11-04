<script lang="ts">
 import { onMount } from 'svelte'
 import { selectLibraryPage, countDownloading } from './lib/library.js'
 import {findPlayback,mediaURL} from './lib/media.js'
 import {createVersionReloadMonitor} from './lib/version-reload.js'
 import {applySubtitleSelection} from './lib/subtitles.js'
 import { Film, House, Download, Search, ArrowDownToLine, Pause, Play, Trash2, HardDrive, Settings, LogOut, Plus, X, CheckCircle2, LoaderCircle, Shield, Clapperboard, AlertCircle, ExternalLink, Star, ChevronLeft, ChevronRight, Maximize2, Clapperboard as FilmClap, MonitorPlay, RefreshCw, ArrowUpCircle } from '@lucide/svelte'

 type Movie = {hash:string,title:string,year:string,poster:string,backdrop:string,rating:number,size:number,completed:number,progress:number,status:string,error?:string,files:{path:string,size:number,completed?:number}[]}
 let page = $state<'home'|'downloader'|'settings'>(typeof window!=='undefined'&&window.location.pathname==='/settings'?'settings':typeof window!=='undefined'&&window.location.pathname==='/downloader'?'downloader':'home')
 let auth = $state<'loading'|'setup'|'login'|'ready'>('loading')
 let pin = $state('')
 let movies = $state<Movie[]>([])
 let error = $state('')
 let notice = $state('')
 let magnet = $state('')
 let submitting = $state(false)
 let search = $state('')
 let libraryPage = $state(1)
 let queueFilter = $state<'all'|'active'|'completed'>('all')
 let selected = $state<Movie|null>(null)
 let playerVideo = $state<HTMLVideoElement|null>(null)
 let playbackError = $state('')
 let ccEnabled = $state(false)
 let selectedSubtitle = $state(0)
 let noticeTimer:ReturnType<typeof setTimeout>|undefined
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
 // Ignore the initial server version; reload only when the running binary changes.
 const versionMonitor=createVersionReloadMonitor(()=>window.location.reload())
 const navigate=(destination:'home'|'downloader'|'settings')=>{
   if(destination==='settings'||destination==='downloader')void loadSettings()
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
 const poster=(path:string)=>path?'https://image.tmdb.org/t/p/w500'+path:''
 const backdrop=(path:string)=>path?'https://image.tmdb.org/t/p/w1280'+path:''
 const completed = $derived(movies.filter(m=>m.status==='completed').length)
 const active = $derived(countDownloading(movies))
 const libraryView = $derived(selectLibraryPage(movies,search,libraryPage,10))
 const queueItems = $derived(movies.filter(m=>queueFilter==='all'||(queueFilter==='active'&&m.status!=='completed'&&m.status!=='rejected')||(queueFilter==='completed'&&m.status==='completed')))
 const selectedPlayback = $derived(selected?findPlayback(selected.files):{video:null,subtitles:[]})
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
 const notify=(message:string)=>{
   if(noticeTimer!==undefined)clearTimeout(noticeTimer)
   notice=message
   noticeTimer=setTimeout(()=>{notice='';noticeTimer=undefined},4200)
 }
 const syncSubtitleTracks=()=>{
   applySubtitleSelection(playerVideo,ccEnabled?selectedSubtitle:-1)
 }
 const toggleCaptions=()=>{
   if(!selectedPlayback.subtitles.length)return
   ccEnabled=!ccEnabled
   syncSubtitleTracks()
 }
 const selectCaptionTrack=(index:number)=>{
   if(!Number.isInteger(index)||index<0||index>=selectedPlayback.subtitles.length)return
   selectedSubtitle=index
   ccEnabled=true
   syncSubtitleTracks()
 }
 const openMovie=(movie:Movie)=>{
   playbackError=''
   playerVideo=null
   selectedSubtitle=0
   ccEnabled=false
   selected=movie
 }
 const closeMovie=()=>{
   playerVideo?.pause()
   playerVideo=null
   selected=null
   selectedSubtitle=0
   ccEnabled=false
   playbackError=''
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
   if(!confirm('Install Cloudflix '+updateInfo.latestVersion+' now? Your server will restart briefly and your movies will be preserved.'))return
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
   }catch(e){error=(e as Error).message}
   finally{
     settingsLoading=false
     if(page==='settings')void loadUpdateInfo(true)
   }
 }
 const saveTMDB=async()=>{
   if(!tmdbToken.trim()){error='Paste a TMDB API key or Read Access Token first';return}
   settingsBusy=true;error='';notice=''
   try{
     const result=await call('settings','PUT',{tmdbApiKey:tmdbToken.trim()})
     tmdbConfigured=Boolean(result.tmdbConfigured)
     tmdbToken=''
     notify('TMDB API token verified and saved. Movie artwork will refresh automatically.')
   }catch(e){error=(e as Error).message}
   finally{settingsBusy=false}
 }
 const clearTMDB=async()=>{
   if(!confirm('Remove the TMDB API token? Existing artwork will remain, but new movie metadata will not be fetched.'))return
   settingsBusy=true;error='';notice=''
   try{
     await call('settings','PUT',{tmdbApiKey:''})
     tmdbConfigured=false
     tmdbToken=''
     notify('TMDB API token removed.')
   }catch(e){error=(e as Error).message}
   finally{settingsBusy=false}
 }
 const setState=(state:{movies:Movie[],version?:string})=>{
   // SSE also covers terminal-based updates and all pages, not just Settings.
   if(state.version && versionMonitor.observe(state.version))return
   movies=state.movies||[]
   if(state.version)version=state.version
   if(selected)selected=movies.find(m=>m.hash===selected?.hash)||null
 }
 const connect=()=>{
   live?.close()
   live=new EventSource('/api/events')
   live.onopen=()=>connected=true
   live.onmessage=(event)=>{try{setState(JSON.parse(event.data))}catch{}}
   live.onerror=()=>connected=false
 }
 const refresh=async()=>{try{setState(await call('state'))}catch(e){error=String(e)}}
 const checkAuth=async()=>{
   try{
     const state=await call('auth/status')
     auth=state.setupRequired?'setup':state.authenticated?'ready':'login'
     if(auth==='ready'){await refresh();connect();if(page==='settings'||page==='downloader')await loadSettings()}
   }catch{error='Unable to connect to Cloudflix server';auth='login'}
 }
 onMount(()=>{
   const handleBack=()=>{page=window.location.pathname==='/settings'?'settings':window.location.pathname==='/downloader'?'downloader':'home';if(page==='settings'||page==='downloader')void loadSettings()}
   window.addEventListener('popstate',handleBack)
   checkAuth()
   const updaterPoll=setInterval(()=>{
     if(page==='settings'&&auth==='ready')void loadUpdateInfo(true)
   },4500)
   return ()=>{live?.close();clearInterval(updaterPoll);if(noticeTimer!==undefined)clearTimeout(noticeTimer);window.removeEventListener('popstate',handleBack)}
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
   }catch(e){
     error=(e as Error).message
     pin=''
   }finally{submitting=false}
 }
 const signOut=async()=>{closeMovie();try{await call('auth/logout','POST',{})}catch{}live?.close();connected=false;auth='login';movies=[]}
 const add=async()=>{
   error='';notice='';submitting=true
   try{
     const settings=await call('settings')
     tmdbConfigured=Boolean(settings.tmdbConfigured)
     tmdbChecked=true
     if(!tmdbConfigured){
       error='Set your TMDB API key in Settings before downloading a movie.'
       return
     }
     await call('magnets','POST',{magnet:magnet.trim()})
     magnet=''
     notify('Magnet queued. Checking its files for an MP4 movie before downloading.')
     await refresh()
   }catch(e){
     error=(e as Error).message
     if(error.includes('TMDB API key')){tmdbConfigured=false;tmdbChecked=true}
   }finally{submitting=false}
 }
 const action=async(m:Movie,verb:'pause'|'resume'|'remove')=>{
   if(verb==='remove'&&!confirm('Remove this torrent from Cloudflix? Downloaded files are retained.'))return
   error='';notice=''
   try{await call('torrents/'+encodeURIComponent(m.hash)+'/'+verb,'POST',{});if(verb==='remove' && selected?.hash===m.hash)closeMovie();await refresh()}
   catch(e){error=(e as Error).message}
 }
</script>

<svelte:head>
 <title>{page==='home'?'Home':page==='settings'?'Settings':'Downloader'} · Cloudflix</title>
</svelte:head>

{#if auth==='loading'}
 <div class="auth-screen"><div class="brand mark"><Film size={42}/><strong>cloud<span>flix</span></strong></div><span class="muted">Connecting to your server...</span></div>
{:else if auth==='setup'}
 <div class="auth-screen">
  <div class="auth-glow"></div>
  <div class="auth-card">
   <div class="brand"><span class="brand-icon"><Film size={28}/></span><strong>cloud<span>flix</span></strong></div>
   <div class="auth-shield"><Shield size={28}/></div>
   <h1>Set up your PIN</h1>
   <p>Access is configured from the VPS terminal, not in the browser. Run the Cloudflix PIN helper as your server administrator, then reload this page.</p>
   <p><code>wget -qO- https://raw.githubusercontent.com/devseckobz/cloudflix/main/installer/pin.sh | sudo bash</code></p>
  </div>
 </div>
{:else if auth==='login'}
 <div class="auth-screen auth-minimal">
  <form class="auth-card auth-compact" novalidate onsubmit={(e)=>{e.preventDefault();void authenticate()}}>
   <div class="auth-wordmark"><span class="auth-logo"><Film size={23} strokeWidth={2.2}/></span><span>cloud<strong>flix</strong></span></div>
   <label for="pin">6-digit access PIN</label>
   <input id="pin" class="pin-field" type="password" inputmode="numeric" maxlength="6" autocomplete="off" bind:value={pin} oninput={(event)=>{const digits=event.currentTarget.value.replace(/[^0-9]/g,'').slice(0,6);event.currentTarget.value=digits;pin=digits;error=''}} placeholder="••••••" aria-invalid={Boolean(error)} />
   {#if error}<div class="auth-error" role="alert"><AlertCircle size={16}/><span>{error}</span></div>{/if}
   <button class="primary auth-submit" disabled={submitting||pin.length!==6} type="submit">{#if submitting}<LoaderCircle size={17} class="spin"/>{:else}Continue <span aria-hidden="true">→</span>{/if}</button>
   <span class="auth-footer">Your personal movie space</span>
  </form>
 </div>
{:else}
 <div class="app-shell">
  <aside class="sidebar">
   <button class="brand brand-button" onclick={()=>navigate('home')} aria-label="Cloudflix homepage"><span class="brand-icon"><Film size={25}/></span><strong>cloud<span>flix</span></strong></button>
   <div class="side-label">MENU</div>
   <nav aria-label="Main navigation">
    <button class:active={page==='home'} onclick={()=>navigate('home')}><House size={19}/> Home</button>
    <button class:active={page==='downloader'} onclick={()=>navigate('downloader')}><Download size={19}/> Downloader {#if active>0}<span class="nav-count" aria-label={active+' active downloads'}>{active}</span>{/if}</button>
    <button class:active={page==='settings'} onclick={()=>navigate('settings')}><Settings size={19}/> Settings</button>
   </nav>
   <div class="sidebar-bottom">
    <div class="server-status"><span class:online={connected} class="status-dot"></span><span>{connected?'Server connected':'Reconnecting...'}</span></div>
    <button onclick={signOut} class="signout"><LogOut size={18}/> Sign out</button>
   </div>
  </aside>
  <main class="main">
   <header class="topbar">
    <div class="mobile-brand brand"><span class="brand-icon"><Film size={19}/></span><strong>cloud<span>flix</span></strong></div>
    {#if page==='home'}
     <nav class="top-library-pagination" aria-label="Movie pages">
      <button type="button" aria-label="Previous page" disabled={libraryView.page===1} onclick={()=>libraryPage=libraryView.page-1}><ChevronLeft size={16}/></button>
      {#each libraryView.pages as number}
       <button type="button" class:active={libraryView.page===number} aria-label={'Page '+number} aria-current={libraryView.page===number?'page':undefined} onclick={()=>libraryPage=number}>{number}</button>
      {/each}
      <button type="button" aria-label="Next page" disabled={libraryView.page===libraryView.totalPages} onclick={()=>libraryPage=libraryView.page+1}><ChevronRight size={16}/></button>
     </nav>
     <label class="nav-movie-search">
      <Search size={18}/>
      <input value={search} oninput={(event)=>{search=event.currentTarget.value;libraryPage=1}} placeholder="Search movies" aria-label="Search downloaded movies"/>
      {#if search.trim()}
       <button type="button" aria-label="Clear search" onclick={()=>{search='';libraryPage=1}}><X size={16}/></button>
      {/if}
     </label>
    {:else}
     <span class="nav-page-caption">{page==='settings'?'Settings':'Downloader'}</span>
    {/if}
   </header>
   {#if notice}<div class="toast success"><CheckCircle2 size={16}/>{notice}<button onclick={()=>notice=''} aria-label="Dismiss"><X size={16}/></button></div>{/if}
   {#if error}<div class="toast error"><AlertCircle size={16}/>{error}<button onclick={()=>error=''} aria-label="Dismiss"><X size={16}/></button></div>{/if}
   {#if page==='home'}
    <section class="movie-library" aria-label="Downloaded movies">
     {#if libraryView.movies.length}
      <div class="movie-library-grid">
       {#each libraryView.movies as movie (movie.hash)}
        <button type="button" class="movie-library-card" title={'Play '+movie.title} onclick={()=>openMovie(movie)}>
         <div class="movie-library-poster">
          {#if movie.poster}
           <img src={poster(movie.poster)} alt={'Poster for '+movie.title} loading="lazy"/>
          {:else}
           <div class="movie-library-placeholder"><Film size={34} strokeWidth={1.5}/></div>
          {/if}
          <span class="movie-library-play" aria-hidden="true"><Play size={22} fill="currentColor"/></span>
         </div>
         <h3>{movie.title}</h3>
         <div class="movie-library-meta">
          <span>{movie.year||'Unknown year'}</span>
          <span class="movie-library-rating"><Star size={13} fill={movie.rating>0?'currentColor':'none'}/> {movie.rating>0?movie.rating.toFixed(1):'N/A'}</span>
         </div>
        </button>
       {/each}
      </div>
      <p class="library-results-count">Showing {libraryView.from}–{libraryView.to} of {libraryView.total}</p>
     {:else}
      <div class="movie-library-empty">
       {#if search.trim()}
        <h2>No movies found</h2>
        <p>Try a different title or year.</p>
        <button type="button" onclick={()=>{search='';libraryPage=1}}>Clear search</button>
       {:else}
        <h2>No movies yet</h2>
        <p>Your completed movie downloads will appear here.</p>
        <button type="button" onclick={()=>navigate('downloader')}>Open Downloader</button>
       {/if}
      </div>
     {/if}
    </section>
   {:else if page==='settings'}
    <div class="dashboard-content settings-page">
     <div class="page-title">
      <div class="eyebrow"><span class="red-dot"></span> CLOUDFLIX CONFIGURATION</div>
      <h1>Settings</h1>
      <p>Manage integrations and your server preferences.</p>
     </div>
     <section class="settings-panel">
      <div class="settings-panel-header">
       <div class="settings-panel-icon"><Film size={23}/></div>
       <div class="settings-panel-intro">
        <h2>TMDB Movie Metadata</h2>
        <p>Automatically find movie titles, posters, background artwork, and release years.</p>
       </div>
       <span class:configured={tmdbConfigured} class="integration-status">{settingsLoading?'Checking...':tmdbConfigured?'● Connected':'○ Not configured'}</span>
      </div>
      <form class="settings-form" onsubmit={(event)=>{event.preventDefault();saveTMDB()}}>
       <label for="tmdb-token">TMDB API key or Read Access Token</label>
       <input id="tmdb-token" type="password" bind:value={tmdbToken} autocomplete="off" autocapitalize="off" spellcheck="false" maxlength="2048" placeholder={tmdbConfigured?'Enter a new token to replace the saved one':'Paste your TMDB API key or Read Access Token'} />
       <p class="settings-hint">Your token is checked directly with TMDB and stored privately on this VPS. For security, the saved token is never displayed.</p>
       <div class="settings-actions">
        <button type="submit" class="primary" disabled={settingsBusy||!tmdbToken.trim()}>{#if settingsBusy}<LoaderCircle size={17}/>{:else}<CheckCircle2 size={17}/>{/if} Save & verify token</button>
        {#if tmdbConfigured}<button type="button" class="settings-remove" disabled={settingsBusy} onclick={clearTMDB}><Trash2 size={17}/> Remove token</button>{/if}
       </div>
       <p class="settings-help">Get your token from <a href="https://www.themoviedb.org/settings/api" target="_blank" rel="noopener noreferrer">TMDB account settings</a>. Both the <strong>32-character API key</strong> and the <strong>API Read Access Token</strong> are supported.</p>
      </form>
     </section>
     <section class="settings-panel update-settings-panel" aria-label="Software updates">
      <div class="settings-panel-header">
       <div class="settings-panel-icon"><RefreshCw size={23}/></div>
       <div class="settings-panel-intro">
        <h2>Software Updates</h2>
        <p>Keep Cloudflix current with verified binaries from GitHub Releases. Your downloads and PIN stay intact.</p>
       </div>
       {#if updateInfo?.updateAvailable}
        <span class="integration-status update-available">● Update available</span>
       {:else if updateInfo && !updateInfo.checkError}
        <span class="integration-status configured">● Up to date</span>
       {/if}
      </div>
      <div class="update-versions">
       <div><span>Installed version</span><strong>{updateInfo?.currentVersion||version}</strong></div>
       <div><span>Latest release</span><strong>{updateChecking?'Checking…':updateInfo?.latestVersion||'—'}</strong></div>
      </div>
      {#if updateInfo?.state==='running'||updateInfo?.state==='queued'}
       <div class="update-progress" role="status"><LoaderCircle size={18} class="spin"/><div><strong>Updating Cloudflix</strong><p>{updateInfo?.message||'Installing the latest release…'} This page will reconnect when the service restarts.</p></div></div>
      {:else if updateInfo?.state==='success'&&!updateInfo?.updateAvailable}
       <p class="update-status-good"><CheckCircle2 size={16}/> Your update finished successfully.</p>
      {:else if updateInfo?.state==='error'}
       <p class="update-status-error"><AlertCircle size={16}/>{updateInfo.message}</p>
      {/if}
      {#if updateInfo?.checkError}
       <p class="settings-hint">{updateInfo.checkError}</p>
      {/if}
      {#if updateError}<p class="update-status-error" role="alert"><AlertCircle size={16}/>{updateError}</p>{/if}
      {#if updateInfo&&!updateInfo.updaterReady}
       <p class="settings-warning"><AlertCircle size={16}/> The web updater isn't installed. Run the one-line update command from your VPS terminal once to enable this feature.</p>
      {/if}
      <div class="settings-actions update-buttons">
       <button type="button" class="settings-update-check" disabled={updateChecking||updateBusy} onclick={()=>loadUpdateInfo()}><RefreshCw size={17}/> {updateChecking?'Checking…':'Check for updates'}</button>
       {#if updateInfo?.updateAvailable}
        <button type="button" class="primary" disabled={updateBusy||!updateInfo.updaterReady||updateInfo.state==='running'||updateInfo.state==='queued'} onclick={startUpdate}>
         {#if updateBusy}<LoaderCircle size={17} class="spin"/>{:else}<ArrowUpCircle size={17}/>{/if}
         Update to {updateInfo.latestVersion}
        </button>
       {/if}
      </div>
      <p class="settings-hint">Updates run through a restricted, root-owned systemd helper. The web app cannot run arbitrary commands.</p>
     </section>
     <section class="settings-panel">
      <div class="settings-panel-header">
       <div class="settings-panel-icon"><HardDrive size={23}/></div>
       <div class="settings-panel-intro">
        <h2>Server Access</h2>
        <p>Access Cloudflix directly using your VPS IP address on default HTTP port 80.</p>
       </div>
      </div>
      <div class="settings-info">
       <span>Current server</span><strong>{typeof window!=='undefined'?window.location.host :'VPS_IP'}</strong>
      </div>
      <p class="settings-warning"><AlertCircle size={16}/> Direct HTTP access is unencrypted. On an untrusted network your PIN, session cookies, and any TMDB token submitted here may be intercepted. Restrict port 80 to trusted IP addresses where possible.</p>
     </section>
    </div>
   {:else}
    <div class="dashboard-content download-page">
     <div class="downloader-heading">
      <div>
       <h1>Downloads</h1>
       <p>Add movies and track their progress.</p>
      </div>
      <span class="downloader-online"><span class:online={connected} class="status-dot"></span>{connected?'Connected':'Reconnecting'}</span>
     </div>
     <div class="stat-grid downloader-stats">
      <div class="stat"><div class="stat-icon red"><Clapperboard size={19}/></div><div><span>All downloads</span><strong>{movies.length}</strong></div></div>
      <div class="stat"><div class="stat-icon blue"><Download size={19}/></div><div><span>Downloading</span><strong>{active}</strong></div></div>
      <div class="stat"><div class="stat-icon green"><CheckCircle2 size={19}/></div><div><span>Completed</span><strong>{completed}</strong></div></div>
      <div class="stat"><div class="stat-icon purple"><HardDrive size={19}/></div><div><span>Downloaded</span><strong>{formatBytes(movies.reduce((a,m)=>a+m.completed,0))}</strong></div></div>
     </div>
     {#if tmdbChecked&&!tmdbConfigured&&!settingsLoading}
      <div class="tmdb-required" role="alert">
       <AlertCircle size={20}/>
       <div><strong>TMDB API key required</strong><p>Set your TMDB API key in Settings before downloading a movie.</p></div>
       <button type="button" onclick={()=>navigate('settings')}><Settings size={16}/> Open Settings</button>
      </div>
     {/if}
     <section class="downloader-magnet-panel" aria-label="Add a movie download">
      <form class="magnet-form downloader-magnet-form" onsubmit={(e)=>{e.preventDefault();void add()}}>
       <div class="magnet-panel-heading">
        <div><h2>Add a download</h2><p>Paste a magnet link to add a movie to your queue.</p></div>
        <span class="magnet-media-hint">MP4 + SRT only</span>
       </div>
       <label for="magnet">MAGNET LINK</label>
       <div class="magnet-entry-row">
        <textarea id="magnet" bind:value={magnet} placeholder="magnet:?xt=urn:btih:..." rows="2" required spellcheck="false"></textarea>
        <button class="primary magnet-add-button" type="submit" disabled={submitting||!magnet.trim()||(tmdbChecked&&!tmdbConfigured)}>
         {#if submitting}<LoaderCircle size={17} class="spin"/>{:else}<Plus size={18}/>{/if}
         Add download
        </button>
       </div>
       <p class="magnet-disclaimer">Only download content you're authorized to use.</p>
      </form>
     </section>
     <section class="download-queue-section" aria-label="Download queue">
      <div class="download-queue-toolbar">
       <div class="download-queue-heading">
        <h2>Queue <span>{movies.length}</span></h2>
        <p>{active} downloading · {completed} completed</p>
       </div>
       <div class="download-queue-filters" role="group" aria-label="Filter downloads">
        <button type="button" class:active={queueFilter==='all'} aria-pressed={queueFilter==='all'} onclick={()=>queueFilter='all'}>All</button>
        <button type="button" class:active={queueFilter==='active'} aria-pressed={queueFilter==='active'} onclick={()=>queueFilter='active'}>In progress</button>
        <button type="button" class:active={queueFilter==='completed'} aria-pressed={queueFilter==='completed'} onclick={()=>queueFilter='completed'}>Completed</button>
       </div>
      </div>
      {#if queueItems.length}
       <div class="download-list queue-list">
        {#each queueItems as m (m.hash)}
         <article class="download-item queue-item" class:download-rejected={m.status==='rejected'}>
          <div class="download-art queue-art">
           {#if m.poster}<img src={poster(m.poster)} alt="" loading="lazy"/>{:else}<Film size={28}/>{/if}
          </div>
          <div class="download-info queue-info">
           <div class="download-top queue-item-heading">
            <h3 title={m.title}>{m.title}</h3>
            <span class="status-pill" class:complete={m.status==='completed'} class:rejected={m.status==='rejected'}>{m.status==='metadata'?'Checking files':m.status==='completed'?'Completed':m.status}</span>
           </div>
           {#if m.status==='rejected'}
            <p class="torrent-rejection"><AlertCircle size={14}/> {m.error||'Only MP4 movies and SRT subtitles are supported.'}</p>
           {:else}
            <div class="queue-file-info">
             {#if m.year}<span>{m.year}</span><span class="queue-meta-sep">·</span>{/if}
             <span>{formatBytes(m.completed)} / {formatBytes(m.size)}</span>
             {#if m.status==='completed'}<span class="queue-ready"><CheckCircle2 size={13}/> Ready to watch</span>{/if}
            </div>
            {#if m.status==='downloading'||m.status==='paused'}
             <div class="queue-progress">
              <div class="progress-track" role="progressbar" aria-label={m.title+' download progress'} aria-valuenow={percent(m)} aria-valuemin="0" aria-valuemax="100"><div class="progress-fill" style:width={percent(m)+'%'}></div></div>
              <span>{percent(m).toFixed(1)}%</span>
             </div>
            {:else if m.status==='metadata'}
             <p class="queue-processing">Inspecting torrent metadata before downloading…</p>
            {/if}
           {/if}
          </div>
          <div class="download-actions queue-actions">
           {#if m.status==='paused'}
            <button type="button" title="Resume" aria-label={'Resume '+m.title} onclick={()=>action(m,'resume')}><Play size={18}/></button>
           {:else if m.status==='downloading'}
            <button type="button" title="Pause" aria-label={'Pause '+m.title} onclick={()=>action(m,'pause')}><Pause size={18}/></button>
           {/if}
           {#if m.status==='completed'}
            <button type="button" class="queue-watch-button" title={'Watch '+m.title} onclick={()=>openMovie(m)}><MonitorPlay size={17}/><span>Watch</span></button>
           {/if}
           <button type="button" class="danger" title={'Remove '+m.title+' from queue'} aria-label={'Remove '+m.title} onclick={()=>action(m,'remove')}><Trash2 size={17}/></button>
          </div>
         </article>
        {/each}
       </div>
      {:else}
       <div class="queue-empty">
        <Download size={25}/>
        <h3>{movies.length?'No downloads in this view':'Your queue is empty'}</h3>
        <p>{movies.length?'Try a different filter.':'Add a magnet link above to start downloading.'}</p>
        {#if movies.length}<button type="button" onclick={()=>queueFilter='all'}>Show all downloads</button>{/if}
       </div>
      {/if}
     </section>
    </div>
   {/if}
   <footer class="footer"><span>© Cloudflix · Made with ❤️ by Kobz</span><span>Movie data from <a href="https://www.themoviedb.org/" target="_blank" rel="noopener noreferrer">TMDB</a>; not endorsed or certified by TMDB.</span></footer>
  </main>
  <nav class="mobile-nav"><button class:active={page==='home'} onclick={()=>navigate('home')}><House size={20}/> Home</button><button class:active={page==='downloader'} onclick={()=>navigate('downloader')}><Download size={20}/> Downloader {#if active>0}<span class="mobile-download-count" aria-label={active+' active downloads'}>{active}</span>{/if}</button><button class:active={page==='settings'} onclick={()=>navigate('settings')}><Settings size={20}/> Settings</button><button onclick={signOut}><LogOut size={20}/> Log out</button></nav>
 </div>
 {#if selected}
  <div class="modal-backdrop cinema-player-backdrop" role="presentation">
   <div class="cinema-player-modal" role="dialog" aria-modal="true" aria-label={'Watch '+selected.title}>
    <header class="cinema-player-heading">
     <div class="player-title-group">
      <h2>{selected.title}</h2>
      <p>{selected.year||'Movie'} {#if selected.rating>0}<span aria-hidden="true">·</span><Star size={13} fill="currentColor"/> {selected.rating.toFixed(1)} / 10{/if}</p>
     </div>
     <button class="cinema-player-close" onclick={closeMovie} aria-label="Close player" title="Close"><X size={20}/></button>
    </header>
    {#if selected.status==='completed' && selectedPlayback.video}
     <div class="cinema-video-frame">
      {#key selected.hash+selectedPlayback.video.path}
       <video
        bind:this={playerVideo}
        class="cinema-video"
        controls
        autoplay
        playsinline
        preload="metadata"
        onloadedmetadata={syncSubtitleTracks}
        poster={selected.backdrop?backdrop(selected.backdrop):selected.poster?poster(selected.poster):undefined}
        onerror={()=>playbackError='Your browser cannot play this MP4 file. Try downloading it or use a browser that supports its video codec.'}
        aria-label={'Playback of '+selected.title}
       >
        <source src={mediaURL(selected.hash,selectedPlayback.video.path)} type="video/mp4"/>
        {#each selectedPlayback.subtitles as sub,i (sub.path)}
         <track kind="subtitles" src={mediaURL(selected.hash,sub.path)} srclang={sub.language} label={sub.label} onload={syncSubtitleTracks}/>
        {/each}
        Your browser does not support HTML5 video.
       </video>
      {/key}
     </div>
     {#if playbackError}<p class="player-error" role="alert"><AlertCircle size={16}/>{playbackError}</p>{/if}
     <div class="cinema-player-toolbar">
      <div class="cinema-player-hint">
       <span>{formatBytes(selectedPlayback.video.size)}</span>
       {#if selectedPlayback.subtitles.length}
        <span class="player-toolbar-sep" aria-hidden="true">·</span>
        <span>{selectedPlayback.subtitles.length} {selectedPlayback.subtitles.length===1?'subtitle':'subtitles'} available</span>
       {/if}
      </div>
      <div class="cinema-player-actions">
       {#if selectedPlayback.subtitles.length}
        <button
         class="cinema-cc-button"
         class:active={ccEnabled}
         type="button"
         aria-label={ccEnabled?'Disable captions':'Enable captions'}
         aria-pressed={ccEnabled}
         title={ccEnabled?'Turn captions off':'Turn captions on'}
         onclick={toggleCaptions}
        >CC</button>
        {#if ccEnabled}
         <div class="cinema-caption-languages" role="group" aria-label="Available subtitles">
          {#each selectedPlayback.subtitles as sub,i (sub.path)}
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
       <button class="cinema-fullscreen-button" type="button" onclick={enterFullscreen}><Maximize2 size={17}/> Fullscreen</button>
      </div>
     </div>
    {:else}
     <div class="cinema-player-unavailable">
      <MonitorPlay size={40} strokeWidth={1.2}/>
      <h3>{selected.status==='rejected'?'Torrent was rejected':'Movie not ready yet'}</h3>
      <p>{selected.error||'Playback is available when the MP4 download finishes.'}</p>
     </div>
    {/if}
   </div>
  </div>
 {/if}
{/if}
