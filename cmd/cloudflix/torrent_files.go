package main

import "net/http"

// torrentFiles returns an on-demand snapshot of in-memory torrent metainfo.
// No filename list is persisted for rejected (or accepted) torrents.
func (a *application) torrentFiles(w http.ResponseWriter,r *http.Request){
 hash:=r.PathValue("hash")
 if !mediaHashPattern.MatchString(hash){
  fail(w,http.StatusBadRequest,"invalid torrent hash")
  return
 }
 e:=a.engine
 e.mu.Lock()
 item:=e.entries[hash]
 var manifest *torrentFileManifest
 reason:=""
 pending:=false
 if item!=nil{
  reason=item.rejectionReason
  pending=reason==""
  if item.fileManifest!=nil{
   // The producer replaces the manifest once, while holding e.mu.
   // Copy the small list so the HTTP handler does not retain the entry.
   snapshot:=*item.fileManifest
   snapshot.Files=append([]manifestFile(nil),snapshot.Files...)
   manifest=&snapshot
  }
 }
 e.mu.Unlock()
 if item==nil{
  fail(w,http.StatusNotFound,"torrent not found")
  return
 }
 if manifest==nil{
  message:="Waiting for torrent metadata. No file list is available yet."
  if !pending{
   message="Temporary file metadata is unavailable after a restart. Remove this rejected entry and add the magnet again if you need to inspect its filenames."
  }
  respond(w,http.StatusOK,map[string]any{
   "available":false,"files":[]manifestFile{},"total":0,
   "rejected":!pending,"reason":reason,"message":message,
  })
  return
 }
 respond(w,http.StatusOK,map[string]any{
  "available":true,"files":manifest.Files,"total":manifest.Total,
  "truncated":manifest.Total>len(manifest.Files),
  "rejected":reason!="","reason":reason,
 })
}
