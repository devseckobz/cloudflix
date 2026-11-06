package main

import (
 "context"
 "database/sql"
 "encoding/json"
 "errors"
 "fmt"
 "io"
 "net/http"
 "net/url"
 "os"
 "path/filepath"
 "regexp"
 "sort"
 "strconv"
 "strings"
 "sync"
 "time"

 "github.com/anacrolix/torrent"
 "github.com/devseckobz/cloudflix/internal/outbound"
)

type entry struct {
 hash string
 queuePosition int64
 magnet string
 torrent *torrent.Torrent
 paused bool
 title string
 year string
 genre string
 poster string
 backdrop string
 rating float64
 lookedUp bool
 attempts int
 retryAt time.Time
 validated bool
 rejectionReason string
 // Proof-of-completion cache. selectedFilesVerified walks every piece of every
 // selected file, which is far too expensive to repeat on the 2s SSE tick for
 // a large library. A positive result is remembered while the torrent still
 // reports every selected byte as downloaded; a negative one is retried no
 // more than once a second.
 verified bool
 verifiedAt time.Time
 // Per-file proof cache for the media and MKV handlers. A player issues dozens
 // of range requests per minute, and each used to re-walk every piece of the
 // requested file (~0.5 MB of allocations for a 20k-piece movie). A verified
 // file stays verified.
 verifiedFiles map[string]bool
 // Files whose background transcode has already been requested, so a download
 // that finished is prepared once rather than on every tick.
 prewarmed map[string]bool
 fileManifest *torrentFileManifest // transient torrent metadata; never stored in SQLite
 inspectingFiles bool
 inspectedFilesOnce bool
 fileInspectionError string
 fileInspectionCancel chan struct{}
}
type engine struct{
 mu sync.Mutex
 client *torrent.Client
 db *sql.DB
 downloads string
 cacheDir string // playback-cache directory; empty disables cache cleanup
 tmdbKey string
 entries map[string]*entry
 activeFileInspections int
 diskSpace func(string)(uint64,uint64,error)
 // Set by main to the transcoder entry point; nil in tests that run no media
 // tools, which leaves prewarming inert rather than panicking.
 prewarm func(hash,path string)
}
// Older Cloudflix SQLite catalogs did not store TMDB ratings.
// Add the column in place without removing any existing movies or downloads.
func ensureMovieRatingSchema(db *sql.DB) error {
 rows,err:=db.Query("PRAGMA table_info(torrents)")
 if err!=nil{return err}
 found:=false
 for rows.Next(){
  var index,required,pk int
  var name,fieldType string
  var defaultValue sql.NullString
  if err:=rows.Scan(&index,&name,&fieldType,&required,&defaultValue,&pk);err!=nil{rows.Close();return err}
  if name=="rating"{found=true}
 }
 if err:=rows.Err();err!=nil{rows.Close();return err}
 if err:=rows.Close();err!=nil{return err}
 if found{return nil}
 _,err=db.Exec("ALTER TABLE torrents ADD COLUMN rating REAL NOT NULL DEFAULT 0")
 return err
}

// Track magnets rejected after metadata inspection without requesting payloads.
func ensureMediaSchema(db *sql.DB) error {
 rows,err:=db.Query("PRAGMA table_info(torrents)")
 if err!=nil{return err}
 found:=false
 for rows.Next(){
  var index,required,pk int
  var name,typ string
  var def sql.NullString
  if err:=rows.Scan(&index,&name,&typ,&required,&def,&pk);err!=nil{rows.Close();return err}
  if name=="rejection_reason"{found=true}
 }
 if err:=rows.Err();err!=nil{rows.Close();return err}
 if err:=rows.Close();err!=nil{return err}
 if found{return nil}
 _,err=db.Exec("ALTER TABLE torrents ADD COLUMN rejection_reason TEXT NOT NULL DEFAULT ''")
 return err
}

func allowedMediaPath(path string)bool{
 if path=="" || strings.Contains(path,"\\"){return false}
 for _,part:=range strings.Split(path,"/"){
  if part=="" || part=="." || part==".."{return false}
 }
 local:=filepath.FromSlash(path)
 if !filepath.IsLocal(local) || filepath.ToSlash(local)!=path{return false}
 ext:=strings.ToLower(filepath.Ext(path))
 return ext==".mp4" || ext==".mkv" || ext==".srt"
}

func isMovieVideo(name string)bool{
 ext:=strings.ToLower(filepath.Ext(name))
 return ext==".mp4" || ext==".mkv"
}

func selectedMediaFiles(paths []string)(selected []string,hasVideo bool){
 for _,path:=range paths{
  if !allowedMediaPath(path){continue}
  selected=append(selected,path)
  if isMovieVideo(path){hasVideo=true}
 }
 return
}

// Confirm every selected file piece is fully hash-verified before playback.
// BytesCompleted also includes dirty/unverified chunks, so it alone is not
// sufficient proof of a finished video or subtitle.
func verifiedFile(file *torrent.File)bool{
 if file.Length()<=0{return false}
 states:=file.State()
 if len(states)==0{return false}
 for _,piece:=range states{
  if !piece.Ok || !piece.Complete{return false}
 }
 return true
}

func selectedFilesVerified(tt *torrent.Torrent)bool{
 if tt==nil || tt.Info()==nil{return false}
 foundVideo:=false
 for _,file:=range tt.Files(){
  if !allowedMediaPath(filepath.ToSlash(file.Path())) || file.Length()<=0{continue}
  if !verifiedFile(file){return false}
  if isMovieVideo(file.Path()){foundVideo=true}
 }
 return foundVideo
}

// How long a failed completion proof is trusted before the O(pieces) walk is
// attempted again. A torrent that is genuinely complete but still hashing must
// not pay the walk on every 2s snapshot tick.
const verifiedRecheckInterval=time.Second

// completedLocked proves that every selected media file is fully hash-verified,
// memoizing the expensive piece walk. size and completed come from the caller's
// already-computed selectedFileStats pass, so a finished movie costs O(1) per
// snapshot instead of O(pieces), which is what used to stall the engine mutex
// (and every concurrent media range request behind it).
//
// Caller holds e.mu. Once a torrent is proven complete it stays complete: this
// application never shrinks or re-fetches a finished download, and the entry
// (and this cache with it) is dropped when the torrent is removed.
func (e *engine) completedLocked(item *entry,tt *torrent.Torrent,size,completed int64)bool{
 if tt==nil || tt.Info()==nil || size<=0 || completed<size{
  item.verified=false
  return false
 }
 if item.verified{return true}
 if time.Since(item.verifiedAt)<verifiedRecheckInterval{return false}
 item.verifiedAt=time.Now()
 // Reuse the per-file proof cache so a completed movie is walked at most once
 // per process, not once per snapshot tick.
 foundVideo:=false
 for _,file:=range tt.Files(){
  path:=filepath.ToSlash(file.Path())
  if !allowedMediaPath(path) || file.Length()<=0{continue}
  if !e.fileVerifiedLocked(item,tt,path){return false}
  if isMovieVideo(path){foundVideo=true}
 }
 if !foundVideo{return false}
 item.verified=true
 return true
}

// fileVerifiedLocked reports whether a single selected media file is fully
// hash-verified, memoizing the per-file piece walk. Every authenticated range
// request for a movie used to repeat that walk while holding e.mu.
//
// Caller holds e.mu.
func (e *engine) fileVerifiedLocked(item *entry,tt *torrent.Torrent,path string)bool{
 if item==nil || item.rejectionReason!="" || tt==nil || tt.Info()==nil{return false}
 if item.verifiedFiles!=nil && item.verifiedFiles[path]{return true}
 for _,file:=range tt.Files(){
  if filepath.ToSlash(file.Path())!=path || !allowedMediaPath(path) || file.Length()<=0{continue}
  if !verifiedFile(file){return false}
  if item.verifiedFiles==nil{item.verifiedFiles=make(map[string]bool,2)}
  item.verifiedFiles[path]=true
  return true
 }
 return false
}

func selectedFileStats(tt *torrent.Torrent)(size,completed int64,files []map[string]any){
 files=make([]map[string]any,0)
 if tt==nil || tt.Info()==nil{return}
 for _,file:=range tt.Files(){
  path:=filepath.ToSlash(file.Path())
  if !allowedMediaPath(path){continue}
  n:=file.Length()
  if n<=0{continue}
  downloaded:=file.BytesCompleted()
  if downloaded<0{downloaded=0}
  if downloaded>n{downloaded=n}
  size+=n
  completed+=downloaded
  files=append(files,map[string]any{"path":path,"size":n,"completed":downloaded})
 }
 return
}

// Keep file listings small enough for the API while preserving the full count.
const maxManifestFiles=300
const maxManifestPathBytes=1024

type manifestFile struct {
 Path string `json:"path"`
 Size int64 `json:"size"`
 Selected bool `json:"selected"`
}

type torrentFileManifest struct {
 Files []manifestFile `json:"files"`
 Total int `json:"total"`
}

// Only filenames and sizes are stored. No torrent data is downloaded by inspection.
func buildFileManifest(files []manifestFile,hasMovie bool)torrentFileManifest{
 manifest:=torrentFileManifest{Files:make([]manifestFile,0),Total:len(files)}
 for _,file:=range files{
  if len(manifest.Files)>=maxManifestFiles{break}
  if len(file.Path)>maxManifestPathBytes || file.Path==""{continue}
  if file.Size<0{file.Size=0}
  file.Selected=hasMovie && file.Size>0 && allowedMediaPath(file.Path)
  manifest.Files=append(manifest.Files,file)
 }
 return manifest
}

// Purge persistent lists left by v0.1.14. New installations never create
// the file_manifest column, and no code writes these filenames to SQLite.
func purgeLegacyFileManifests(db *sql.DB)error{
 rows,err:=db.Query("PRAGMA table_info(torrents)")
 if err!=nil{return err}
 exists:=false
 for rows.Next(){
  var index,required,pk int
  var name,typ string
  var def sql.NullString
  if err:=rows.Scan(&index,&name,&typ,&required,&def,&pk);err!=nil{rows.Close();return err}
  if name=="file_manifest"{exists=true}
 }
 if err:=rows.Err();err!=nil{rows.Close();return err}
 if err:=rows.Close();err!=nil{return err}
 if !exists{return nil}
 _,err=db.Exec("UPDATE torrents SET file_manifest='' WHERE file_manifest!=''")
 return err
}

// Movie genre comes from TMDB's genre_ids on search results.
// The migration requeues existing matched movies exactly once so their
// genre can be backfilled, without modifying movie files or existing art.
func ensureMovieGenreSchema(db *sql.DB)error{
 rows,err:=db.Query("PRAGMA table_info(torrents)")
 if err!=nil{return err}
 found:=false
 for rows.Next(){
  var index,required,pk int
  var name,typ string
  var def sql.NullString
  if err:=rows.Scan(&index,&name,&typ,&required,&def,&pk);err!=nil{rows.Close();return err}
  if name=="genre"{found=true}
 }
 if err:=rows.Err();err!=nil{rows.Close();return err}
 if err:=rows.Close();err!=nil{return err}
 if found{return nil}
 if _,err:=db.Exec("ALTER TABLE torrents ADD COLUMN genre TEXT NOT NULL DEFAULT ''");err!=nil{return err}
 _,err=db.Exec("UPDATE torrents SET looked_up=0 WHERE looked_up=1 AND rejection_reason=''")
 return err
}

var tmdbMovieGenres=map[int]string{
 28:"Action",12:"Adventure",16:"Animation",35:"Comedy",
 80:"Crime",99:"Documentary",18:"Drama",10751:"Family",
 14:"Fantasy",36:"History",27:"Horror",10402:"Music",
 9648:"Mystery",10749:"Romance",878:"Science Fiction",10770:"TV Movie",
 53:"Thriller",10752:"War",37:"Western",
}

func firstMovieGenre(ids []int)string{
 for _,id:=range ids{
  if name,ok:=tmdbMovieGenres[id];ok{return name}
 }
 return ""
}

// Disable payload transfer in the spec before registering any magnet.
// DisallowDataDownload after AddMagnet leaves a brief request window.
func addMagnetMetadataOnly(client *torrent.Client,magnet string)(*torrent.Torrent,error){
 spec,err:=torrent.TorrentSpecFromMagnetUri(magnet)
 if err!=nil{return nil,err}
 spec.DisallowDataDownload=true
 spec.DisallowDataUpload=true
 tt,_,err:=client.AddTorrentSpec(spec)
 return tt,err
}

func newEngine(downloads string,db *sql.DB,key string)(*engine,error){
 if err:=ensureMovieRatingSchema(db);err!=nil{return nil,err}
 if err:=ensureMediaSchema(db);err!=nil{return nil,err}
 if err:=ensureMovieGenreSchema(db);err!=nil{return nil,err}
 if err:=purgeLegacyFileManifests(db);err!=nil{return nil,err}
 config:=torrent.NewDefaultClientConfig()
 config.DataDir=downloads
 config.Seed=false
 config.NoUpload=true
 config.NoDefaultPortForwarding=true
 config.HTTPDialContext=outbound.DialContext
 config.TrackerDialContext=outbound.DialContext
 config.WebTransport=outbound.Transport()
 config.MetainfoSourcesClient=outbound.Client()
 if raw:=os.Getenv("CLOUDFLIX_TORRENT_PORT");raw!=""{
  port,err:=strconv.Atoi(raw);if err!=nil || port<1 || port>65535{return nil,errors.New("invalid CLOUDFLIX_TORRENT_PORT")}
  config.ListenPort=port
 }
 client,err:=torrent.NewClient(config)
 if err!=nil{return nil,err}
 e:=&engine{client:client,db:db,downloads:downloads,cacheDir:filepath.Join(filepath.Dir(downloads),"playback-cache"),tmdbKey:key,entries:make(map[string]*entry)}
 if key!=""{_,_=db.Exec("UPDATE torrents SET looked_up=0 WHERE (poster='' OR rating=0) AND looked_up=1")}
 rows,err:=db.Query("SELECT rowid,hash,magnet,paused,title,year,genre,poster,backdrop,rating,looked_up,rejection_reason FROM torrents ORDER BY rowid ASC")
 if err!=nil{client.Close();return nil,err}
 defer rows.Close()
 for rows.Next(){
  var item entry
  var paused,lookedUp int
  if err:=rows.Scan(&item.queuePosition,&item.hash,&item.magnet,&paused,&item.title,&item.year,&item.genre,&item.poster,&item.backdrop,&item.rating,&lookedUp,&item.rejectionReason);err!=nil{client.Close();return nil,err}
  item.paused=paused!=0;item.lookedUp=lookedUp!=0
  if item.rejectionReason!="" {
   e.entries[item.hash]=&item
   continue
  }
  tt,err:=addMagnetMetadataOnly(client,item.magnet)
  if err!=nil{continue}
  item.hash=tt.InfoHash().HexString()
  item.torrent=tt
  e.entries[item.hash]=&item
  e.startWhenReady(&item)
 }
 return e,nil
}
func (e *engine) close(){e.client.Close()}
// Request metadata first and select only MP4, MKV and SRT payload files.
func (e *engine) startWhenReady(item *entry){
 go func(){
  tt:=item.torrent
  if tt==nil{return}
  select{
  case <-tt.GotInfo():
  case <-time.After(10*time.Minute):
   e.rejectTorrent(item,"Could not fetch torrent metadata. No movie was downloaded.")
   return
  }
  e.mu.Lock()
  defer e.mu.Unlock()
  if e.entries[item.hash]!=item || item.torrent!=tt{return}
  torrentFiles:=tt.Files()
  rawFiles:=make([]manifestFile,0,min(len(torrentFiles),maxManifestFiles))
  hasVideo:=false
  for _,file:=range torrentFiles{
   path:=filepath.ToSlash(file.Path())
   size:=file.Length()
   if len(rawFiles)<maxManifestFiles{
    rawFiles=append(rawFiles,manifestFile{Path:path,Size:size})
   }
   if allowedMediaPath(path) && size>0 && isMovieVideo(path){hasVideo=true}
  }
  // Preserve the actual torrent filename even if the magnet is rejected.
  if item.title=="" && tt.Name()!=""{
   title,year:=identify(tt.Name())
   if title==""{title=tt.Name()}
   item.title=title
   item.year=year
   _,_=e.db.Exec("UPDATE torrents SET title=?,year=? WHERE hash=?",title,year,item.hash)
  }
  manifest:=buildFileManifest(rawFiles,hasVideo)
  manifest.Total=len(torrentFiles)
  // Keep the inspected filenames only until the process restarts or Delete
  // is clicked. Rejected torrents never request payload pieces.
  item.fileManifest=&manifest
  if !hasVideo{
   e.rejectTorrentLocked(item,"Rejected: torrent contains no .mp4 or .mkv movie. Only .mp4, .mkv and .srt files are permitted.")
   return
  }
  if err:=e.checkTorrentStorageLocked(item);err!=nil{
   e.rejectTorrentLocked(item,err.Error())
   return
  }
  // Never call DownloadAll. Pieces can straddle a selected and skipped file,
  // so a small number of adjacent boundary bytes may still be transferred.
  for _,file:=range tt.Files(){file.SetPriority(torrent.PiecePriorityNone)}
  for _,file:=range tt.Files(){
   if allowedMediaPath(filepath.ToSlash(file.Path())) && file.Length()>0{file.Download()}
  }
  item.validated=true
  if !item.paused{tt.AllowDataDownload()}
 }()
}

func (e *engine) rejectTorrent(item *entry,reason string){
 e.mu.Lock()
 defer e.mu.Unlock()
 e.rejectTorrentLocked(item,reason)
}
func (e *engine) rejectTorrentLocked(item *entry,reason string){
 if e.entries[item.hash]!=item || item.rejectionReason!=""{return}
 if e.db!=nil{
  if _,err:=e.db.Exec("UPDATE torrents SET rejection_reason=? WHERE hash=?",reason,item.hash);err!=nil{
   reason="Torrent validation failed (could not save rejection)."
  }
 }
 item.rejectionReason=reason
 if item.fileManifest!=nil{
  for i:=range item.fileManifest.Files{item.fileManifest.Files[i].Selected=false}
 }
 if item.torrent!=nil{
  item.torrent.DisallowDataDownload()
  item.torrent.Drop()
  item.torrent=nil
 }
 item.validated=false
}

var errTMDBRequired=errors.New("TMDB API key is required. Open Settings to configure your TMDB API key before downloading.")

func (e *engine) add(magnet string)(string,error){
 if len(magnet)>8192 || len(magnet)<20 || !strings.HasPrefix(magnet,"magnet:?"){return "",errors.New("invalid magnet URI")}
 parsed,err:=url.Parse(magnet)
 if err!=nil || parsed.Scheme!="magnet" || len(parsed.Query()["xt"])==0{return "",errors.New("invalid magnet URI")}
 validXT:=false
 for _,xt:=range parsed.Query()["xt"]{if strings.HasPrefix(xt,"urn:btih:") || strings.HasPrefix(xt,"urn:btmh:"){validXT=true}}
 if !validXT{return "",errors.New("missing BitTorrent infohash")}
 e.mu.Lock()
 defer e.mu.Unlock()
 if strings.TrimSpace(e.tmdbKey)==""{return "",errTMDBRequired}
 if len(e.entries)>=100{return "",errors.New("torrent limit reached (100)")}
 tt,err:=addMagnetMetadataOnly(e.client,magnet)
 if err!=nil{return "",errors.New("invalid or unsupported magnet")}
 hash:=tt.InfoHash().HexString()
 // A duplicate request must not pause an existing validated torrent.
 if _,exists:=e.entries[hash];exists{return hash,nil}
 result,err:=e.db.Exec("INSERT INTO torrents(hash,magnet) VALUES(?,?)",hash,magnet)
 if err!=nil{tt.Drop();return "",err}
 queuePosition,err:=result.LastInsertId()
 if err!=nil{
  // SQLite reports a rowid for every newly inserted torrent.
  _=e.db.QueryRow("SELECT rowid FROM torrents WHERE hash=?",hash).Scan(&queuePosition)
 }
 item:=&entry{hash:hash,magnet:magnet,torrent:tt,queuePosition:queuePosition}
 e.entries[hash]=item
 e.startWhenReady(item)
 return hash,nil
}
// torrentFilesLocked lists the media paths a torrent owns: from live metadata
// when the torrent is still loaded, otherwise from the manifest captured while
// inspecting it. Both are filtered through allowedMediaPath, so a hostile name
// can never point outside the downloads root.
func (e *engine) torrentFilesLocked(item *entry)[]string{
 var paths []string
 if item.torrent!=nil && item.torrent.Info()!=nil{
  for _,file:=range item.torrent.Files(){
   if path:=filepath.ToSlash(file.Path());allowedMediaPath(path){paths=append(paths,path)}
  }
  return paths
 }
 if item.fileManifest!=nil{
  for _,file:=range item.fileManifest.Files{
   if file.Selected && allowedMediaPath(file.Path){paths=append(paths,file.Path)}
  }
 }
 return paths
}

// foreignTorrentFilesLocked collects every media path another queued torrent
// claims. Two torrents can ship the same filename, and the shared downloads
// directory means deleting one must never destroy the other's movie.
func (e *engine) foreignTorrentFilesLocked(skip string)map[string]bool{
 foreign:=make(map[string]bool)
 for hash,other:=range e.entries{
  if hash==skip || other==nil{continue}
  for _,path:=range e.torrentFilesLocked(other){foreign[path]=true}
 }
 return foreign
}

// deleteTorrentFilesLocked removes this torrent's payload files from disk. It
// only ever unlinks paths the torrent itself declares, inside an os.Root, and
// skips anything a different queued torrent still owns.
func (e *engine) deleteTorrentFilesLocked(item *entry)error{
 owned:=e.torrentFilesLocked(item)
 if len(owned)==0{return nil}
 foreign:=e.foreignTorrentFilesLocked(item.hash)
 root,err:=os.OpenRoot(e.downloads)
 if err!=nil{return err}
 defer root.Close()
 directories:=make(map[string]bool)
 var firstErr error
 for _,path:=range owned{
  if foreign[path]{continue}
  local:=filepath.FromSlash(path)
  if err:=root.Remove(local);err!=nil{
   // A torrent that was queued but never finished has no files on disk.
   if !errors.Is(err,os.ErrNotExist) && firstErr==nil{firstErr=err}
   continue
  }
  // Remember every ancestor so emptied release folders get tidied up.
  for dir:=filepath.Dir(local);dir!="." && dir!=string(filepath.Separator);dir=filepath.Dir(dir){
   directories[dir]=true
  }
 }
 // Deepest first: a nested folder can only be removed once its children are gone.
 ordered:=make([]string,0,len(directories))
 for dir:=range directories{ordered=append(ordered,dir)}
 sort.Slice(ordered,func(i,j int)bool{return len(ordered[i])>len(ordered[j])})
 for _,dir:=range ordered{
  // Remove only succeeds on an empty directory, which is exactly the guard we
  // want: a folder still holding another torrent's files is left alone.
  _=root.Remove(dir)
 }
 return firstErr
}

// deletePlaybackCacheLocked drops cached transcodes of this torrent's files.
// Without it, a re-added movie would serve a stale transcode of the old bytes.
// Both container layouts are cached under separate keys, so both are removed.
func (e *engine) deletePlaybackCacheLocked(item *entry){
 if e.cacheDir==""{return}
 for _,path:=range e.torrentFilesLocked(item){
  key:=mkvKey(item.hash,path)
  for _,prefix:=range []string{key,key+faststartSuffix}{
   _=os.Remove(filepath.Join(e.cacheDir,prefix+".mp4"))
   _=os.Remove(filepath.Join(e.cacheDir,prefix+".json"))
   _=os.Remove(filepath.Join(e.cacheDir,prefix+".part.mp4"))
  }
 }
}

// action applies a queue verb. deleteFiles only applies to "remove": true
// (the default at the HTTP layer) removes the payload along with the entry.
func (e *engine) action(hash,action string,deleteFiles bool)error{
 e.mu.Lock();defer e.mu.Unlock()
 item:=e.entries[hash];if item==nil{return errors.New("torrent not found")}
 switch action{
 case "pause":
  if item.rejectionReason!="" || item.torrent==nil{return errors.New("rejected torrent cannot be paused")}
  if _,err:=e.db.Exec("UPDATE torrents SET paused=1 WHERE hash=?",hash);err!=nil{return err}
  item.paused=true
  item.torrent.DisallowDataDownload()
 case "resume":
  if item.rejectionReason!="" || item.torrent==nil{return errors.New("rejected torrent cannot be resumed")}
  if item.validated {
   if err:=e.checkTorrentStorageLocked(item);err!=nil{return err}
  }
  if _,err:=e.db.Exec("UPDATE torrents SET paused=0 WHERE hash=?",hash);err!=nil{return err}
  item.paused=false
  if item.validated{item.torrent.AllowDataDownload()}
 case "remove":
  if _,err:=e.db.Exec("DELETE FROM torrents WHERE hash=?",hash);err!=nil{return err}
  if item.torrent!=nil{
   item.torrent.DisallowDataDownload()
   item.torrent.Drop()
   item.torrent=nil
  }
  // Stop the torrent before touching its files, then unlink the payload when
  // asked. Rejected torrents never requested payload pieces, so they have
  // nothing to delete. Paths always come from the torrent's own metadata or
  // the inspected manifest, never from the request.
  if deleteFiles && item.rejectionReason==""{
   if err:=e.deleteTorrentFilesLocked(item);err!=nil{
    // The queue row is already gone; report the file error rather than
    // pretending the delete was complete.
    item.fileManifest=nil
    delete(e.entries,hash)
    return fmt.Errorf("torrent removed, but some files could not be deleted: %w",err)
   }
   e.deletePlaybackCacheLocked(item)
  }
  item.fileManifest=nil
  item.fileInspectionError=""
  if item.fileInspectionCancel!=nil {
   close(item.fileInspectionCancel)
   item.fileInspectionCancel=nil
   if e.activeFileInspections>0{e.activeFileInspections--}
  }
  item.inspectingFiles=false
  delete(e.entries,hash)
 default:return errors.New("unknown action")
 }
 return nil
}
// orderedEntries uses the persisted SQLite insertion rowid, not the random
// iteration order of Go maps. Ties fall back to the stable infohash.
func orderedEntries(entries map[string]*entry)[]*entry{
 items:=make([]*entry,0,len(entries))
 for _,item:=range entries{items=append(items,item)}
 sort.Slice(items,func(i,j int)bool{
  a,b:=items[i],items[j]
  if a.queuePosition!=b.queuePosition{
   if a.queuePosition==0{return false}
   if b.queuePosition==0{return true}
   return a.queuePosition<b.queuePosition
  }
  return a.hash<b.hash
 })
 return items
}

func (e *engine) snapshot()[]map[string]any{
 e.mu.Lock();defer e.mu.Unlock()
 movies:=make([]map[string]any,0,len(e.entries))
 for _,item:=range orderedEntries(e.entries){
  name:=item.title
  if name==""{name="Fetching movie metadata..."}
  status:="metadata"
  var size,completed int64
  files:=make([]map[string]any,0)
  if item.rejectionReason!=""{
   status="rejected"
  }else if item.torrent!=nil {
   tt:=item.torrent
   if item.title=="" && tt.Name()!=""{name=tt.Name()}
   if tt.Info()!=nil{
    size,completed,files=selectedFileStats(tt)
   }
   if item.validated && e.completedLocked(item,tt,size,completed) {
    status="completed"
   }else if item.paused{
    status="paused"
   }else if item.validated{
    status="downloading"
   }
  }
  progress:=0.0
  if size>0{progress=100*float64(completed)/float64(size)}
  if progress>100{progress=100}
  movies=append(movies,map[string]any{
   "hash":item.hash,"title":name,"year":item.year,"genre":item.genre,"poster":item.poster,
   "backdrop":item.backdrop,"rating":item.rating,"size":size,"completed":completed,
   "progress":progress,"status":status,"files":files,"error":item.rejectionReason,
  })
 }
 return movies
}
var yearPattern=regexp.MustCompile("(19[0-9]{2}|20[0-9]{2})")
var qualityPattern=regexp.MustCompile("(?i)\\b(2160p|1080p|720p|480p|bluray|brrip|web-dl|webrip|x264|x265|h264|h265|hevc|remux|hdr|dvdrip)\\b")
func identify(raw string)(string,string){
 title:=strings.TrimSuffix(filepath.Base(raw),filepath.Ext(raw))
 title=strings.NewReplacer("."," ","_"," ","-"," ").Replace(title)
 year:=""
 if loc:=yearPattern.FindStringIndex(title);loc!=nil{
  year=title[loc[0]:loc[1]];title=title[:loc[0]]
 }
 title=qualityPattern.ReplaceAllString(title,"")
 title=strings.Join(strings.Fields(title)," ")
 return title,year
}
func (e *engine) run(ctx context.Context){
 ticker:=time.NewTicker(10*time.Second);defer ticker.Stop()
 for{
  e.refreshMetadata(ctx)
  e.prewarmCompleted(ctx)
  select{case <-ctx.Done():return;case <-ticker.C:}
 }
}

// completedMKVFilesLocked lists the verified .mkv files of every finished
// torrent, keyed by hash. Only files that are fully downloaded and verified
// qualify: a partial file cannot be transcoded.
func (e *engine) completedMKVFilesLocked()map[string][]string{
 finished:=make(map[string][]string)
 for hash,item:=range e.entries{
  if item==nil || item.rejectionReason!="" || !item.validated || item.torrent==nil{continue}
  tt:=item.torrent
  if tt.Info()==nil{continue}
  size,completed,_:=selectedFileStats(tt)
  if size<=0 || !e.completedLocked(item,tt,size,completed){continue}
  for _,path:=range e.torrentFilesLocked(item){
   if strings.EqualFold(filepath.Ext(path),".mkv"){finished[hash]=append(finished[hash],path)}
  }
 }
 return finished
}

// prewarmCompleted converts finished MKVs in the background so the movie is
// ready to play the moment the user clicks Watch. Previously the transcode only
// started on that click, so a feature-length movie meant staring at "Preparing
// MKV for playback" for 20+ minutes after the download had already completed.
func (e *engine) prewarmCompleted(ctx context.Context){
 if e.prewarm==nil{return}
 files:=e.finishedMKVs()
 if len(files)==0{return}
 for _,target:=range files{
  select{case <-ctx.Done():return;default:}
  e.prewarm(target.hash,target.path)
 }
}

// finishedMKV is one completed .mkv awaiting preparation.
type finishedMKV struct{hash,path string}

// finishedMKVs returns the completed MKV files not yet handed to the transcoder.
func (e *engine) finishedMKVs()[]finishedMKV{
 e.mu.Lock()
 defer e.mu.Unlock()
 pending:=make([]finishedMKV,0)
 for hash,paths:=range e.completedMKVFilesLocked(){
  item:=e.entries[hash]
  if item==nil{continue}
  for _,path:=range paths{
   if item.prewarmed[path]{continue}
   if item.prewarmed==nil{item.prewarmed=make(map[string]bool,2)}
   item.prewarmed[path]=true
   pending=append(pending,finishedMKV{hash:hash,path:path})
  }
 }
 sort.Slice(pending,func(i,j int)bool{
  if pending[i].hash!=pending[j].hash{return pending[i].hash<pending[j].hash}
  return pending[i].path<pending[j].path
 })
 return pending
}
func (e *engine) refreshMetadata(ctx context.Context){
 type candidate struct{hash,title,year string}
 pending:=make([]candidate,0)
 e.mu.Lock()
 key:=e.tmdbKey
 for _,item:=range e.entries{
  if item.lookedUp || item.rejectionReason!="" || item.torrent==nil || item.torrent.Info()==nil || time.Now().Before(item.retryAt){continue}
  title,year:=identify(item.torrent.Name())
  if title==""{continue}
  item.title=title;item.year=year
  _,_=e.db.Exec("UPDATE torrents SET title=?,year=? WHERE hash=?",title,year,item.hash)
  if key==""{
   item.lookedUp=true
  }else{
   item.attempts++
   item.retryAt=time.Now().Add(time.Minute)
   if item.attempts>=5{item.lookedUp=true}
   pending=append(pending,candidate{item.hash,title,year})
  }
 }
 e.mu.Unlock()
 for _,item:=range pending{
  select{case <-ctx.Done():return;default:}
  title,year,poster,backdrop,rating,genre:=lookupMovie(ctx,key,item.title,item.year)
  if title==""{continue}
  e.mu.Lock()
  if current:=e.entries[item.hash];current!=nil{
   current.title=title;current.year=year;current.genre=genre;current.poster=poster;current.backdrop=backdrop;current.rating=rating;current.lookedUp=true
   _,_=e.db.Exec("UPDATE torrents SET title=?,year=?,genre=?,poster=?,backdrop=?,rating=?,looked_up=1 WHERE hash=?",title,year,genre,poster,backdrop,rating,item.hash)
  }
  e.mu.Unlock()
 }
}
// TMDB vote_average is a rating from 0 to 10. Zero means not yet rated.
func parseTMDBMovie(data io.Reader, fallbackYear string)(string,string,string,string,float64,string,error){
 var result struct {
  Results []struct{
   Title string `json:"title"`
   ReleaseDate string `json:"release_date"`
   PosterPath string `json:"poster_path"`
   BackdropPath string `json:"backdrop_path"`
   Rating float64 `json:"vote_average"`
   GenreIDs []int `json:"genre_ids"`
  } `json:"results"`
 }
 if err:=json.NewDecoder(io.LimitReader(data,1<<20)).Decode(&result);err!=nil{return "","","","",0,"",err}
 if len(result.Results)==0{return "","","","",0,"",nil}
 first:=result.Results[0]
 year:=fallbackYear
 if len(first.ReleaseDate)>=4{year=first.ReleaseDate[:4]}
 rating:=first.Rating
 if rating<0 || rating>10 || rating!=rating{rating=0}
 return first.Title,year,first.PosterPath,first.BackdropPath,rating,firstMovieGenre(first.GenreIDs),nil
}

func lookupMovie(ctx context.Context,key,title,year string)(string,string,string,string,float64,string){
 q:=url.Values{"query":{title},"include_adult":{"false"}}
 if year!=""{q.Set("year",year)}
 endpoint:="https://api.themoviedb.org/3/search/movie?"+q.Encode()
 reqCtx,cancel:=context.WithTimeout(ctx,8*time.Second);defer cancel()
 request,err:=http.NewRequestWithContext(reqCtx,"GET",endpoint,nil)
 if err!=nil{return "","","","",0,""}
 applyTMDBAuthentication(request,key)
 client:=&http.Client{
  Timeout:8*time.Second,
  CheckRedirect:func(_ *http.Request,_ []*http.Request)error{return http.ErrUseLastResponse},
 }
 response,err:=client.Do(request)
 if err!=nil{return "","","","",0,""}
 defer response.Body.Close()
 if response.StatusCode!=http.StatusOK{return "","","","",0,""}
 resultTitle,resultYear,poster,backdrop,rating,genre,err:=parseTMDBMovie(response.Body,year)
 if err!=nil{return "","","","",0,""}
 return resultTitle,resultYear,poster,backdrop,rating,genre
}
func (a *application) addMagnet(w http.ResponseWriter,r *http.Request){
 var req map[string]string
 if !readJSON(w,r,&req){return}
 hash,err:=a.engine.add(req["magnet"])
 if errors.Is(err,errTMDBRequired){fail(w,http.StatusPreconditionRequired,err.Error());return}
 if err!=nil{fail(w,http.StatusBadRequest,err.Error());return}
 respond(w,201,map[string]string{"hash":hash})
}
func (a *application) torrentAction(w http.ResponseWriter,r *http.Request){
 hash:=r.PathValue("hash");action:=r.PathValue("action")
 if !regexp.MustCompile("^[0-9a-f]{40}$").MatchString(hash){fail(w,400,"invalid torrent hash");return}
 // Deleting a torrent now always removes its downloaded files too, so the
 // queue entry and the payload can never disagree. {"keepFiles":true} remains
 // available for tooling that deliberately wants the bytes left on disk.
 //
 // deleteFiles is accepted and ignored: it is what the browser sends for a
 // delete, and readJSON rejects unknown fields, so declaring it is what keeps
 // the button from failing with "invalid request" and deleting nothing.
 var req struct{
  KeepFiles bool `json:"keepFiles"`
  DeleteFiles bool `json:"deleteFiles"`
 }
 if r.ContentLength>0{
  if !readJSON(w,r,&req){return}
 }
 err:=a.engine.action(hash,action,action=="remove" && !req.KeepFiles)
 if err!=nil{fail(w,400,err.Error());return}
 respond(w,200,map[string]bool{"ok":true})
}
func (a *application) downloadFile(w http.ResponseWriter,r *http.Request){
 hash:=r.PathValue("hash");wanted:=r.URL.Query().Get("path")
 if !regexp.MustCompile("^[0-9a-f]{40}$").MatchString(hash) || wanted==""{fail(w,400,"invalid file request");return}
 e:=a.engine
 e.mu.Lock()
 item:=e.entries[hash]
 valid:=false
 if item!=nil && item.validated && item.torrent!=nil && item.torrent.Info()!=nil{
  for _,file:=range item.torrent.Files(){
   if filepath.ToSlash(file.Path())==wanted && allowedMediaPath(wanted) && verifiedFile(file){valid=true;break}
  }
 }
 e.mu.Unlock()
 if !valid || !filepath.IsLocal(filepath.FromSlash(wanted)){fail(w,404,"file not found");return}
 root,err:=os.OpenRoot(e.downloads)
 if err!=nil{fail(w,500,"storage unavailable");return}
 defer root.Close()
 f,err:=root.Open(filepath.FromSlash(wanted))
 if err!=nil{fail(w,404,"file not found");return}
 defer f.Close()
 info,err:=f.Stat()
 if err!=nil || !info.Mode().IsRegular(){fail(w,404,"file not found");return}
 w.Header().Set("Content-Disposition","attachment")
 http.ServeContent(w,r,info.Name(),info.ModTime(),f)
}
