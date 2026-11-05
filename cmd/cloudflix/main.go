package main

import (
 "context"
 "crypto/rand"
 "crypto/sha256"
 "crypto/subtle"
 "database/sql"
 "encoding/hex"
 "encoding/json"
 "errors"
 "fmt"
 "io"
 "log"
 "net"
 "net/http"
 "net/url"
 "os"
 "path/filepath"
 "strings"
 "sync"
 "time"

 "github.com/alexedwards/argon2id"
 "github.com/devseckobz/cloudflix/static"
 _ "modernc.org/sqlite"
)

var version = "dev"

type application struct {
 db *sql.DB
 engine *engine
 playback *mkvManager
 tmdbKey string
 dataDir string
 authMu sync.Mutex
 attempts map[string][]time.Time
 liveMu sync.Mutex
 live *liveState
 movieInfo func(hash string)(title string,completed,known bool) // test seam for the movie catalog
}

// liveState fans one snapshot per tick out to every connected browser. Building
// a snapshot walks every torrent and every selected file's piece states under
// engine.mu, so computing it per client made N open tabs cost N walks and N
// JSON encodes every two seconds, all of it contending with the media range
// requests that must answer inside a 16.7ms frame budget. One goroutine now
// builds the payload once and every subscriber receives the same bytes; a
// stalled client skips ticks instead of blocking the broadcaster or its peers.
type liveState struct{
 mu sync.Mutex
 subscribers map[chan []byte]struct{}
 last []byte
 running bool
 wake chan struct{} // poked when the last subscriber leaves, so exit is immediate
}

func main() {
 if len(os.Args)>1 && os.Args[1]=="pin"{
  if err:=pinCommand(os.Args[2:]);err!=nil{
   if errors.Is(err,ErrPINNotConfigured){os.Exit(3)}
   log.Fatal(err)
  }
  return
 }
 dataDir:=env("CLOUDFLIX_DATA_DIR","./data")
 if err:=os.MkdirAll(dataDir,0700);err!=nil{log.Fatal(err)}
 dataDir,err:=filepath.Abs(dataDir);if err!=nil{log.Fatal(err)}
 downloads:=filepath.Join(dataDir,"downloads")
 if err:=os.MkdirAll(downloads,0700);err!=nil{log.Fatal(err)}
 db,err:=sql.Open("sqlite",filepath.Join(dataDir,"cloudflix.db"))
 if err!=nil{log.Fatal(err)}
 db.SetMaxOpenConns(1)
 defer db.Close()
 statements:=[]string{
 "PRAGMA journal_mode=WAL",
 "PRAGMA busy_timeout=5000",
 "CREATE TABLE IF NOT EXISTS users (id INTEGER PRIMARY KEY CHECK(id=1), password_hash TEXT NOT NULL)",
 "CREATE TABLE IF NOT EXISTS torrents (hash TEXT PRIMARY KEY, magnet TEXT NOT NULL, paused INTEGER NOT NULL DEFAULT 0, title TEXT NOT NULL DEFAULT '', year TEXT NOT NULL DEFAULT '', poster TEXT NOT NULL DEFAULT '', backdrop TEXT NOT NULL DEFAULT '', looked_up INTEGER NOT NULL DEFAULT 0)",
 }
 for _,statement:=range statements {if _,err=db.Exec(statement);err!=nil{log.Fatal(err)}}
 if err:=ensurePINSchema(db);err!=nil{log.Fatal(err)}
 if err:=ensureSessionsSchema(db);err!=nil{log.Fatal(err)}
 if err:=ensureSettingsSchema(db);err!=nil{log.Fatal(err)}
 tmdbKey,err:=loadTMDBKey(db,os.Getenv("TMDB_API_KEY"))
 if err!=nil{log.Fatal(err)}
 a:=&application{db:db,dataDir:dataDir,tmdbKey:tmdbKey,playback:newMKVManager(),attempts:make(map[string][]time.Time)}
 a.engine,err=newEngine(downloads,db,a.tmdbKey)
 if err!=nil{log.Fatal(err)}
 defer a.engine.close()
 ctx,cancel:=context.WithCancel(context.Background());defer cancel()
 go a.engine.run(ctx)
 mux:=http.NewServeMux()
 mux.HandleFunc("GET /api/health",func(w http.ResponseWriter,r *http.Request){respond(w,200,map[string]any{"status":"ok","version":version})})
 mux.HandleFunc("GET /api/auth/status",a.authStatus)
 mux.HandleFunc("POST /api/auth/login",a.login)
 mux.HandleFunc("POST /api/auth/logout",a.require(a.logout))
 mux.HandleFunc("GET /api/state",a.require(a.state))
 mux.HandleFunc("GET /api/session",a.require(a.sessionResume))
 mux.HandleFunc("POST /api/session/watching",a.require(a.sessionWatching))
 mux.HandleFunc("GET /api/sessions",a.require(a.sessionList))
 mux.HandleFunc("DELETE /api/sessions/{id}",a.require(a.sessionRevoke))
 mux.HandleFunc("GET /api/settings",a.require(a.getSettings))
 mux.HandleFunc("PUT /api/settings",a.require(a.saveSettings))
 mux.HandleFunc("GET /api/updates",a.require(a.updateInfo))
 mux.HandleFunc("POST /api/updates",a.require(a.requestUpdate))
 mux.HandleFunc("GET /api/events",a.require(a.events))
 mux.HandleFunc("POST /api/magnets",a.require(a.addMagnet))
 mux.HandleFunc("POST /api/torrents/{hash}/{action}",a.require(a.torrentAction))
 mux.HandleFunc("GET /api/files/{hash}",a.require(a.downloadFile))
 mux.HandleFunc("GET /api/media/{hash}",a.require(a.streamMedia))
 mux.HandleFunc("POST /api/playback/{hash}/prepare",a.require(a.mkvPrepare))
 mux.HandleFunc("GET /api/playback/{hash}/status",a.require(a.mkvStatus))
 mux.HandleFunc("GET /api/playback/{hash}/video",a.require(func(w http.ResponseWriter,r *http.Request){a.serveMKVCache(w,r,false)}))
 mux.HandleFunc("GET /api/playback/{hash}/subtitle",a.require(func(w http.ResponseWriter,r *http.Request){a.serveMKVCache(w,r,true)}))
 mux.HandleFunc("GET /api/torrents/{hash}/files",a.require(a.torrentFiles))
 mux.HandleFunc("GET /api/torrents/{hash}/magnet",a.require(a.torrentMagnet))
 mux.Handle("/",static.Handler())
 addr:=env("CLOUDFLIX_LISTEN","0.0.0.0:80")
 server:=&http.Server{Addr:addr,Handler:securityHeaders(mux),ReadHeaderTimeout:5*time.Second,IdleTimeout:60*time.Second,ReadTimeout:20*time.Second,MaxHeaderBytes:1<<20}
 log.Printf("Cloudflix %s listening on %s",version,addr)
 if err:=server.ListenAndServe();err!=nil && !errors.Is(err,http.ErrServerClosed){log.Fatal(err)}
}

func env(key,fallback string)string{if value:=os.Getenv(key);value!=""{return value};return fallback}

func securityHeaders(next http.Handler)http.Handler{
 return http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
  w.Header().Set("X-Content-Type-Options","nosniff")
  w.Header().Set("Referrer-Policy","no-referrer")
  w.Header().Set("X-Frame-Options","DENY")
  w.Header().Set("Content-Security-Policy","default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' https://image.tmdb.org data:; connect-src 'self'; object-src 'none'; base-uri 'none'; frame-ancestors 'none'")
  if r.Method=="POST" || r.Method=="PUT" || r.Method=="PATCH" || r.Method=="DELETE" {
   // A bodyless DELETE has no JSON payload to validate; the Origin check below
   // still applies, and browsers cannot issue a cross-site DELETE without CORS.
   if (r.Method!="DELETE" || r.ContentLength!=0) && !strings.HasPrefix(r.Header.Get("Content-Type"),"application/json"){http.Error(w,"expected application/json",415);return}
   if origin:=r.Header.Get("Origin");origin!=""{
    if !sameOriginHost(origin,r.Host){http.Error(w,"forbidden origin",403);return}
   }
  }
  next.ServeHTTP(w,r)
 })
}
// Compare hostnames case-insensitively and normalize HTTP's default port.
// Browsers omit :80 from Origin even when a proxy preserves :80 in Host.
func sameOriginHost(origin,host string)bool{
 source,err:=url.Parse(origin)
 if err!=nil || (source.Scheme!="http" && source.Scheme!="https") || source.Hostname()=="" || source.User!=nil || source.Path!="" || source.RawQuery!="" || source.Fragment!=""{return false}
 target,err:=url.Parse("//"+host)
 if err!=nil || target.Hostname()=="" || target.User!=nil || target.Path!=""{return false}
 expectedPort:="80"
 if source.Scheme=="https"{expectedPort="443"}
 originPort:=source.Port()
 if originPort==""{originPort=expectedPort}
 targetPort:=target.Port()
 if targetPort==""{targetPort=expectedPort}
 return strings.EqualFold(source.Hostname(),target.Hostname()) && originPort==targetPort
}
func respond(w http.ResponseWriter,status int,value any){
 w.Header().Set("Content-Type","application/json; charset=utf-8")
 w.Header().Set("Cache-Control","no-store")
 w.WriteHeader(status)
 _=json.NewEncoder(w).Encode(value)
}
func fail(w http.ResponseWriter,status int,message string){respond(w,status,map[string]string{"error":message})}
func readJSON(w http.ResponseWriter,r *http.Request,dst any)bool{
 r.Body=http.MaxBytesReader(w,r.Body,16<<10)
 decoder:=json.NewDecoder(r.Body);decoder.DisallowUnknownFields()
 if err:=decoder.Decode(dst);err!=nil{fail(w,400,"invalid request");return false}
 var extra any
 if err:=decoder.Decode(&extra);err!=io.EOF{
  fail(w,400,"unexpected trailing content");return false
 }
 return true
}
func randomToken()(string,error){data:=make([]byte,32);if _,err:=rand.Read(data);err!=nil{return "",err};return hex.EncodeToString(data),nil}
func hashToken(token string)string{sum:=sha256.Sum256([]byte(token));return hex.EncodeToString(sum[:])}
func (a *application) authStatus(w http.ResponseWriter,r *http.Request){respond(w,200,map[string]bool{"setupRequired":!pinConfigured(a.db),"authenticated":a.authenticated(r)&&pinConfigured(a.db)})}
func (a *application) authenticated(r *http.Request)bool{
 cookie,err:=r.Cookie("cloudflix_session")
 if err!=nil || len(cookie.Value)!=64{return false}
 sum:=hashToken(cookie.Value)
 var stored string
 if err=a.db.QueryRowContext(r.Context(),"SELECT token_hash FROM sessions WHERE token_hash=? AND expires_at>?",sum,time.Now().Unix()).Scan(&stored);err!=nil{return false}
 if subtle.ConstantTimeCompare([]byte(stored),[]byte(sum))!=1{return false}
 // Cheap touch on every authenticated request; skipped within the same second
 // so a burst of range requests does not queue writes behind the single connection.
 _,_=a.db.ExecContext(r.Context(),"UPDATE sessions SET last_seen=? WHERE token_hash=? AND last_seen<?",time.Now().Unix(),sum,time.Now().Unix())
 return true
}
func (a *application) require(h http.HandlerFunc)http.HandlerFunc{
 return func(w http.ResponseWriter,r *http.Request){if !pinConfigured(a.db)||!a.authenticated(r){fail(w,401,"PIN authentication required");return};h(w,r)}
}
func ipAddress(r *http.Request)string{host,_,err:=net.SplitHostPort(r.RemoteAddr);if err!=nil{return r.RemoteAddr};return host}
func (a *application) allowLogin(ip string)bool{
 a.authMu.Lock();defer a.authMu.Unlock()
 now:=time.Now();recent:=a.attempts[ip][:0]
 for _,t:=range a.attempts[ip]{if now.Sub(t)<10*time.Minute{recent=append(recent,t)}}
 if len(a.attempts)>2000{a.attempts=make(map[string][]time.Time)}
 if len(recent)>=10{a.attempts[ip]=recent;return false}
 a.attempts[ip]=append(recent,now);return true
}
func (a *application) login(w http.ResponseWriter,r *http.Request){
 if !a.allowLogin(ipAddress(r)){w.Header().Set("Retry-After","600");fail(w,429,"Too many attempts. Try again later.");return}
 var req struct{PIN string `json:"pin"`}
 if !readJSON(w,r,&req){return}
 a.authMu.Lock()
 defer a.authMu.Unlock()
 if !pinConfigured(a.db){fail(w,503,"Access PIN must be configured on the VPS");return}
 locked,until,err:=a.pinLocked()
 if err!=nil{fail(w,500,"Authentication unavailable");return}
 if locked{
  w.Header().Set("Retry-After",fmt.Sprint(until-time.Now().Unix()))
  fail(w,429,"Too many incorrect PIN attempts. Try again in 15 minutes.")
  return
 }
 valid:=false
 if pinPattern.MatchString(req.PIN){
  var hash string
  if err:=a.db.QueryRow("SELECT password_hash FROM users WHERE id=1 AND pin_enabled=1").Scan(&hash);err==nil{
   matched,err:=argon2id.ComparePasswordAndHash(req.PIN,hash)
   valid=err==nil && matched
  }
 }
 if !valid{
  if err:=a.pinFailure();err!=nil{fail(w,500,"Authentication unavailable");return}
  fail(w,401,"Incorrect PIN")
  return
 }
 if err:=a.clearPinFailures();err!=nil{fail(w,500,"Authentication unavailable");return}
 a.issueSession(w,r)
}
func (a *application) issueSession(w http.ResponseWriter,r *http.Request){
 token,err:=randomToken();if err!=nil{fail(w,500,"session error");return}
 now:=time.Now()
 expiration:=now.Add(sessionLifetime)
 if _,err=a.db.Exec("INSERT INTO sessions(token_hash,expires_at,ip,user_agent,created_at,last_seen) VALUES(?,?,?,?,?,?)",hashToken(token),expiration.Unix(),ipAddress(r),userAgent(r),now.Unix(),now.Unix());err!=nil{fail(w,500,"session error");return}
 http.SetCookie(w,&http.Cookie{Name:"cloudflix_session",Value:token,Path:"/",HttpOnly:true,Secure:secureRequest(r),SameSite:http.SameSiteStrictMode,Expires:expiration})
 respond(w,200,map[string]bool{"ok":true})
}
func secureRequest(r *http.Request)bool{
 if r.TLS!=nil{return true}
 host,_,err:=net.SplitHostPort(r.RemoteAddr);if err!=nil{return false}
 addr:=net.ParseIP(host)
 return addr!=nil && addr.IsLoopback() && r.Header.Get("X-Forwarded-Proto")=="https"
}
func (a *application) logout(w http.ResponseWriter,r *http.Request){
 if cookie,err:=r.Cookie("cloudflix_session");err==nil{_,_=a.db.Exec("DELETE FROM sessions WHERE token_hash=?",hashToken(cookie.Value))}
 http.SetCookie(w,&http.Cookie{Name:"cloudflix_session",Path:"/",MaxAge:-1,HttpOnly:true,Secure:secureRequest(r),SameSite:http.SameSiteStrictMode})
 respond(w,200,map[string]bool{"ok":true})
}
func (a *application) state(w http.ResponseWriter,r *http.Request){respond(w,200,map[string]any{"movies":a.engine.snapshot(),"storage":a.engine.storage(),"version":version})}
func (a *application) liveState()*liveState{
 a.liveMu.Lock()
 defer a.liveMu.Unlock()
 if a.live==nil{a.live=&liveState{subscribers:map[chan []byte]struct{}{},wake:make(chan struct{},1)}}
 return a.live
}
func (a *application) livePayload()[]byte{
 payload,_:=json.Marshal(map[string]any{"movies":a.engine.snapshot(),"storage":a.engine.storage(),"version":version})
 return payload
}
// liveSubscribe registers one SSE client and starts the shared broadcaster on
// demand. The returned channel is buffered with the latest frame, so a client
// that falls behind simply misses a tick instead of stalling anyone else.
func (a *application) liveSubscribe()(<-chan []byte,func()){
 live:=a.liveState()
 updates:=make(chan []byte,1)
 live.mu.Lock()
 live.subscribers[updates]=struct{}{}
 if !live.running{
  live.running=true
  go a.liveBroadcast(live)
 }
 last:=live.last
 live.mu.Unlock()
 if last!=nil{select{case updates<-last:default:}}
 return updates,func(){
  live.mu.Lock()
  delete(live.subscribers,updates)
  idle:=len(live.subscribers)==0
  live.mu.Unlock()
  if idle{select{case live.wake<-struct{}{}:default:}}
 }
}
// liveBroadcast computes the snapshot once per tick and fans the identical
// bytes out to every subscriber. It exits when the last client disconnects, so
// an idle server pays nothing and no goroutine is left behind.
func (a *application) liveBroadcast(live *liveState){
 ticker:=time.NewTicker(2*time.Second)
 defer ticker.Stop()
 for{
  live.mu.Lock()
  if len(live.subscribers)==0{
   live.running=false
   live.last=nil
   live.mu.Unlock()
   return
  }
  live.mu.Unlock()
  payload:=a.livePayload()
  live.mu.Lock()
  if len(live.subscribers)==0{
   live.running=false
   live.last=nil
   live.mu.Unlock()
   return
  }
  live.last=payload
  for updates:=range live.subscribers{
   // Replace any frame the client has not read yet: a slow client must see the
   // newest state, never a backlog, and must never block this loop.
   select{case <-updates:default:}
   select{case updates<-payload:default:}
  }
  live.mu.Unlock()
  select{
  case <-ticker.C:
  case <-live.wake:
   // A client left. Loop back so the subscriber set is re-checked: if someone
   // else connected in the meantime the broadcaster must keep running.
  }
 }
}
func (a *application) events(w http.ResponseWriter,r *http.Request){
 w.Header().Set("Content-Type","text/event-stream")
 w.Header().Set("Cache-Control","no-store")
 w.Header().Set("X-Accel-Buffering","no")
 flusher,ok:=w.(http.Flusher);if !ok{fail(w,500,"stream unavailable");return}
 updates,unsubscribe:=a.liveSubscribe()
 defer unsubscribe()
 for{
  select{
  case <-r.Context().Done():return
  case payload:=<-updates:
   if _,err:=fmt.Fprintf(w,"data: %s\n\n",payload);err!=nil{return}
   flusher.Flush()
  }
 }
}
