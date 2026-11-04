package main

import (
 "database/sql"
 "encoding/json"
 "errors"
 "net/http"
)

// torrentFiles lists metainfo filenames only. It never opens or streams files,
// including for torrents that were rejected before any payload was downloaded.
func (a *application) torrentFiles(w http.ResponseWriter,r *http.Request){
 hash:=r.PathValue("hash")
 if !mediaHashPattern.MatchString(hash){
  fail(w,http.StatusBadRequest,"invalid torrent hash")
  return
 }
 e:=a.engine
 e.mu.Lock()
 _,found:=e.entries[hash]
 e.mu.Unlock()
 if !found{
  fail(w,http.StatusNotFound,"torrent not found")
  return
 }
 var raw,reason string
 err:=e.db.QueryRow("SELECT file_manifest,rejection_reason FROM torrents WHERE hash=?",hash).Scan(&raw,&reason)
 if errors.Is(err,sql.ErrNoRows){
  fail(w,http.StatusNotFound,"torrent not found")
  return
 }
 if err!=nil{
  fail(w,http.StatusInternalServerError,"could not load torrent files")
  return
 }
 if raw==""{
  message:="Waiting for torrent metadata. No file list is available yet."
  if reason!=""{
   message="This torrent was rejected before file metadata could be saved. Remove it and add the magnet again to inspect its files."
  }
  respond(w,http.StatusOK,map[string]any{
   "available":false,"files":[]manifestFile{},"total":0,
   "rejected":reason!="","reason":reason,"message":message,
  })
  return
 }
 var manifest torrentFileManifest
 if err:=json.Unmarshal([]byte(raw),&manifest);err!=nil{
  fail(w,http.StatusInternalServerError,"stored torrent file list is invalid")
  return
 }
 if manifest.Total<0 || len(manifest.Files)>maxManifestFiles{
  fail(w,http.StatusInternalServerError,"stored torrent file list is invalid")
  return
 }
 respond(w,http.StatusOK,map[string]any{
  "available":true,"files":manifest.Files,"total":manifest.Total,
  "truncated":manifest.Total>len(manifest.Files),
  "rejected":reason!="","reason":reason,
 })
}
