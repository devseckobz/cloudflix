package main

// Authentication runs once per media range request, so its cost is multiplied
// by every chunk a player fetches. These benchmarks put a number on it: the
// query itself, and the whole authenticated range path.

import (
 "net/http"
 "net/http/httptest"
 "testing"
 "time"
)

func benchmarkAuthSession(b *testing.B)(*application,string){
 b.Helper()
 db:=testPINDB(&testing.T{})
 if err:=ensurePINSchema(db);err!=nil{b.Fatal(err)}
 if err:=ensureSessionsSchema(db);err!=nil{b.Fatal(err)}
 token,err:=randomToken()
 if err!=nil{b.Fatal(err)}
 now:=time.Now().Unix()
 if _,err:=db.Exec("INSERT INTO sessions(token_hash,expires_at,ip,user_agent,created_at,last_seen) VALUES(?,?,?,?,?,?)",hashToken(token),time.Now().Add(sessionLifetime).Unix(),"10.0.0.9","bench",now,now);err!=nil{
  b.Fatal(err)
 }
 return &application{db:db},token
}

// BenchmarkAuthQuery is one authenticated() call: the cost every range request
// pays before a single media byte moves.
func BenchmarkAuthQuery(b *testing.B){
 app,token:=benchmarkAuthSession(b)
 b.ReportAllocs()
 b.ResetTimer()
 for i:=0;i<b.N;i++{
  r:=httptest.NewRequest(http.MethodGet,"/api/state",nil)
  r.AddCookie(&http.Cookie{Name:"cloudflix_session",Value:token})
  if !app.authenticated(r){b.Fatal("auth failed")}
 }
}

// BenchmarkAuthQueryLegacy reproduces the previous implementation exactly:
// a SELECT of the token, then an unconditional UPDATE of last_seen. Measured
// side by side with BenchmarkAuthQuery so the comparison is one machine, one
// run, not two numbers from different moments.
func BenchmarkAuthQueryLegacy(b *testing.B){
 app,token:=benchmarkAuthSession(b)
 sum:=hashToken(token)
 b.ReportAllocs()
 b.ResetTimer()
 for i:=0;i<b.N;i++{
  r:=httptest.NewRequest(http.MethodGet,"/api/state",nil)
  var stored string
  if err:=app.db.QueryRowContext(r.Context(),"SELECT token_hash FROM sessions WHERE token_hash=? AND expires_at>?",sum,time.Now().Unix()).Scan(&stored);err!=nil{b.Fatal(err)}
  _,_=app.db.ExecContext(r.Context(),"UPDATE sessions SET last_seen=? WHERE token_hash=? AND last_seen<?",time.Now().Unix(),sum,time.Now().Unix())
 }
}
