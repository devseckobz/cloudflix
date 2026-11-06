package main

import (
 "net/http"
 "net/http/httptest"
 "path/filepath"
 "strings"
 "testing"
)

// TestTorrentDeleteAcceptsTheRequestTheUIActuallySends guards the contract
// between App.svelte and torrentAction.
//
// The delete button used to fail with "invalid request" and delete nothing: the
// UI posted a JSON body, torrentAction declares only keepFiles, and readJSON
// rejects unknown fields. Both the body the UI sends and the documented
// keepFiles escape hatch must be accepted.
func TestTorrentDeleteAcceptsTheRequestTheUIActuallySends(t *testing.T){
 hash:=strings.Repeat("a",40)
 for _,body:=range []string{`{"deleteFiles":true}`,`{}`,`{"keepFiles":false}`,`{"keepFiles":true}`,""}{
  a:=&application{engine:&engine{entries:map[string]*entry{}}}
  request:=httptest.NewRequest(http.MethodPost,"/api/torrents/"+hash+"/remove",strings.NewReader(body))
  request.Header.Set("Content-Type","application/json")
  request.SetPathValue("hash",hash)
  request.SetPathValue("action","remove")
  w:=httptest.NewRecorder()
  a.torrentAction(w,request)
  // No torrent is registered, so a well-formed request reaches the engine and
  // comes back as "torrent not found"; a rejected body comes back as
  // "invalid request" before any work happens.
  if strings.Contains(w.Body.String(),"invalid request"){
   t.Errorf("body %q was rejected as malformed: %s",body,w.Body.String())
  }
 }
}

// TestUIDeleteBodyActuallyDeletesTheFiles is the end-to-end guard for the bug
// where the delete button showed "invalid request" and removed nothing. The UI
// posts exactly {"deleteFiles":true}; that must delete the movie from disk.
func TestUIDeleteBodyActuallyDeletesTheFiles(t *testing.T){
 hash:=strings.Repeat("5",40)
 e,_,downloads:=deleteTestEngine(t,hash,[]string{"movie.mkv"})
 movie:=filepath.Join(downloads,"movie.mkv")
 app:=&application{engine:e}

 // This is the literal body App.svelte sends for a delete.
 req:=httptest.NewRequest("POST","/api/torrents/"+hash+"/remove",strings.NewReader(`{"deleteFiles":true}`))
 req.Header.Set("Content-Type","application/json")
 req.SetPathValue("hash",hash)
 req.SetPathValue("action","remove")
 w:=httptest.NewRecorder()
 app.torrentAction(w,req)
 if w.Code!=http.StatusOK{
  t.Fatalf("the UI's delete request was rejected: %d %s",w.Code,w.Body.String())
 }
 if exists(movie){
  t.Fatal("the delete the UI sends left the movie on disk")
 }
}
