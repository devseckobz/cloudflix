package main

import (
 "database/sql"
 "errors"
 "math"
 "net/http"
 "regexp"
 "sort"
 "strings"
 "time"
 "unicode/utf8"
)

const (
 sessionLifetime=7*24*time.Hour
 watchingActiveSeconds=120
 shortSessionIDLength=16
 maxUserAgentBytes=256
 // A position beyond any feature film is treated as nonsense, not stored.
 maxWatchingPosition=30*24*60*60
)

var sessionIDPattern=regexp.MustCompile("^[0-9a-f]{16}$")

// Every browser login owns one row: who it is, when it was last active and
// what this browser is watching, so the theater can resume where it stopped.
const sessionsTable="CREATE TABLE IF NOT EXISTS sessions (token_hash TEXT PRIMARY KEY, expires_at INTEGER NOT NULL, ip TEXT NOT NULL DEFAULT '', user_agent TEXT NOT NULL DEFAULT '', created_at INTEGER NOT NULL DEFAULT 0, last_seen INTEGER NOT NULL DEFAULT 0, watching_hash TEXT NOT NULL DEFAULT '', watching_position REAL NOT NULL DEFAULT 0, watching_updated_at INTEGER NOT NULL DEFAULT 0)"

type resumeState struct{
 Hash string `json:"hash"`
 Position float64 `json:"position"`
 Title string `json:"title"`
 UpdatedAt int64 `json:"updatedAt"`
}

type watchingState struct{
 Hash string `json:"hash"`
 Title string `json:"title"`
 Position float64 `json:"position"`
}

type sessionView struct{
 ID string `json:"id"`
 Current bool `json:"current"`
 IP string `json:"ip"`
 UserAgent string `json:"userAgent"`
 CreatedAt int64 `json:"createdAt"`
 LastSeen int64 `json:"lastSeen"`
 Status string `json:"status"`
 Watching *watchingState `json:"watching"`
 hash string
 position float64
 updatedAt int64
}

// Sessions written by older releases hold only the token hash and expiry.
// Missing columns are added in place, so existing logins keep working.
func ensureSessionsSchema(db *sql.DB)error{
 if _,err:=db.Exec(sessionsTable);err!=nil{return err}
 rows,err:=db.Query("PRAGMA table_info(sessions)")
 if err!=nil{return err}
 existing:=make(map[string]bool,9)
 for rows.Next(){
  var index,required,pk int
  var name,typ string
  var def sql.NullString
  if err:=rows.Scan(&index,&name,&typ,&required,&def,&pk);err!=nil{rows.Close();return err}
  existing[name]=true
 }
 if err:=rows.Err();err!=nil{rows.Close();return err}
 if err:=rows.Close();err!=nil{return err}
 for _,column:=range []struct{name,definition string}{
  {"ip","TEXT NOT NULL DEFAULT ''"},
  {"user_agent","TEXT NOT NULL DEFAULT ''"},
  {"created_at","INTEGER NOT NULL DEFAULT 0"},
  {"last_seen","INTEGER NOT NULL DEFAULT 0"},
  {"watching_hash","TEXT NOT NULL DEFAULT ''"},
  {"watching_position","REAL NOT NULL DEFAULT 0"},
  {"watching_updated_at","INTEGER NOT NULL DEFAULT 0"},
 }{
  if existing[column.name]{continue}
  if _,err:=db.Exec("ALTER TABLE sessions ADD COLUMN "+column.name+" "+column.definition);err!=nil{return err}
 }
 // Rows from before this migration have no timestamps. Derive them from the
 // expiry instead of showing the Unix epoch in the Sessions panel.
 _,err=db.Exec("UPDATE sessions SET created_at=MAX(0,expires_at-?),last_seen=MAX(0,expires_at-?) WHERE created_at=0 AND last_seen=0",int64(sessionLifetime/time.Second),int64(sessionLifetime/time.Second))
 return err
}

func sessionHash(r *http.Request)(string,bool){
 cookie,err:=r.Cookie("cloudflix_session")
 if err!=nil || len(cookie.Value)!=64{return "",false}
 return hashToken(cookie.Value),true
}

func userAgent(r *http.Request)string{
 agent:=strings.TrimSpace(r.UserAgent())
 if len(agent)>maxUserAgentBytes{
  cut:=maxUserAgentBytes
  for cut>0 && !utf8.RuneStart(agent[cut]){cut--}
  agent=agent[:cut]
 }
 return agent
}

func shortSessionID(tokenHash string)string{
 if len(tokenHash)<=shortSessionIDLength{return tokenHash}
 return tokenHash[:shortSessionIDLength]
}

// movieInfo lets tests describe the movie catalog without a live torrent client.
// It reports the title, whether the download is finished, and whether the
// infohash is in the catalog at all.
func (a *application) movie(hash string)(string,bool,bool){
 if a.movieInfo!=nil{return a.movieInfo(hash)}
 if a.engine==nil{return "",false,false}
 return a.engine.movieState(hash)
}

// Only a finished download offers a resumable position.
func (e *engine) movieState(hash string)(string,bool,bool){
 e.mu.Lock()
 defer e.mu.Unlock()
 item:=e.entries[hash]
 if item==nil || item.rejectionReason!=""{return "",false,false}
 title:=item.title
 tt:=item.torrent
 if tt==nil || tt.Info()==nil{return title,false,true}
 if title==""{title=tt.Name()}
 if !item.validated{return title,false,true}
 size,completed,_:=selectedFileStats(tt)
 if size<=0 || completed<size || !selectedFilesVerified(tt){return title,false,true}
 return title,true,true
}

func (a *application) resumeView(hash string,position float64,updatedAt int64)any{
 if hash=="" || !(position>0){return nil}
 title,completed,_:=a.movie(hash)
 if !completed{return nil}
 return resumeState{Hash:hash,Position:position,Title:title,UpdatedAt:updatedAt}
}

func (a *application) sessionResume(w http.ResponseWriter,r *http.Request){
 sum,ok:=sessionHash(r)
 if !ok{fail(w,http.StatusUnauthorized,"PIN authentication required");return}
 var hash string
 var position float64
 var updatedAt int64
 err:=a.db.QueryRowContext(r.Context(),"SELECT watching_hash,watching_position,watching_updated_at FROM sessions WHERE token_hash=?",sum).Scan(&hash,&position,&updatedAt)
 if err!=nil && !errors.Is(err,sql.ErrNoRows){fail(w,http.StatusInternalServerError,"session unavailable");return}
 respond(w,http.StatusOK,map[string]any{"resume":a.resumeView(hash,position,updatedAt)})
}

func (a *application) sessionWatching(w http.ResponseWriter,r *http.Request){
 sum,ok:=sessionHash(r)
 if !ok{fail(w,http.StatusUnauthorized,"PIN authentication required");return}
 var req struct{
  Hash string `json:"hash"`
  Position float64 `json:"position"`
 }
 if !readJSON(w,r,&req){return}
 hash:=req.Hash
 position:=req.Position
 if !(position>0) || math.IsInf(position,0){position=0}
 if position>maxWatchingPosition{position=maxWatchingPosition}
 var updatedAt int64
 if hash!="" && position>0{
  // A movie that is still downloading is known but not resumable yet; only an
  // infohash absent from the catalog is rejected.
  if _,_,known:=a.movie(hash);!known{fail(w,http.StatusBadRequest,"unknown movie");return}
  updatedAt=time.Now().Unix()
 }else{
  hash=""
  position=0
 }
 if _,err:=a.db.ExecContext(r.Context(),"UPDATE sessions SET watching_hash=?,watching_position=?,watching_updated_at=? WHERE token_hash=?",hash,position,updatedAt,sum);err!=nil{
  fail(w,http.StatusInternalServerError,"unable to save playback position");return
 }
 respond(w,http.StatusOK,map[string]bool{"ok":true})
}

func (a *application) sessionList(w http.ResponseWriter,r *http.Request){
 sum,ok:=sessionHash(r)
 if !ok{fail(w,http.StatusUnauthorized,"PIN authentication required");return}
 rows,err:=a.db.QueryContext(r.Context(),"SELECT token_hash,ip,user_agent,created_at,last_seen,watching_hash,watching_position,watching_updated_at FROM sessions WHERE expires_at>?",time.Now().Unix())
 if err!=nil{fail(w,http.StatusInternalServerError,"sessions unavailable");return}
 sessions:=make([]sessionView,0)
 for rows.Next(){
  var tokenHash,ip,agent,hash string
  var createdAt,lastSeen,updatedAt int64
  var position float64
  if err:=rows.Scan(&tokenHash,&ip,&agent,&createdAt,&lastSeen,&hash,&position,&updatedAt);err!=nil{
   rows.Close()
   fail(w,http.StatusInternalServerError,"sessions unavailable");return
  }
  sessions=append(sessions,sessionView{ID:shortSessionID(tokenHash),Current:tokenHash==sum,IP:ip,UserAgent:agent,CreatedAt:createdAt,LastSeen:lastSeen,Status:"idle",hash:hash,position:position,updatedAt:updatedAt})
 }
 if err:=rows.Err();err!=nil{rows.Close();fail(w,http.StatusInternalServerError,"sessions unavailable");return}
 rows.Close()
 // Resolve titles only after the cursor is closed: the database has a single
 // connection, and the movie lookup may itself need it.
 now:=time.Now().Unix()
 titles:=make(map[string]string,4)
 for i:=range sessions{
  view:=&sessions[i]
  if view.hash=="" || !(view.position>0){continue}
  title,known:=titles[view.hash]
  if !known{
   completed:=false
   title,completed,_=a.movie(view.hash)
   if completed{titles[view.hash]=title}else{title=""}
  }
  // Only claim "watching" when the movie can still be resolved, so the UI never
  // renders a Watching pill with no title beside it.
  if title==""{continue}
  view.Watching=&watchingState{Hash:view.hash,Title:title,Position:view.position}
  if view.updatedAt>0 && now-view.updatedAt<=watchingActiveSeconds{view.Status="watching"}
 }
 sort.SliceStable(sessions,func(i,j int)bool{
  if sessions[i].Current!=sessions[j].Current{return sessions[i].Current}
  if sessions[i].LastSeen!=sessions[j].LastSeen{return sessions[i].LastSeen>sessions[j].LastSeen}
  return sessions[i].ID<sessions[j].ID
 })
 respond(w,http.StatusOK,map[string]any{"sessions":sessions})
}

// Session identifiers are the short prefix of the stored token hash, so the
// opaque cookie value can never be replayed from the API.
func (a *application) sessionRevoke(w http.ResponseWriter,r *http.Request){
 sum,ok:=sessionHash(r)
 if !ok{fail(w,http.StatusUnauthorized,"PIN authentication required");return}
 id:=strings.ToLower(r.PathValue("id"))
 if !sessionIDPattern.MatchString(id){fail(w,http.StatusBadRequest,"invalid session id");return}
 var tokenHash string
 err:=a.db.QueryRowContext(r.Context(),"SELECT token_hash FROM sessions WHERE substr(token_hash,1,?)=? LIMIT 1",shortSessionIDLength,id).Scan(&tokenHash)
 if errors.Is(err,sql.ErrNoRows){fail(w,http.StatusNotFound,"session not found");return}
 if err!=nil{fail(w,http.StatusInternalServerError,"sessions unavailable");return}
 if tokenHash==sum{fail(w,http.StatusBadRequest,"cannot revoke the current session");return}
 if _,err:=a.db.ExecContext(r.Context(),"DELETE FROM sessions WHERE token_hash=?",tokenHash);err!=nil{
  fail(w,http.StatusInternalServerError,"unable to revoke session");return
 }
 respond(w,http.StatusOK,map[string]bool{"ok":true})
}
