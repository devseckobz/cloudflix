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
