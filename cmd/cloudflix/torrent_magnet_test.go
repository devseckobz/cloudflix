package main

import (
 "encoding/json"
 "net/http"
 "net/http/httptest"
 "strings"
 "testing"
)

func TestMagnetEndpointReturnsOriginalLinkNotState(t *testing.T){
 hash:=strings.Repeat("a",40)
 magnet:="magnet:?xt=urn:btih:"+hash+"&dn=An%20Example%20Movie&tr=https%3A%2F%2Ftracker.example"
 a:=&application{engine:&engine{entries:map[string]*entry{hash:{hash:hash,magnet:magnet,title:"Example"}}}}
 r:=httptest.NewRequest("GET","/api/torrents/"+hash+"/magnet",nil)
 r.SetPathValue("hash",hash)
 response:=httptest.NewRecorder()
 a.torrentMagnet(response,r)
 if response.Code!=http.StatusOK{t.Fatalf("expected 200, got %d: %s",response.Code,response.Body.String())}
 var body struct{Magnet string `json:"magnet"`}
 if err:=json.Unmarshal(response.Body.Bytes(),&body);err!=nil{t.Fatal(err)}
 if body.Magnet!=magnet{t.Fatalf("original magnet mutated: %q",body.Magnet)}
 if response.Header().Get("Cache-Control")!="no-store"{t.Fatal("original magnet must not be cached")}
 // The movie state and SSE payload must not contain the tracker URL/magnet.
 snapshot:=a.engine.snapshot()
 encoded,_:=json.Marshal(snapshot)
 if strings.Contains(string(encoded),"magnet:?")||strings.Contains(string(encoded),"tracker.example"){
  t.Fatal("magnet link was exposed to the state broadcast")
 }
}

func TestMagnetEndpointRejectsInvalidAndUnknownHashes(t *testing.T){
 hash:=strings.Repeat("b",40)
 a:=&application{engine:&engine{entries:map[string]*entry{}}}
 for _,tc:=range []struct{hash string;code int}{
  {"../escape",http.StatusBadRequest},
  {"NOT_A_SHA",http.StatusBadRequest},
  {hash,http.StatusNotFound},
 }{
  req:=httptest.NewRequest("GET","/api/torrents/"+tc.hash+"/magnet",nil)
  req.SetPathValue("hash",tc.hash)
  response:=httptest.NewRecorder()
  a.torrentMagnet(response,req)
  if response.Code!=tc.code{t.Fatalf("%q returned %d, expected %d",tc.hash,response.Code,tc.code)}
 }
}
