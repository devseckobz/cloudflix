package main

import (
 "net/http"
 "net/http/httptest"
 "strings"
 "testing"
)

func TestTMDBSettingsSecretNeverReturned(t *testing.T) {
 db:=testPINDB(t)
 if err:=ensureSettingsSchema(db);err!=nil{t.Fatal(err)}
 a:=&application{db:db,engine:&engine{tmdbKey:"VERY_SECRET_TEST_TOKEN",entries:make(map[string]*entry)}}
 w:=httptest.NewRecorder()
 a.getSettings(w,httptest.NewRequest(http.MethodGet,"/api/settings",nil))
 if w.Code!=http.StatusOK || !strings.Contains(w.Body.String(),`"tmdbConfigured":true`){t.Fatalf("unexpected response: %d %s",w.Code,w.Body.String())}
 if strings.Contains(w.Body.String(),"VERY_SECRET_TEST_TOKEN"){t.Fatal("TMDB secret leaked in Settings API response")}
}
func TestClearTMDBSettings(t *testing.T) {
 db:=testPINDB(t)
 if err:=ensureSettingsSchema(db);err!=nil{t.Fatal(err)}
 a:=&application{db:db,engine:&engine{tmdbKey:"VERY_SECRET_TEST_TOKEN",entries:make(map[string]*entry)}}
 r:=httptest.NewRequest(http.MethodPut,"/api/settings",strings.NewReader(`{"tmdbApiKey":""}`))
 r.Header.Set("Content-Type","application/json")
 w:=httptest.NewRecorder()
 a.saveSettings(w,r)
 if w.Code!=http.StatusOK{t.Fatalf("save failed: %d %s",w.Code,w.Body.String())}
 key,err:=loadTMDBKey(db,"a-legacy-key")
 if err!=nil{t.Fatal(err)}
 if key!="" || a.engine.tmdbKey!=""{t.Fatal("TMDB credential was not cleared")}
 if strings.Contains(w.Body.String(),"VERY_SECRET_TEST_TOKEN"){t.Fatal("TMDB secret was returned to browser")}
}
func TestTMDBTokenValidationPattern(t *testing.T) {
 for _,bad:=range []string{"","short","example token with spaces","../naughty","\ninvalid"} {
  if validTMDBToken(bad){t.Errorf("accepted invalid bearer token %q",bad)}
 }
 if !validTMDBToken(strings.Repeat("A",80)){t.Fatal("rejected standard token character set")}
}

func TestTMDBCredentialStyles(t *testing.T){
 key:=strings.Repeat("a",32)
 if !isTMDBV3Key(key) || !validTMDBToken(key){t.Fatal("v3 API key rejected")}
 req:=httptest.NewRequest(http.MethodGet,"https://api.themoviedb.org/3/search/movie?query=Test",nil)
 applyTMDBAuthentication(req,key)
 if req.URL.Query().Get("api_key")!=key{t.Fatal("v3 key was not passed as api_key")}
 if req.Header.Get("Authorization")!=""{t.Fatal("v3 key must not be passed as bearer")}
 bearer:="eyJ."+strings.Repeat("b",40)+"."+strings.Repeat("c",40)
 req2:=httptest.NewRequest(http.MethodGet,"https://api.themoviedb.org/3/search/movie",nil)
 applyTMDBAuthentication(req2,bearer)
 if req2.Header.Get("Authorization")!="Bearer "+bearer{t.Fatal("bearer token header missing")}
 if req2.URL.Query().Has("api_key"){t.Fatal("v4 access token must not appear in URL")}
}
