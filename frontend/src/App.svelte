<script lang="ts">
 import { onMount } from 'svelte'
 import { selectLibraryPage, countDownloading } from './lib/library.js'
 import {findPlayback,mediaURL} from './lib/media.js'
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
 let selected = $state<Movie|null>(null)
 let playerVideo = $state<HTMLVideoElement|null>(null)
 let playbackError = $state('')
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
 const openMovie=(movie:Movie)=>{
   playbackError=''
   playerVideo=null
   selected=movie
 }
 const closeMovie=()=>{
   playerVideo?.pause()
   playerVideo=null
   selected=null
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
 const setState=(state:{movies:Movie[],version?:string})=>{movies=state.movies||[];if(state.version)version=state.version;if(selected)selected=movies.find(m=>m.hash===selected?.hash)||null}
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
    <div class="top-caption">{page==='home'?'Movies':page==='settings'?'Settings':'Downloader'}</div>
    <div class="top-right"><span class="version">{version}</span><div class="avatar">C</div></div>
   </header>
   {#if notice}<div class="toast success"><CheckCircle2 size={16}/>{notice}<button onclick={()=>notice=''} aria-label="Dismiss"><X size={16}/></button></div>{/if}
   {#if error}<div class="toast error"><AlertCircle size={16}/>{error}<button onclick={()=>error=''} aria-label="Dismiss"><X size={16}/></button></div>{/if}
   {#if page==='home'}
    <section class="film-library" aria-label="Your downloaded movie library">
     <div class="library-masthead">
      <div class="library-intro">
       <span class="library-kicker"><span class="kicker-mark"></span> PRIVATE SCREENING ROOM <span class="kicker-index">/ YOUR COLLECTION</span></span>
       <h1>The collection<span class="title-period">.</span></h1>
       <p>{libraryView.total} {libraryView.total===1?'film':'films'} ready for your next movie night.</p>
      </div>
      <div class="library-controls">
       <label class="cinema-search">
        <Search size={18}/>
        <input value={search} oninput={(event)=>{search=event.currentTarget.value;libraryPage=1}} placeholder="Find a movie or year" aria-label="Search downloaded movies"/>
        {#if search.trim()}<button type="button" aria-label="Clear search" onclick={()=>{search='';libraryPage=1}}><X size={15}/></button>{/if}
       </label>
      </div>
     </div>
     <div class="shelf-divider">
      <span><span class="shelf-light"></span> ALL FILMS</span>
      <div class="shelf-line"></div>
      <span class="shelf-count">{String(libraryView.total).padStart(2,'0')} TITLES</span>
     </div>
     {#if libraryView.movies.length}
      <div class="cinema-wall">
       {#each libraryView.movies as movie,i (movie.hash)}
        <button class="cinema-tile" type="button" title={'Play '+movie.title} onclick={()=>openMovie(movie)}>
         <div class="cinema-poster">
          {#if movie.poster}
           <img src={poster(movie.poster)} alt={'Poster for '+movie.title} loading="lazy"/>
          {:else}
           <div class="cinema-art-placeholder" aria-hidden="true"><FilmClap size={43} strokeWidth={1.1}/><span>CLOUDFLIX</span></div>
          {/if}
          <span class="film-corner-number">{String((libraryView.page-1)*10+i+1).padStart(2,'0')}</span>
          <span class="film-poster-rating"><Star size={13} fill={movie.rating>0?'currentColor':'none'} /> {movie.rating>0?movie.rating.toFixed(1):'N/A'}</span>
          <span class="film-play-ring" aria-hidden="true"><Play size={23} fill="currentColor" strokeWidth={1.2}/></span>
          <span class="film-watch-label">PLAY MOVIE <span aria-hidden="true">↗</span></span>
         </div>
         <div class="cinema-tile-copy">
          <h3>{movie.title}</h3>
          <span>{movie.year||'YEAR UNKNOWN'}</span>
         </div>
        </button>
       {/each}
      </div>
      <nav class="library-pagination cinema-pagination" aria-label="Movie pages">
       <span class="pagination-summary">{String(libraryView.from).padStart(2,'0')}—{String(libraryView.to).padStart(2,'0')} <span class="pagination-slash">/</span> {String(libraryView.total).padStart(2,'0')} FILMS</span>
       {#if libraryView.totalPages>1}
        <div class="pagination-buttons">
         <button type="button" aria-label="Previous page" disabled={libraryView.page===1} onclick={()=>libraryPage=libraryView.page-1}><ChevronLeft size={17}/></button>
         {#each libraryView.pages as number}
          <button type="button" class:current={libraryView.page===number} aria-label={'Page '+number} aria-current={libraryView.page===number?'page':undefined} onclick={()=>libraryPage=number}>{number}</button>
         {/each}
         <button type="button" aria-label="Next page" disabled={libraryView.page===libraryView.totalPages} onclick={()=>libraryPage=libraryView.page+1}><ChevronRight size={17}/></button>
        </div>
       {/if}
      </nav>
     {:else}
      <div class="cinema-blank">
       <div class="cinema-blank-art" aria-hidden="true">
        <div class="blank-film blank-film-back"></div>
        <div class="blank-film blank-film-front"><FilmClap size={42} strokeWidth={1.1}/></div>
       </div>
       <div class="cinema-blank-copy">
        <span class="library-kicker">THE SCREEN IS WAITING</span>
        <h2>{search.trim()?'No films found.':'Every great collection starts with a film.'}</h2>
        <p>{search.trim()?'Try a different movie title or release year.':'Your completed MP4 downloads will find their home here, with artwork, ratings and subtitles.'}</p>
        {#if search.trim()}
         <button class="cinema-empty-button" onclick={()=>{search='';libraryPage=1}}>Clear search <span aria-hidden="true">↗</span></button>
        {:else}
         <button class="cinema-empty-button" onclick={()=>navigate('downloader')}>Add a movie <span aria-hidden="true">↗</span></button>
        {/if}
       </div>
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
     <div class="stat-grid downloader-stats">
      <div class="stat"><div class="stat-icon red"><Clapperboard size={20}/></div><div><span>Total downloads</span><strong>{movies.length}</strong></div></div>
      <div class="stat"><div class="stat-icon blue"><Download size={20}/></div><div><span>Downloading</span><strong>{active}</strong></div></div>
      <div class="stat"><div class="stat-icon green"><CheckCircle2 size={20}/></div><div><span>Completed</span><strong>{completed}</strong></div></div>
      <div class="stat"><div class="stat-icon purple"><HardDrive size={20}/></div><div><span>Downloaded</span><strong>{formatBytes(movies.reduce((a,m)=>a+m.completed,0))}</strong></div></div>
     </div>
     <div class="page-title"><div><div class="eyebrow"><span class="red-dot"></span> DOWNLOAD MANAGER</div><h1>Downloader</h1><p>Add magnet links and manage your downloads from anywhere.</p></div></div>
     {#if tmdbChecked&&!tmdbConfigured&&!settingsLoading}
      <div class="tmdb-required" role="alert">
       <AlertCircle size={20}/>
       <div><strong>TMDB API key required</strong><p>Configure your TMDB API key in Settings before downloading a movie.</p></div>
       <button type="button" onclick={()=>navigate('settings')}><Settings size={16}/> Open Settings</button>
      </div>
     {/if}
     <form class="magnet-form" onsubmit={(e)=>{e.preventDefault();add()}}>
      <div class="form-head"><span class="form-icon"><Plus size={22}/></span><div><h2>Add a new download</h2><p>Paste a magnet URI to start downloading to your VPS.</p></div></div>
      <label for="magnet">MAGNET URI</label>
      <textarea id="magnet" bind:value={magnet} placeholder="magnet:?xt=urn:btih:..." rows="3" required></textarea>
      <div class="form-footer"><span>Only download content you're authorized to use.</span><button class="primary" type="submit" disabled={submitting||!magnet.trim()||(tmdbChecked&&!tmdbConfigured)}>{#if submitting}<LoaderCircle size={17}/>{:else}<ArrowDownToLine size={17}/>{/if} Start Download</button></div>
     </form>
     <div class="section-heading"><div><h2>Download Queue</h2><p>{movies.length} total · {active} active · {completed} completed</p></div></div>
     {#if movies.length}
      <div class="download-list">
      {#each movies as m (m.hash)}
       <article class="download-item" class:download-rejected={m.status==='rejected'}>
        <div class="download-art">{#if m.poster}<img src={poster(m.poster)} alt="" />{:else}<Film size={30}/>{/if}</div>
        <div class="download-info">
         <div class="download-top"><h3>{m.title}</h3><span class:complete={m.status==='completed'} class:rejected={m.status==='rejected'} class="status-pill">{m.status==='metadata'?'Checking files':m.status}</span></div>
         {#if m.status==='rejected'}
          <p class="torrent-rejection"><AlertCircle size={15}/> {m.error||'Torrent rejected. Only MP4 movies and SRT subtitles are supported.'}</p>
         {:else if m.status==='metadata'}
          <p>Inspecting torrent files before downloading…</p>
         {:else}
          <p>{formatBytes(m.completed)} / {formatBytes(m.size)} {m.year?'· '+m.year:''}</p>
          <div class="progress-track"><div class="progress-fill" style:width={percent(m)+'%'}></div></div>
          <div class="download-meta"><span>{percent(m).toFixed(1)}% complete</span><span>{m.files.length} selected .mp4 / .srt files</span></div>
         {/if}
        </div>
        <div class="download-actions">
         {#if m.status==='paused'}<button title="Resume" onclick={()=>action(m,'resume')}><Play size={18}/></button>{:else if m.status==='downloading'}<button title="Pause" onclick={()=>action(m,'pause')}><Pause size={18}/></button>{/if}
         {#if m.status!=='rejected'}<button title={m.status==='completed'?'Watch movie':'Movie files'} onclick={()=>openMovie(m)}><MonitorPlay size={18}/></button>{/if}
         <button class="danger" title="Remove from queue" onclick={()=>action(m,'remove')}><Trash2 size={18}/></button>
        </div>
       </article>
      {/each}
      </div>
     {:else}
      <div class="empty"><div class="empty-icon"><Download size={34}/></div><h3>No downloads yet</h3><p>Paste a magnet link above and your download will appear here.</p></div>
     {/if}
    </div>
   {/if}
   <footer class="footer"><span>© Cloudflix · Self-hosted with ❤️</span><span>Movie data from <a href="https://www.themoviedb.org/" target="_blank" rel="noopener noreferrer">TMDB</a>; not endorsed or certified by TMDB.</span></footer>
  </main>
  <nav class="mobile-nav"><button class:active={page==='home'} onclick={()=>navigate('home')}><House size={20}/> Home</button><button class:active={page==='downloader'} onclick={()=>navigate('downloader')}><Download size={20}/> Downloader {#if active>0}<span class="mobile-download-count" aria-label={active+' active downloads'}>{active}</span>{/if}</button><button class:active={page==='settings'} onclick={()=>navigate('settings')}><Settings size={20}/> Settings</button><button onclick={signOut}><LogOut size={20}/> Log out</button></nav>
 </div>
 {#if selected}
  <div class="modal-backdrop cinema-player-backdrop" role="presentation" onclick={(event)=>{if(event.target===event.currentTarget)closeMovie()}}>
   <div class="cinema-player-modal" role="dialog" aria-modal="true" aria-label={'Watch '+selected.title}>
    <div class="cinema-player-heading">
     <div class="player-title-group">
      <span class="library-kicker"><span class="kicker-mark"></span> NOW SHOWING</span>
      <h2>{selected.title}</h2>
      <p>{selected.year||'Film'} {#if selected.rating>0}<span>·</span> <Star size={13} fill="currentColor"/> {selected.rating.toFixed(1)} / 10{/if}</p>
     </div>
     <button class="cinema-player-close" onclick={closeMovie} aria-label="Close player"><X size={23}/></button>
    </div>
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
        poster={selected.backdrop?backdrop(selected.backdrop):selected.poster?poster(selected.poster):undefined}
        onerror={()=>playbackError='Your browser cannot play this MP4 file. Try downloading it or use a browser that supports its video codec.'}
        aria-label={'Playback of '+selected.title}
       >
        <source src={mediaURL(selected.hash,selectedPlayback.video.path)} type="video/mp4"/>
        {#each selectedPlayback.subtitles as sub,i (sub.path)}
         <track kind="subtitles" src={mediaURL(selected.hash,sub.path)} srclang={sub.language} label={sub.label} default={i===0}/>
        {/each}
        Your browser does not support HTML5 video.
       </video>
      {/key}
     </div>
     {#if playbackError}<p class="player-error"><AlertCircle size={16}/>{playbackError}</p>{/if}
     <div class="cinema-player-toolbar">
      <div class="cinema-player-hint"><span class="player-ready-dot"></span> READY TO WATCH <span class="player-toolbar-sep">/</span> {formatBytes(selectedPlayback.video.size)}
       {#if selectedPlayback.subtitles.length}<span class="player-toolbar-sep">/</span> {selectedPlayback.subtitles.length} SUBTITLE {selectedPlayback.subtitles.length===1?'TRACK':'TRACKS'}{/if}
      </div>
      <div class="cinema-player-actions">
       <a href={'/api/files/'+encodeURIComponent(selected.hash)+'?path='+encodeURIComponent(selectedPlayback.video.path)} title="Download MP4 video"><ArrowDownToLine size={16}/> Download MP4</a>
       <button onclick={enterFullscreen} title="Enter fullscreen"><Maximize2 size={17}/> Fullscreen</button>
      </div>
     </div>
    {:else}
     <div class="cinema-player-unavailable">
      <MonitorPlay size={40} strokeWidth={1.2}/>
      <h3>{selected.status==='rejected'?'Torrent was rejected':'Your film is almost ready.'}</h3>
      <p>{selected.error||'Playback becomes available once the selected MP4 and subtitle files finish downloading.'}</p>
     </div>
    {/if}
    {#if selected.files.length}
     <div class="cinema-player-files">
      <div class="cinema-player-files-title"><span>FILES ON YOUR SERVER</span><span>{selected.files.length} FILES</span></div>
      {#each selected.files as file (file.path)}
       <div class="cinema-player-file">
        <span class="file-name">{file.path}</span>
        {#if file.completed!==undefined && file.completed<file.size}
         <span class="muted">{formatBytes(file.completed)} / {formatBytes(file.size)}</span>
        {:else}
         <a href={'/api/files/'+encodeURIComponent(selected.hash)+'?path='+encodeURIComponent(file.path)} title={'Download '+file.path}><ArrowDownToLine size={15}/><span>{formatBytes(file.size)}</span></a>
        {/if}
       </div>
      {/each}
     </div>
    {/if}
   </div>
  </div>
 {/if}
{/if}
