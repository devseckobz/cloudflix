package main

import (
 "database/sql"
 "encoding/json"
 "net/http"
 "net/http/httptest"
 "strings"
 "testing"
 "time"
)

const (
 testMovieHash="0123456789abcdef0123456789abcdef01234567"
 testDownloadingHash="fedcba9876543210fedcba9876543210fedcba98"
)

func testSessionApp(t *testing.T)(*application,*sql.DB){
 t.Helper()
 db:=testPINDB(t)
 if err:=ensureSessionsSchema(db);err!=nil{t.Fatal(err)}
 a:=&application{db:db,movieInfo:func(hash string)(string,bool,bool){
  switch hash{
  case testMovieHash:return "Example Movie",true,true
  case testDownloadingHash:return "Downloading Movie",false,true
  }
  return "",false,false
 }}
 return a,db
}

// insertSession stores a session row and returns the raw cookie token.
func insertSession(t *testing.T,db *sql.DB,ip,agent string,createdAt,lastSeen int64)string{
 t.Helper()
 token,err:=randomToken()
 if err!=nil{t.Fatal(err)}
 if _,err:=db.Exec("INSERT INTO sessions(token_hash,expires_at,ip,user_agent,created_at,last_seen) VALUES(?,?,?,?,?,?)",hashToken(token),time.Now().Add(sessionLifetime).Unix(),ip,agent,createdAt,lastSeen);err!=nil{t.Fatal(err)}
 return token
}

func sessionRequest(t *testing.T,method,path,body,token string)*http.Request{
 t.Helper()
 r:=httptest.NewRequest(method,path,strings.NewReader(body))
 if body!=""{r.Header.Set("Content-Type","application/json")}
 if token!=""{r.AddCookie(&http.Cookie{Name:"cloudflix_session",Value:token})}
 return r
}

func TestSessionsMigrationKeepsLegacyRows(t *testing.T){
 db:=testPINDB(t)
 if _,err:=db.Exec("CREATE TABLE sessions (token_hash TEXT PRIMARY KEY, expires_at INTEGER NOT NULL)");err!=nil{t.Fatal(err)}
 legacyExpiry:=time.Now().Add(48*time.Hour).Unix()
 token,err:=randomToken()
 if err!=nil{t.Fatal(err)}
 if _,err:=db.Exec("INSERT INTO sessions(token_hash,expires_at) VALUES(?,?)",hashToken(token),legacyExpiry);err!=nil{t.Fatal(err)}
 if err:=ensureSessionsSchema(db);err!=nil{t.Fatal(err)}
 if err:=ensureSessionsSchema(db);err!=nil{t.Fatalf("migration must be idempotent: %v",err)}
 var ip,agent,hash string
 var createdAt,lastSeen int64
 var position float64
 if err:=db.QueryRow("SELECT ip,user_agent,created_at,last_seen,watching_hash,watching_position FROM sessions WHERE token_hash=?",hashToken(token)).Scan(&ip,&agent,&createdAt,&lastSeen,&hash,&position);err!=nil{
  t.Fatalf("legacy session was lost: %v",err)
 }
 if ip!="" || agent!="" || hash!="" || position!=0{t.Fatalf("unexpected legacy defaults: %q %q %q %v",ip,agent,hash,position)}
 if createdAt<=0 || lastSeen<=0{t.Fatalf("legacy timestamps were not derived: %d %d",createdAt,lastSeen)}
 if createdAt>legacyExpiry{t.Fatalf("derived created_at %d is after expiry %d",createdAt,legacyExpiry)}
 // The pre-upgrade login must keep working, not be signed out by the migration.
 a:=&application{db:db}
 if !a.authenticated(sessionRequest(t,http.MethodGet,"/api/state","",token)){t.Fatal("legacy session stopped authenticating after migration")}
}

func TestSessionResumeRoundTrip(t *testing.T){
 a,db:=testSessionApp(t)
 token:=insertSession(t,db,"203.0.113.9","TestBrowser/1.0",time.Now().Unix(),time.Now().Unix())
 recorder:=httptest.NewRecorder()
 a.sessionWatching(recorder,sessionRequest(t,http.MethodPost,"/api/session/watching",`{"hash":"`+testMovieHash+`","position":42.5}`,token))
 if recorder.Code!=http.StatusOK{t.Fatalf("save failed: %d %s",recorder.Code,recorder.Body.String())}
 recorder=httptest.NewRecorder()
 a.sessionResume(recorder,sessionRequest(t,http.MethodGet,"/api/session","",token))
 if recorder.Code!=http.StatusOK{t.Fatalf("resume failed: %d %s",recorder.Code,recorder.Body.String())}
 var body struct{Resume *resumeState `json:"resume"`}
 if err:=json.Unmarshal(recorder.Body.Bytes(),&body);err!=nil{t.Fatal(err)}
 if body.Resume==nil{t.Fatalf("expected resume state, got %s",recorder.Body.String())}
 if body.Resume.Hash!=testMovieHash || body.Resume.Position!=42.5 || body.Resume.Title!="Example Movie" || body.Resume.UpdatedAt<=0{
  t.Fatalf("unexpected resume payload: %+v",*body.Resume)
 }
}

func TestSessionResumeClearsAndRejectsUnknownMovie(t *testing.T){
 a,db:=testSessionApp(t)
 token:=insertSession(t,db,"203.0.113.9","TestBrowser/1.0",time.Now().Unix(),time.Now().Unix())
 for _,body:=range []string{
  `{"hash":"`+testMovieHash+`","position":0}`,
  `{"hash":"","position":30}`,
  `{"hash":"`+testMovieHash+`","position":-5}`,
 }{
  recorder:=httptest.NewRecorder()
  a.sessionWatching(recorder,sessionRequest(t,http.MethodPost,"/api/session/watching",body,token))
  if recorder.Code!=http.StatusOK{t.Fatalf("%s rejected: %d %s",body,recorder.Code,recorder.Body.String())}
  recorder=httptest.NewRecorder()
  a.sessionResume(recorder,sessionRequest(t,http.MethodGet,"/api/session","",token))
  if !strings.Contains(recorder.Body.String(),`"resume":null`){t.Fatalf("%s did not clear resume: %s",body,recorder.Body.String())}
 }
 recorder:=httptest.NewRecorder()
 a.sessionWatching(recorder,sessionRequest(t,http.MethodPost,"/api/session/watching",`{"hash":"`+strings.Repeat("f",40)+`","position":10}`,token))
 if recorder.Code!=http.StatusBadRequest{t.Fatalf("unknown movie accepted: %d %s",recorder.Code,recorder.Body.String())}
 recorder=httptest.NewRecorder()
 a.sessionWatching(recorder,sessionRequest(t,http.MethodPost,"/api/session/watching",`{"hash":"not-a-hash","position":10}`,token))
 if recorder.Code!=http.StatusBadRequest{t.Fatalf("malformed hash accepted: %d %s",recorder.Code,recorder.Body.String())}
 // A movie that is still downloading is a known hash: the position is stored,
 // but GET /api/session must not offer it as a resume point yet.
 recorder=httptest.NewRecorder()
 a.sessionWatching(recorder,sessionRequest(t,http.MethodPost,"/api/session/watching",`{"hash":"`+testDownloadingHash+`","position":10}`,token))
 if recorder.Code!=http.StatusOK{t.Fatalf("downloading movie rejected: %d %s",recorder.Code,recorder.Body.String())}
 recorder=httptest.NewRecorder()
 a.sessionResume(recorder,sessionRequest(t,http.MethodGet,"/api/session","",token))
 if !strings.Contains(recorder.Body.String(),`"resume":null`){t.Fatalf("unfinished movie offered a resume: %s",recorder.Body.String())}
}

func TestSessionWatchingClampsHostilePositions(t *testing.T){
 a,db:=testSessionApp(t)
 token:=insertSession(t,db,"203.0.113.9","TestBrowser/1.0",time.Now().Unix(),time.Now().Unix())
 for _,body:=range []string{
  `{"hash":"`+testMovieHash+`","position":1e18}`,
  `{"hash":"`+testMovieHash+`","position":-1e18}`,
 }{
  recorder:=httptest.NewRecorder()
  a.sessionWatching(recorder,sessionRequest(t,http.MethodPost,"/api/session/watching",body,token))
  if recorder.Code!=http.StatusOK{t.Fatalf("%s rejected: %d %s",body,recorder.Code,recorder.Body.String())}
 }
 var position float64
 if err:=db.QueryRow("SELECT watching_position FROM sessions WHERE token_hash=?",hashToken(token)).Scan(&position);err!=nil{t.Fatal(err)}
 if !(position>=0) || position>maxWatchingPosition{t.Fatalf("position was not clamped: %v",position)}
}

func TestSessionResumeHidesUnfinishedMovie(t *testing.T){
 a,db:=testSessionApp(t)
 token:=insertSession(t,db,"203.0.113.9","TestBrowser/1.0",time.Now().Unix(),time.Now().Unix())
 if _,err:=db.Exec("UPDATE sessions SET watching_hash=?,watching_position=?,watching_updated_at=? WHERE token_hash=?",testMovieHash,12.0,time.Now().Unix(),hashToken(token));err!=nil{t.Fatal(err)}
 a.movieInfo=func(string)(string,bool,bool){return "",false,false}
 recorder:=httptest.NewRecorder()
 a.sessionResume(recorder,sessionRequest(t,http.MethodGet,"/api/session","",token))
 if !strings.Contains(recorder.Body.String(),`"resume":null`){t.Fatalf("unfinished movie offered a resume: %s",recorder.Body.String())}
}

func TestSessionListStatusAndCurrentFlag(t *testing.T){
 a,db:=testSessionApp(t)
 now:=time.Now().Unix()
 current:=insertSession(t,db,"198.51.100.7","CurrentBrowser/1.0",now-60,now)
 idle:=insertSession(t,db,"198.51.100.8","IdleBrowser/1.0",now-600,now-120)
 stale:=insertSession(t,db,"198.51.100.9","StaleBrowser/1.0",now-900,now-300)
 if _,err:=db.Exec("UPDATE sessions SET watching_hash=?,watching_position=?,watching_updated_at=? WHERE token_hash=?",testMovieHash,90.0,now,hashToken(current));err!=nil{t.Fatal(err)}
 if _,err:=db.Exec("UPDATE sessions SET watching_hash=?,watching_position=?,watching_updated_at=? WHERE token_hash=?",testMovieHash,30.0,now-300,hashToken(idle));err!=nil{t.Fatal(err)}
 if _,err:=db.Exec("UPDATE sessions SET watching_hash=?,watching_position=?,watching_updated_at=? WHERE token_hash=?",testMovieHash,10.0,now-121,hashToken(stale));err!=nil{t.Fatal(err)}
 recorder:=httptest.NewRecorder()
 a.sessionList(recorder,sessionRequest(t,http.MethodGet,"/api/sessions","",current))
 if recorder.Code!=http.StatusOK{t.Fatalf("list failed: %d %s",recorder.Code,recorder.Body.String())}
 var body struct{Sessions []sessionView `json:"sessions"`}
 if err:=json.Unmarshal(recorder.Body.Bytes(),&body);err!=nil{t.Fatal(err)}
 if len(body.Sessions)!=3{t.Fatalf("expected 3 sessions, got %d",len(body.Sessions))}
 if !body.Sessions[0].Current || body.Sessions[0].ID!=shortSessionID(hashToken(current)){t.Fatalf("current session must sort first: %+v",body.Sessions[0])}
 if body.Sessions[0].Status!="watching" || body.Sessions[0].Watching==nil || body.Sessions[0].Watching.Title!="Example Movie"{
  t.Fatalf("current session should be watching: %+v",body.Sessions[0])
 }
 if body.Sessions[1].ID!=shortSessionID(hashToken(idle)) || body.Sessions[1].Status!="idle"{t.Fatalf("idle session wrong: %+v",body.Sessions[1])}
 if body.Sessions[2].ID!=shortSessionID(hashToken(stale)) || body.Sessions[2].Status!="idle"{t.Fatalf("stale session must be idle: %+v",body.Sessions[2])}
 if body.Sessions[1].Watching==nil || body.Sessions[1].Watching.Position!=30{t.Fatalf("idle session should still report its movie: %+v",body.Sessions[1])}
 if body.Sessions[0].IP!="198.51.100.7" || body.Sessions[0].UserAgent!="CurrentBrowser/1.0"{t.Fatalf("session metadata missing: %+v",body.Sessions[0])}
 if len(body.Sessions[0].ID)!=16{t.Fatalf("session id must be 16 hex chars: %q",body.Sessions[0].ID)}
}

// A session whose movie can no longer be resolved must not claim to be
// watching: the UI would render a Watching pill with no title beside it.
func TestSessionListDoesNotClaimWatchingWithoutResolvableMovie(t *testing.T){
 a,db:=testSessionApp(t)
 now:=time.Now().Unix()
 current:=insertSession(t,db,"198.51.100.7","CurrentBrowser/1.0",now,now)
 gone:=strings.Repeat("c",40)
 if _,err:=db.Exec("UPDATE sessions SET watching_hash=?,watching_position=?,watching_updated_at=? WHERE token_hash=?",gone,120.0,now,hashToken(current));err!=nil{t.Fatal(err)}
 recorder:=httptest.NewRecorder()
 a.sessionList(recorder,sessionRequest(t,http.MethodGet,"/api/sessions","",current))
 if recorder.Code!=http.StatusOK{t.Fatalf("list failed: %d %s",recorder.Code,recorder.Body.String())}
 var body struct{Sessions []sessionView `json:"sessions"`}
 if err:=json.Unmarshal(recorder.Body.Bytes(),&body);err!=nil{t.Fatal(err)}
 if len(body.Sessions)!=1{t.Fatalf("expected 1 session, got %d",len(body.Sessions))}
 if body.Sessions[0].Status!="idle"{t.Fatalf("unresolvable movie must not report watching: %+v",body.Sessions[0])}
 if body.Sessions[0].Watching!=nil{t.Fatalf("unresolvable movie must not report a watching object: %+v",body.Sessions[0])}
}

func TestSessionRevokeRefusesCurrentSession(t *testing.T){
 a,db:=testSessionApp(t)
 now:=time.Now().Unix()
 current:=insertSession(t,db,"198.51.100.7","CurrentBrowser/1.0",now,now)
 other:=insertSession(t,db,"198.51.100.8","OtherBrowser/1.0",now,now)
 recorder:=httptest.NewRecorder()
 request:=sessionRequest(t,http.MethodDelete,"/api/sessions/"+shortSessionID(hashToken(current)),"",current)
 request.SetPathValue("id",shortSessionID(hashToken(current)))
 a.sessionRevoke(recorder,request)
 if recorder.Code!=http.StatusBadRequest{t.Fatalf("current session was revoked: %d %s",recorder.Code,recorder.Body.String())}
 recorder=httptest.NewRecorder()
 request=sessionRequest(t,http.MethodDelete,"/api/sessions/"+shortSessionID(hashToken(other)),"",current)
 request.SetPathValue("id",shortSessionID(hashToken(other)))
 a.sessionRevoke(recorder,request)
 if recorder.Code!=http.StatusOK{t.Fatalf("revoke failed: %d %s",recorder.Code,recorder.Body.String())}
 var remaining int
 if err:=db.QueryRow("SELECT COUNT(*) FROM sessions WHERE token_hash=?",hashToken(other)).Scan(&remaining);err!=nil{t.Fatal(err)}
 if remaining!=0{t.Fatal("revoked session still exists")}
 recorder=httptest.NewRecorder()
 request=sessionRequest(t,http.MethodDelete,"/api/sessions/"+shortSessionID(hashToken(other)),"",current)
 request.SetPathValue("id",shortSessionID(hashToken(other)))
 a.sessionRevoke(recorder,request)
 if recorder.Code!=http.StatusNotFound{t.Fatalf("unknown session id should be 404, got %d",recorder.Code)}
 recorder=httptest.NewRecorder()
 request=sessionRequest(t,http.MethodDelete,"/api/sessions/not-hex","",current)
 request.SetPathValue("id","not-hex")
 a.sessionRevoke(recorder,request)
 if recorder.Code!=http.StatusBadRequest{t.Fatalf("invalid session id should be 400, got %d",recorder.Code)}
}

func TestIssueSessionRecordsClientAndLastSeen(t *testing.T){
 a,db:=testSessionApp(t)
 request:=httptest.NewRequest(http.MethodPost,"http://198.51.100.20/api/auth/login",strings.NewReader("{}"))
 request.RemoteAddr="198.51.100.20:54321"
 request.Header.Set("User-Agent","CloudflixTest/2.0")
 recorder:=httptest.NewRecorder()
 a.issueSession(recorder,request)
 if recorder.Code!=http.StatusOK{t.Fatalf("issue failed: %d %s",recorder.Code,recorder.Body.String())}
 cookies:=recorder.Result().Cookies()
 if len(cookies)!=1{t.Fatalf("expected one cookie, got %d",len(cookies))}
 var ip,agent string
 var createdAt,lastSeen int64
 if err:=db.QueryRow("SELECT ip,user_agent,created_at,last_seen FROM sessions WHERE token_hash=?",hashToken(cookies[0].Value)).Scan(&ip,&agent,&createdAt,&lastSeen);err!=nil{t.Fatal(err)}
 if ip!="198.51.100.20" || agent!="CloudflixTest/2.0"{t.Fatalf("client metadata not recorded: %q %q",ip,agent)}
 if createdAt<=0 || lastSeen<=0{t.Fatalf("timestamps not recorded: %d %d",createdAt,lastSeen)}
 before:=lastSeen
 time.Sleep(1100*time.Millisecond)
 authenticated:=httptest.NewRequest(http.MethodGet,"/api/state",nil)
 authenticated.AddCookie(cookies[0])
 if !a.authenticated(authenticated){t.Fatal("issued session is not authenticated")}
 if err:=db.QueryRow("SELECT last_seen FROM sessions WHERE token_hash=?",hashToken(cookies[0].Value)).Scan(&lastSeen);err!=nil{t.Fatal(err)}
 if lastSeen<=before{t.Fatalf("last_seen was not refreshed: %d -> %d",before,lastSeen)}
}

// The browser sends DELETE without a JSON body, so the CSRF guard must not
// demand a Content-Type that fetch() would never set.
func TestSessionRevokeAcceptsBodylessDelete(t *testing.T){
 a,db:=testSessionApp(t)
 now:=time.Now().Unix()
 current:=insertSession(t,db,"198.51.100.7","CurrentBrowser/1.0",now,now)
 other:=insertSession(t,db,"198.51.100.8","OtherBrowser/1.0",now,now)
 handler:=securityHeaders(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
  r.SetPathValue("id",r.URL.Path[strings.LastIndex(r.URL.Path,"/")+1:])
  a.sessionRevoke(w,r)
 }))
 request:=httptest.NewRequest(http.MethodDelete,"http://198.51.100.20/api/sessions/"+shortSessionID(hashToken(other)),nil)
 request.Header.Set("Origin","http://198.51.100.20")
 request.AddCookie(&http.Cookie{Name:"cloudflix_session",Value:current})
 recorder:=httptest.NewRecorder()
 handler.ServeHTTP(recorder,request)
 if recorder.Code!=http.StatusOK{t.Fatalf("bodyless DELETE rejected: %d %s",recorder.Code,recorder.Body.String())}
 request=httptest.NewRequest(http.MethodDelete,"http://198.51.100.20/api/sessions/"+shortSessionID(hashToken(current)),nil)
 request.Header.Set("Origin","https://evil.example")
 request.AddCookie(&http.Cookie{Name:"cloudflix_session",Value:current})
 recorder=httptest.NewRecorder()
 handler.ServeHTTP(recorder,request)
 if recorder.Code!=http.StatusForbidden{t.Fatalf("cross-origin DELETE allowed: %d",recorder.Code)}
}

// movieState must distinguish "not in the catalog" from "known but not ready",
// without a live torrent client.
func TestEngineMovieStateKnowsPendingButNotFinished(t *testing.T){
 pending:=strings.Repeat("a",40)
 e:=&engine{entries:map[string]*entry{pending:{hash:pending,title:"Pending Movie"}}}
 if title,completed,known:=e.movieState(pending);!known || completed || title!="Pending Movie"{
  t.Fatalf("pending movie reported wrong state: %q %v %v",title,completed,known)
 }
 if _,_,known:=e.movieState(strings.Repeat("b",40));known{t.Fatal("missing movie must be unknown")}
 rejected:=strings.Repeat("c",40)
 e.entries[rejected]=&entry{hash:rejected,title:"Rejected Movie",rejectionReason:"no video"}
 if _,_,known:=e.movieState(rejected);known{t.Fatal("rejected movie must not be resumable")}
}

func TestSessionEndpointsRequireAuthentication(t *testing.T){
 a,_:=testSessionApp(t)
 for _,tc:=range []struct{name string;handler http.HandlerFunc}{
  {"resume",a.sessionResume},
  {"watching",a.sessionWatching},
  {"list",a.sessionList},
  {"revoke",a.sessionRevoke},
 }{
  recorder:=httptest.NewRecorder()
  request:=sessionRequest(t,http.MethodGet,"/api/session","", "")
  request.SetPathValue("id",strings.Repeat("a",16))
  tc.handler(recorder,request)
  if recorder.Code!=http.StatusUnauthorized{t.Errorf("%s without a cookie returned %d",tc.name,recorder.Code)}
 }
}
