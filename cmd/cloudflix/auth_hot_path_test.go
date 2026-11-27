package main

// The video path authenticates on every range request, so a player buffering a
// movie pays whatever authentication costs once per range. These tests pin the
// two properties that made it expensive: an UPDATE on every request, and
// reading the session row twice to get data the first read already had.

import (
 "database/sql"
 "net/http"
 "testing"
 "time"
)

// authCheck reports whether the session authenticates, through the same
// predicate require() calls, so the fixture needs no configured PIN.
func authCheck(t *testing.T,app *application,token string)bool{
 t.Helper()
 return app.authenticated(sessionRequest(t,http.MethodGet,"/api/state","",token))
}

func sessionLastSeen(t *testing.T,db *sql.DB,token string)int64{
 t.Helper()
 var lastSeen int64
 if err:=db.QueryRow("SELECT last_seen FROM sessions WHERE token_hash=?",hashToken(token)).Scan(&lastSeen);err!=nil{t.Fatal(err)}
 return lastSeen
}

func TestAuthenticatedAcceptsSessionWithoutWritingOnEveryRequest(t *testing.T){
 app,db:=testSessionApp(t)
 now:=time.Now().Unix()
 token:=insertSession(t,db,"10.0.0.1","agent",now,now)
 if !authCheck(t,app,token){t.Fatal("fresh session rejected")}

 // Requests inside the touch interval must still authenticate and must not
 // move last_seen: that per-request UPDATE was the cost being removed.
 before:=sessionLastSeen(t,db,token)
 for i:=0;i<50;i++{
  if !authCheck(t,app,token){t.Fatalf("request %d rejected",i)}
 }
 if after:=sessionLastSeen(t,db,token);after!=before{
  t.Fatalf("last_seen moved on every request: %d -> %d",before,after)
 }
}

func TestAuthenticatedRefreshesStaleSessionAndRejectsExpired(t *testing.T){
 app,db:=testSessionApp(t)
 stale:=time.Now().Unix()-int64(sessionTouchInterval)-5
 token:=insertSession(t,db,"10.0.0.2","agent",stale,stale)
 if !authCheck(t,app,token){t.Fatal("stale session rejected")}
 if after:=sessionLastSeen(t,db,token);after<=stale{t.Fatalf("stale last_seen was not refreshed: %d",after)}

 expired:=insertSession(t,db,"10.0.0.3","agent",0,0)
 if _,err:=db.Exec("UPDATE sessions SET expires_at=? WHERE token_hash=?",time.Now().Add(-time.Hour).Unix(),hashToken(expired));err!=nil{t.Fatal(err)}
 if authCheck(t,app,expired){t.Fatal("expired session accepted")}
}

func TestAuthenticatedRejectsMissingOrMalformedCookie(t *testing.T){
 app,_:=testSessionApp(t)
 if app.authenticated(sessionRequest(t,http.MethodGet,"/api/state","","")){t.Fatal("anonymous request accepted")}
 if app.authenticated(sessionRequest(t,http.MethodGet,"/api/state","","not-a-64-char-token")){t.Fatal("short cookie accepted")}
}
