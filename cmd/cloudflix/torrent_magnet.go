package main

import (
 "net/http"
)

// torrentMagnet returns the original magnet only to an authenticated request.
// Never include magnet links in the broadcast state/SSE payload.
func (a *application) torrentMagnet(w http.ResponseWriter,r *http.Request){
 hash:=r.PathValue("hash")
 if !mediaHashPattern.MatchString(hash){
  fail(w,http.StatusBadRequest,"invalid torrent hash")
  return
 }
 a.engine.mu.Lock()
 item:=a.engine.entries[hash]
 magnet:=""
 if item!=nil{magnet=item.magnet}
 a.engine.mu.Unlock()
 if item==nil || magnet==""{
  fail(w,http.StatusNotFound,"magnet link not found")
  return
 }
 w.Header().Set("Cache-Control","no-store")
 respond(w,http.StatusOK,map[string]string{"magnet":magnet})
}
