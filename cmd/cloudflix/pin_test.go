package main

import (
 "database/sql"
 "net/http"
 "net/http/httptest"
 "path/filepath"
 "strings"
 "testing"
 "time"

 "github.com/alexedwards/argon2id"
 _ "modernc.org/sqlite"
)

func testPINDB(t *testing.T) *sql.DB {
 t.Helper()
 db,err:=sql.Open("sqlite",filepath.Join(t.TempDir(),"auth.db"))
 if err!=nil{t.Fatal(err)}
 db.SetMaxOpenConns(1)
 t.Cleanup(func(){db.Close()})
 return db
}

func TestPINMigrationAndPersistence(t *testing.T) {
 db:=testPINDB(t)
 if _,err:=db.Exec("CREATE TABLE users(id INTEGER PRIMARY KEY CHECK(id=1),password_hash TEXT NOT NULL)");err!=nil{t.Fatal(err)}
 if _,err:=db.Exec("INSERT INTO users(id,password_hash) VALUES (1,'legacy-password-hash')");err!=nil{t.Fatal(err)}
 if err:=ensurePINSchema(db);err!=nil{t.Fatal(err)}
 if pinConfigured(db){t.Fatal("legacy password must not enable PIN login")}
 if err:=ensurePINSchema(db);err!=nil{t.Fatal("migration must be idempotent",err)}
 if _,err:=db.Exec("UPDATE users SET pin_enabled=1 WHERE id=1");err!=nil{t.Fatal(err)}
 if !pinConfigured(db){t.Fatal("PIN activation must persist in existing user record")}
}

func TestPINRateLimitPersistsInSQLite(t *testing.T){
 db:=testPINDB(t)
 if err:=ensurePINSchema(db);err!=nil{t.Fatal(err)}
 a:=&application{db:db}
 for i:=0;i<4;i++{
  if err:=a.pinFailure();err!=nil{t.Fatal(err)}
  locked,_,err:=a.pinLocked()
  if err!=nil || locked{t.Fatalf("early lock at attempt %d: %v",i+1,err)}
 }
 if err:=a.pinFailure();err!=nil{t.Fatal(err)}
 locked,until,err:=a.pinLocked()
 if err!=nil || !locked || until<=time.Now().Unix(){t.Fatal("fifth incorrect attempt must lock PIN login")}
 a2:=&application{db:db}
 locked,_,err=a2.pinLocked()
 if err!=nil || !locked{t.Fatal("lock must persist across app instances")}
 if err:=a2.clearPinFailures();err!=nil{t.Fatal(err)}
 locked,_,err=a2.pinLocked()
 if err!=nil || locked{t.Fatal("successful PIN entry should clear lockout")}
}

func TestPINLogin(t *testing.T){
 db:=testPINDB(t)
 if err:=ensurePINSchema(db);err!=nil{t.Fatal(err)}
 hash,err:=argon2id.CreateHash("007123",argon2id.DefaultParams)
 if err!=nil{t.Fatal(err)}
 if _,err:=db.Exec("INSERT INTO users(id,password_hash,pin_enabled) VALUES(1,?,1)",hash);err!=nil{t.Fatal(err)}
 a:=&application{db:db,attempts:map[string][]time.Time{}}
 for _,tc:=range []struct{body string; status int}{
  {`{"pin":"007123"}`,200},
  {`{"pin":"007124"}`,401},
  {`{"password":"007123"}`,400},
  {`{"pin":"not6"}`,401},
 }{
  req:=httptest.NewRequest("POST","http://localhost:3000/api/auth/login",strings.NewReader(tc.body))
  req.Header.Set("Content-Type","application/json")
  rr:=httptest.NewRecorder()
  a.login(rr,req)
  if rr.Code!=tc.status{t.Errorf("%s got %d expected %d (%s)",tc.body,rr.Code,tc.status,rr.Body.String())}
 }
}

func TestPINLoginOverPort80SetsValidSession(t *testing.T){
 db:=testPINDB(t)
 if err:=ensurePINSchema(db);err!=nil{t.Fatal(err)}
 hash,err:=argon2id.CreateHash("005501",argon2id.DefaultParams)
 if err!=nil{t.Fatal(err)}
 if _,err:=db.Exec("INSERT INTO users(id,password_hash,pin_enabled) VALUES (1,?,1)",hash);err!=nil{t.Fatal(err)}
 a:=&application{db:db,attempts:map[string][]time.Time{}}
 handler:=securityHeaders(http.HandlerFunc(a.login))
 req:=httptest.NewRequest(http.MethodPost,"http://127.0.0.1:80/api/auth/login",strings.NewReader(`{"pin":"005501"}`))
 req.Header.Set("Origin","http://127.0.0.1")
 req.Header.Set("Content-Type","application/json")
 resp:=httptest.NewRecorder()
 handler.ServeHTTP(resp,req)
 if resp.Code!=200{t.Fatalf("PIN login failed with HTTP %d: %s",resp.Code,resp.Body.String())}
 cookies:=resp.Result().Cookies()
 if len(cookies)!=1 || cookies[0].Name!="cloudflix_session" || !cookies[0].HttpOnly {
  t.Fatalf("login did not set a secure session cookie: %+v",cookies)
 }
 statusReq:=httptest.NewRequest("GET","http://127.0.0.1/api/auth/status",nil)
 statusReq.AddCookie(cookies[0])
 if !a.authenticated(statusReq){t.Fatal("session created by PIN login is not recognized")}
}
