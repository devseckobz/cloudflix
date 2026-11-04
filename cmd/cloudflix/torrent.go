package main

import (
 "context"
 "database/sql"
 "encoding/json"
 "errors"
 "io"
 "net/http"
 "net/url"
 "os"
 "path/filepath"
 "regexp"
 "strconv"
 "strings"
 "sync"
 "time"

 "github.com/anacrolix/torrent"
 "github.com/devseckobz/cloudflix/internal/outbound"
)

type entry struct {
 hash string
 magnet string
 torrent *torrent.Torrent
 paused bool
 title string
 year string
 poster string
 backdrop string
 lookedUp bool
 attempts int
 retryAt time.Time
}
type engine struct{
 mu sync.Mutex
 client *torrent.Client
 db *sql.DB
 downloads string
 tmdbKey string
 entries map[string]*entry
}
func newEngine(downloads string,db *sql.DB,key string)(*engine,error){
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
 e:=&engine{client:client,db:db,downloads:downloads,tmdbKey:key,entries:make(map[string]*entry)}
 if key!=""{_,_=db.Exec("UPDATE torrents SET looked_up=0 WHERE poster=''")}
 rows,err:=db.Query("SELECT hash,magnet,paused,title,year,poster,backdrop,looked_up FROM torrents")
 if err!=nil{client.Close();return nil,err}
 defer rows.Close()
 for rows.Next(){
  var item entry
  var paused,lookedUp int
  if err:=rows.Scan(&item.hash,&item.magnet,&paused,&item.title,&item.year,&item.poster,&item.backdrop,&lookedUp);err!=nil{client.Close();return nil,err}
  tt,err:=client.AddMagnet(item.magnet)
  if err!=nil{continue}
  item.hash=tt.InfoHash().HexString()
  item.torrent=tt;item.paused=paused!=0;item.lookedUp=lookedUp!=0
  e.entries[item.hash]=&item
  e.startWhenReady(&item)
 }
 return e,nil
}
func (e *engine) close(){e.client.Close()}
func (e *engine) startWhenReady(item *entry){
 go func(){
  select{case <-item.torrent.GotInfo():case <-time.After(24*time.Hour):return}
  e.mu.Lock()
  defer e.mu.Unlock()
  if e.entries[item.hash]!=item{return}
  item.torrent.DownloadAll()
  if item.paused {item.torrent.DisallowDataDownload()}
 }()
}
func (e *engine) add(magnet string)(string,error){
 if len(magnet)>8192 || len(magnet)<20 || !strings.HasPrefix(magnet,"magnet:?"){return "",errors.New("invalid magnet URI")}
 parsed,err:=url.Parse(magnet)
 if err!=nil || parsed.Scheme!="magnet" || len(parsed.Query()["xt"])==0{return "",errors.New("invalid magnet URI")}
 validXT:=false
 for _,xt:=range parsed.Query()["xt"]{if strings.HasPrefix(xt,"urn:btih:") || strings.HasPrefix(xt,"urn:btmh:"){validXT=true}}
 if !validXT{return "",errors.New("missing BitTorrent infohash")}
 e.mu.Lock()
 defer e.mu.Unlock()
 if len(e.entries)>=100{return "",errors.New("torrent limit reached (100)")}
 tt,err:=e.client.AddMagnet(magnet)
 if err!=nil{return "",errors.New("invalid or unsupported magnet")}
 hash:=tt.InfoHash().HexString()
 if _,exists:=e.entries[hash];exists{return hash,nil}
 if _,err=e.db.Exec("INSERT INTO torrents(hash,magnet) VALUES(?,?)",hash,magnet);err!=nil{tt.Drop();return "",err}
 item:=&entry{hash:hash,magnet:magnet,torrent:tt}
 e.entries[hash]=item
 e.startWhenReady(item)
 return hash,nil
}
func (e *engine) action(hash,action string)error{
 e.mu.Lock();defer e.mu.Unlock()
 item:=e.entries[hash];if item==nil{return errors.New("torrent not found")}
 switch action{
 case "pause":
  if _,err:=e.db.Exec("UPDATE torrents SET paused=1 WHERE hash=?",hash);err!=nil{return err}
  item.paused=true
  item.torrent.DisallowDataDownload()
 case "resume":
  if _,err:=e.db.Exec("UPDATE torrents SET paused=0 WHERE hash=?",hash);err!=nil{return err}
  item.paused=false
  if item.torrent.Info()!=nil{item.torrent.AllowDataDownload();item.torrent.DownloadAll()}
 case "remove":
  if _,err:=e.db.Exec("DELETE FROM torrents WHERE hash=?",hash);err!=nil{return err}
  item.torrent.Drop()
  delete(e.entries,hash)
 default:return errors.New("unknown action")
 }
 return nil
}
func (e *engine) snapshot()[]map[string]any{
 e.mu.Lock();defer e.mu.Unlock()
 movies:=make([]map[string]any,0,len(e.entries))
 for _,item:=range e.entries{
  tt:=item.torrent
  name:=tt.Name()
  if name==""{name="Fetching metadata..."}
  if item.title!=""{name=item.title}
  size:=tt.Length()
  completed:=tt.BytesCompleted()
  percent:=0.0
  if size>0{percent=100*float64(completed)/float64(size)}
  if percent>100{percent=100}
  status:="metadata"
  if item.paused{status="paused"}else if tt.Info()!=nil{status="downloading";if size>0 && completed>=size{status="completed"}}
  files:=make([]map[string]any,0)
  if tt.Info()!=nil{
   for _,file:=range tt.Files(){
    path:=filepath.ToSlash(file.Path())
    if path==""{continue}
    files=append(files,map[string]any{"path":path,"size":file.Length()})
   }
  }
  movies=append(movies,map[string]any{
   "hash":item.hash,"title":name,"year":item.year,"poster":item.poster,"backdrop":item.backdrop,
   "size":size,"completed":completed,"progress":percent,"status":status,"files":files,
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
  select{case <-ctx.Done():return;case <-ticker.C:}
 }
}
func (e *engine) refreshMetadata(ctx context.Context){
 type candidate struct{hash,title,year string}
 pending:=make([]candidate,0)
 e.mu.Lock()
 key:=e.tmdbKey
 for _,item:=range e.entries{
  if item.lookedUp || item.torrent.Info()==nil || time.Now().Before(item.retryAt){continue}
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
  title,year,poster,backdrop:=lookupMovie(ctx,key,item.title,item.year)
  if title==""{continue}
  e.mu.Lock()
  if current:=e.entries[item.hash];current!=nil{
   current.title=title;current.year=year;current.poster=poster;current.backdrop=backdrop;current.lookedUp=true
   _,_=e.db.Exec("UPDATE torrents SET title=?,year=?,poster=?,backdrop=?,looked_up=1 WHERE hash=?",title,year,poster,backdrop,item.hash)
  }
  e.mu.Unlock()
 }
}
func lookupMovie(ctx context.Context,key,title,year string)(string,string,string,string){
 q:=url.Values{"query":{title},"include_adult":{"false"}}
 if year!=""{q.Set("year",year)}
 endpoint:="https://api.themoviedb.org/3/search/movie?"+q.Encode()
 reqCtx,cancel:=context.WithTimeout(ctx,8*time.Second);defer cancel()
 request,err:=http.NewRequestWithContext(reqCtx,"GET",endpoint,nil)
 if err!=nil{return "","","",""}
 applyTMDBAuthentication(request,key)
 client:=&http.Client{Timeout:8*time.Second}
 response,err:=client.Do(request)
 if err!=nil{return "","","",""}
 defer response.Body.Close()
 if response.StatusCode!=200{return "","","",""}
 // Decode TMDB's underscore-separated JSON fields using explicit field maps.
 var body struct{Results []map[string]any}
 if err:=json.NewDecoder(io.LimitReader(response.Body,1<<20)).Decode(&body);err!=nil{return "","","",""}
 if len(body.Results)==0{return "","","",""}
 first:=body.Results[0]
 get:=func(key string)string{value,_:=first[key].(string);return value}
 matched:=get("title");release:=get("release_date")
 if len(release)>=4{release=release[:4]}else{release=year}
 return matched,release,get("poster_path"),get("backdrop_path")
}
func (a *application) addMagnet(w http.ResponseWriter,r *http.Request){
 var req map[string]string
 if !readJSON(w,r,&req){return}
 hash,err:=a.engine.add(req["magnet"])
 if err!=nil{fail(w,400,err.Error());return}
 respond(w,201,map[string]string{"hash":hash})
}
func (a *application) torrentAction(w http.ResponseWriter,r *http.Request){
 hash:=r.PathValue("hash");action:=r.PathValue("action")
 if !regexp.MustCompile("^[0-9a-f]{40}$").MatchString(hash){fail(w,400,"invalid torrent hash");return}
 err:=a.engine.action(hash,action)
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
 if item!=nil && item.torrent.Info()!=nil{
  for _,file:=range item.torrent.Files(){if filepath.ToSlash(file.Path())==wanted{valid=true;break}}
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
