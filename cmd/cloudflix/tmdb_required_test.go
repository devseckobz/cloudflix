package main

import (
 "errors"
 "net/http"
 "net/http/httptest"
 "strings"
 "testing"
)

func TestMagnetDownloadRequiresTMDBConfiguration(t *testing.T){
 magnet:="magnet:?xt=urn:btih:0123456789abcdef0123456789abcdef01234567"
 engine:=&engine{entries:make(map[string]*entry)}
 _,err:=engine.add(magnet)
 if !errors.Is(err,errTMDBRequired){t.Fatalf("expected TMDB prerequisite, got %v",err)}
 if len(engine.entries)!=0{t.Fatal("download was accepted without TMDB key")}
 a:=&application{engine:engine}
 req:=httptest.NewRequest(http.MethodPost,"http://localhost/api/magnets",strings.NewReader(`{"magnet":"`+magnet+`"}`))
 req.Header.Set("Content-Type","application/json")
 response:=httptest.NewRecorder()
 a.addMagnet(response,req)
 if response.Code!=http.StatusPreconditionRequired{
  t.Fatalf("expected HTTP 428 when TMDB is not configured, got %d: %s",response.Code,response.Body.String())
 }
 if !strings.Contains(response.Body.String(),"TMDB API key"){t.Fatal("error should tell user how to configure TMDB")}
}

func TestNewDownloadsRemainBlockedAfterTMDBRemoval(t *testing.T){
 e:=&engine{entries:make(map[string]*entry),tmdbKey:"  "}
 _,err:=e.add("magnet:?xt=urn:btih:0123456789abcdef0123456789abcdef01234567")
 if !errors.Is(err,errTMDBRequired){t.Fatalf("whitespace must not count as a configured TMDB key, got %v",err)}
}
