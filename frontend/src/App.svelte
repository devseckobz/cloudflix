<script lang="ts">
 import { onMount } from 'svelte'
 import { Film, House, Download, Search, ArrowDownToLine, Pause, Play, Trash2, HardDrive, LogOut, Plus, X, CheckCircle2, LoaderCircle, Shield, Clapperboard, AlertCircle, ExternalLink } from 'lucide-svelte'

 type Movie = {hash:string,title:string,year:string,poster:string,backdrop:string,size:number,completed:number,progress:number,status:string,files:{path:string,size:number}[]}
 let page = $state<'home'|'downloader'>('home')
 let auth = $state<'loading'|'setup'|'login'|'ready'>('loading')
 let password = $state('')
 let movies = $state<Movie[]>([])
 let error = $state('')
 let notice = $state('')
 let magnet = $state('')
 let submitting = $state(false)
 let search = $state('')
 let filter = $state<'all'|'active'|'complete'>('all')
 let selected = $state<Movie|null>(null)
 let connected = $state(false)
 let version = $state('v0.1.0-alpha')
 let live:EventSource|null = null
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
 const active = $derived(movies.filter(m=>m.status==='downloading'||m.status==='metadata').length)
 const filtered = $derived(movies.filter(m => (filter==='all'||(filter==='active'&&m.status!=='completed'&&m.status!=='paused')||(filter==='complete'&&m.status==='completed')) && (m.title.toLowerCase().includes(search.toLowerCase())||m.year.includes(search))))
 const featured = $derived(movies.find(m=>m.backdrop)||movies[0])
 const call = async (path:string,method='GET',data?:object)=>{
   const response=await fetch('/api/'+path,{method,credentials:'same-origin',headers:data?{'Content-Type':'application/json'}:{},body:data?JSON.stringify(data):undefined})
   const result=await response.json()
   if(!response.ok) throw new Error(result.error||'Request failed')
   return result
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
     if(auth==='ready'){await refresh();connect()}
   }catch{error='Unable to connect to Cloudflix server';auth='login'}
 }
 onMount(()=>{checkAuth();return ()=>{live?.close()}})
 const authenticate=async()=>{
   error=''
   if(auth==='setup'&&password.length<12){error='Choose a password of at least 12 characters';return}
   submitting=true
   try{
     await call(auth==='setup'?'auth/setup':'auth/login','POST',{password})
     password='';auth='ready';await refresh();connect()
   }catch(e){error=(e as Error).message}
   finally{submitting=false}
 }
 const signOut=async()=>{try{await call('auth/logout','POST',{})}catch{}live?.close();connected=false;auth='login';movies=[]}
 const add=async()=>{
   error='';notice='';submitting=true
   try{
     await call('magnets','POST',{magnet:magnet.trim()})
     magnet='';notice='Magnet added to your download queue';page='downloader';await refresh()
   }catch(e){error=(e as Error).message}
   finally{submitting=false}
 }
 const action=async(m:Movie,verb:'pause'|'resume'|'remove')=>{
   if(verb==='remove'&&!confirm('Remove this torrent from Cloudflix? Downloaded files are retained.'))return
   error='';notice=''
   try{await call('torrents/'+encodeURIComponent(m.hash)+'/'+verb,'POST',{});await refresh()}
   catch(e){error=(e as Error).message}
 }
</script>

<svelte:head>
 <title>{page==='home'?'Home':'Downloader'} · Cloudflix</title>
</svelte:head>

{#if auth==='loading'}
 <div class="auth-screen"><div class="brand mark"><Film size={42}/><strong>cloud<span>flix</span></strong></div><span class="muted">Connecting to your server...</span></div>
{:else if auth==='login'||auth==='setup'}
 <div class="auth-screen">
  <div class="auth-glow"></div>
  <form class="auth-card" onsubmit={(e)=>{e.preventDefault();authenticate()}}>
   <div class="brand"><span class="brand-icon"><Film size={28}/></span><strong>cloud<span>flix</span></strong></div>
   <div class="auth-shield"><Shield size={28}/></div>
   <h1>{auth==='setup'?'Welcome to Cloudflix':'Welcome back'}</h1>
   <p>{auth==='setup'?'Create your administrator password to secure your personal movie library.':'Log in to access your private movie library.'}</p>
   <label for="password">Administrator password</label>
   <input id="password" type="password" minlength={auth==='setup'?12:1} required autocomplete={auth==='setup'?'new-password':'current-password'} bind:value={password} placeholder={auth==='setup'?'At least 12 characters':'Enter password'}/>
   {#if error}<div class="alert"><AlertCircle size={16}/>{error}</div>{/if}
   <button class="primary wide" disabled={submitting} type="submit">{auth==='setup'?'Create administrator':'Log in'} <span>→</span></button>
   <small>Your media, credentials and download history stay on your own server.</small>
  </form>
 </div>
{:else}
 <div class="app-shell">
  <aside class="sidebar">
   <button class="brand brand-button" onclick={()=>page='home'} aria-label="Cloudflix homepage"><span class="brand-icon"><Film size={25}/></span><strong>cloud<span>flix</span></strong></button>
   <div class="side-label">MENU</div>
   <nav aria-label="Main navigation">
    <button class:active={page==='home'} onclick={()=>page='home'}><House size={19}/> Home</button>
    <button class:active={page==='downloader'} onclick={()=>page='downloader'}><Download size={19}/> Downloader {#if active}<span class="nav-count">{active}</span>{/if}</button>
   </nav>
   <div class="side-label library-label">YOUR LIBRARY</div>
   <nav>
    <button onclick={()=>{page='home';filter='all'}}><Clapperboard size={18}/> All movies <span class="nav-count">{movies.length}</span></button>
    <button onclick={()=>{page='home';filter='complete'}}><CheckCircle2 size={18}/> Completed</button>
    <button onclick={()=>{page='home';filter='active'}}><LoaderCircle size={18}/> In progress</button>
   </nav>
   <div class="sidebar-bottom">
    <div class="server-status"><span class:online={connected} class="status-dot"></span><span>{connected?'Server connected':'Reconnecting...'}</span></div>
    <button onclick={signOut} class="signout"><LogOut size={18}/> Sign out</button>
   </div>
  </aside>
  <main class="main">
   <header class="topbar">
    <div class="mobile-brand brand"><span class="brand-icon"><Film size={19}/></span><strong>cloud<span>flix</span></strong></div>
    <div class="top-caption">{page==='home'?'Your personal cinema':'Manage your downloads'}</div>
    <div class="top-right"><span class="version">{version}</span><div class="avatar">C</div></div>
   </header>
   {#if notice}<div class="toast success"><CheckCircle2 size={16}/>{notice}<button onclick={()=>notice=''} aria-label="Dismiss"><X size={16}/></button></div>{/if}
   {#if error}<div class="toast error"><AlertCircle size={16}/>{error}<button onclick={()=>error=''} aria-label="Dismiss"><X size={16}/></button></div>{/if}
   {#if page==='home'}
    <section class="hero" style:background-image={featured?.backdrop? 'linear-gradient(90deg,#111925 0%,rgba(17,25,37,.92) 32%,rgba(17,25,37,.28) 100%),url('+backdrop(featured.backdrop)+')':''}>
     <div class="eyebrow"><span class="red-dot"></span> YOUR PERSONAL MOVIE HUB</div>
     {#if featured}
      <div class="hero-copy"><h1>{featured.title}</h1><p>{featured.year||'Movie'} · {featured.status==='completed'?'Ready to download':'In your library'}</p><div class="hero-actions"><button class="primary" onclick={()=>selected=featured}><Play size={18} fill="currentColor"/> View details</button><button class="outline" onclick={()=>page='downloader'}><Download size={18}/> Downloads</button></div></div>
     {:else}
      <div class="hero-copy"><h1>All your movies.<br/><em>One beautiful place.</em></h1><p>Add a magnet link to get started. Cloudflix organizes your downloads and finds their movie artwork automatically.</p><div class="hero-actions"><button class="primary" onclick={()=>page='downloader'}><Plus size={18}/> Add your first movie</button></div></div>
     {/if}
     <div class="hero-vignette"></div>
    </section>
    <div class="dashboard-content">
     <div class="stat-grid">
      <div class="stat"><div class="stat-icon red"><Clapperboard size={20}/></div><div><span>Total movies</span><strong>{movies.length}</strong></div></div>
      <div class="stat"><div class="stat-icon blue"><Download size={20}/></div><div><span>Downloading</span><strong>{active}</strong></div></div>
      <div class="stat"><div class="stat-icon green"><CheckCircle2 size={20}/></div><div><span>Completed</span><strong>{completed}</strong></div></div>
      <div class="stat"><div class="stat-icon purple"><HardDrive size={20}/></div><div><span>Downloaded</span><strong>{formatBytes(movies.reduce((a,m)=>a+m.completed,0))}</strong></div></div>
     </div>
     <div class="section-heading"><div><h2>My Movie Library</h2><p>Browse and manage your collection</p></div><div class="filter-row"><div class="searchbox"><Search size={17}/><input bind:value={search} placeholder="Search movies" aria-label="Search movies"/></div></div></div>
     <div class="tabs"><button class:chosen={filter==='all'} onclick={()=>filter='all'}>All Movies</button><button class:chosen={filter==='active'} onclick={()=>filter='active'}>Downloading</button><button class:chosen={filter==='complete'} onclick={()=>filter='complete'}>Completed</button></div>
     {#if filtered.length}
      <div class="movie-grid">{#each filtered as m (m.hash)}<button class="movie-card" onclick={()=>selected=m}>
       <div class="poster">{#if m.poster}<img src={poster(m.poster)} alt={'Poster for '+m.title} loading="lazy"/>{:else}<div class="poster-fallback"><Film size={45}/><small>NO POSTER YET</small></div>{/if}<span class:done={m.status==='completed'} class="movie-status">{m.status==='completed'?'✓ Completed':m.status==='paused'?'Paused':m.status==='metadata'?'Loading':'↓ '+percent(m).toFixed(0)+'%'}</span></div>
       <h3>{m.title}</h3><p>{m.year||'Unknown year'} · {formatBytes(m.size)}</p>
      </button>{/each}</div>
     {:else}
      <div class="empty"><div class="empty-icon"><Film size={34}/></div><h3>{movies.length?'No matching movies':'Your collection starts here'}</h3><p>{movies.length?'Try a different search or filter.':'Movies you download will appear here with posters and artwork.'}</p><button class="primary" onclick={()=>{page='downloader';filter='all'}}><Plus size={17}/> Open downloader</button></div>
     {/if}
    </div>
   {:else}
    <div class="dashboard-content download-page">
     <div class="page-title"><div><div class="eyebrow"><span class="red-dot"></span> DOWNLOAD MANAGER</div><h1>Downloader</h1><p>Add magnet links and manage your downloads from anywhere.</p></div></div>
     <form class="magnet-form" onsubmit={(e)=>{e.preventDefault();add()}}>
      <div class="form-head"><span class="form-icon"><Plus size={22}/></span><div><h2>Add a new download</h2><p>Paste a magnet URI to start downloading to your VPS.</p></div></div>
      <label for="magnet">MAGNET URI</label>
      <textarea id="magnet" bind:value={magnet} placeholder="magnet:?xt=urn:btih:..." rows="3" required></textarea>
      <div class="form-footer"><span>Only download content you're authorized to use.</span><button class="primary" type="submit" disabled={submitting||!magnet.trim()}>{#if submitting}<LoaderCircle size={17}/>{:else}<ArrowDownToLine size={17}/>{/if} Start Download</button></div>
     </form>
     <div class="section-heading"><div><h2>Download Queue</h2><p>{movies.length} total · {active} active · {completed} completed</p></div></div>
     {#if movies.length}
      <div class="download-list">
      {#each movies as m (m.hash)}
       <article class="download-item"><div class="download-art">{#if m.poster}<img src={poster(m.poster)} alt="" />{:else}<Film size={30}/>{/if}</div><div class="download-info"><div class="download-top"><h3>{m.title}</h3><span class:complete={m.status==='completed'} class="status-pill">{m.status}</span></div><p>{formatBytes(m.completed)} / {formatBytes(m.size)} {m.year?'· '+m.year:''}</p><div class="progress-track"><div class="progress-fill" style:width={percent(m)+'%'}></div></div><div class="download-meta"><span>{percent(m).toFixed(1)}% complete</span><span>{m.files.length} files</span></div></div>
       <div class="download-actions">{#if m.status==='paused'}<button title="Resume" onclick={()=>action(m,'resume')}><Play size={18}/></button>{:else if m.status!=='completed'}<button title="Pause" onclick={()=>action(m,'pause')}><Pause size={18}/></button>{/if}<button title="Files" onclick={()=>selected=m}><ExternalLink size={18}/></button><button class="danger" title="Remove" onclick={()=>action(m,'remove')}><Trash2 size={18}/></button></div></article>
      {/each}
      </div>
     {:else}
      <div class="empty"><div class="empty-icon"><Download size={34}/></div><h3>No downloads yet</h3><p>Paste a magnet link above and your download will appear here.</p></div>
     {/if}
    </div>
   {/if}
   <footer class="footer"><span>© Cloudflix · Self-hosted with ❤️</span><span>Powered by Go & Svelte</span></footer>
  </main>
  <nav class="mobile-nav"><button class:active={page==='home'} onclick={()=>page='home'}><House size={20}/> Home</button><button class:active={page==='downloader'} onclick={()=>page='downloader'}><Download size={20}/> Downloader</button><button onclick={signOut}><LogOut size={20}/> Log out</button></nav>
 </div>
 {#if selected}
  <div class="modal-backdrop" role="presentation" onclick={(event)=>{if(event.target===event.currentTarget)selected=null}}>
   <section class="modal" role="dialog" aria-modal="true" aria-label="Movie details">
    {#if selected.backdrop}<div class="modal-hero" style:background-image={'linear-gradient(0deg,#151e2b,transparent),url('+backdrop(selected.backdrop)+')'}></div>{/if}
    <button class="modal-close" onclick={()=>selected=null} aria-label="Close dialog"><X size={20}/></button>
    <div class="modal-content"><h2>{selected.title}</h2><p class="muted">{selected.year||'Movie'} · {formatBytes(selected.size)} · {selected.status}</p><div class="progress-track"><div class="progress-fill" style:width={percent(selected)+'%'}></div></div><p class="muted">{percent(selected).toFixed(1)}% downloaded</p><h3>Movie files</h3>
    {#if selected.files.length}<div class="file-list">{#each selected.files as file}<a href={'/api/files/'+encodeURIComponent(selected.hash)+'?path='+encodeURIComponent(file.path)}><span class="file-name">{file.path}</span><span>{formatBytes(file.size)} <ArrowDownToLine size={15}/></span></a>{/each}</div>{:else}<p class="muted">Waiting for torrent metadata...</p>{/if}
    </div>
   </section>
  </div>
 {/if}
{/if}
