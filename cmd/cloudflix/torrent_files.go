package main

import (
 "net/http"
 "path/filepath"
 "strings"
 "time"

 "github.com/anacrolix/torrent"
)

const maxConcurrentRejectedInspections=3
const rejectedInspectionTimeout=90*time.Second

// beginRejectedFileInspectionLocked fetches metadata only. In particular,
// DisallowDataDownload is set *before* adding the torrent to the client, so
// there is no window in which payload pieces can be requested.
func (e *engine) beginRejectedFileInspectionLocked(item *entry){
 item.inspectedFilesOnce=true
 item.fileInspectionError=""
 if item.rejectionReason=="" || item.fileManifest!=nil || item.inspectingFiles{return}
 if e.client==nil || item.magnet==""{
  item.fileInspectionError="The saved magnet is unavailable for inspection."
  return
 }
 if e.activeFileInspections>=maxConcurrentRejectedInspections{
  item.fileInspectionError="Too many file inspections are running. Retry shortly."
  return
 }
 spec,err:=torrent.TorrentSpecFromMagnetUri(item.magnet)
 if err!=nil{
  item.fileInspectionError="The stored magnet link is invalid."
  return
 }
 spec.DisallowDataDownload=true
 spec.DisallowDataUpload=true
 tt,created,err:=e.client.AddTorrentSpec(spec)
 if err!=nil{
  item.fileInspectionError="Unable to inspect this magnet's metadata. Retry shortly."
  return
 }
 if !created{
  // Avoid modifying an existing active download if a torrent was already
  // added to the client for another purpose.
  item.fileInspectionError="This torrent is already being inspected. Retry shortly."
  return
 }
 if tt.InfoHash().HexString()!=item.hash{
  tt.Drop()
  item.fileInspectionError="Stored magnet infohash does not match this entry."
  return
 }
 cancel:=make(chan struct{})
 item.inspectingFiles=true
 item.fileInspectionCancel=cancel
 item.torrent=tt
 e.activeFileInspections++
 go e.awaitRejectedFileMetadata(item,tt,cancel)
}

func (e *engine) awaitRejectedFileMetadata(item *entry,tt *torrent.Torrent,cancel <-chan struct{}){
 timer:=time.NewTimer(rejectedInspectionTimeout)
 defer timer.Stop()
 fetched:=false
 select{
 case <-tt.GotInfo():
  fetched=true
 case <-timer.C:
 case <-cancel:
  // Delete already dropped the temporary torrent and cleared its count.
  return
 }
 e.mu.Lock()
 defer e.mu.Unlock()
 if e.entries[item.hash]!=item || item.fileInspectionCancel!=cancel{
  return
 }
 item.fileInspectionCancel=nil
 item.inspectingFiles=false
 if e.activeFileInspections>0{e.activeFileInspections--}
 if fetched && tt.Info()!=nil{
  files:=tt.Files()
  raw:=make([]manifestFile,0,min(len(files),maxManifestFiles))
  for _,file:=range files{
   if len(raw)>=maxManifestFiles{break}
   raw=append(raw,manifestFile{
    Path:filepath.ToSlash(file.Path()),
    Size:file.Length(),
   })
  }
  // The original rejection is authoritative: inspection never selects
  // payload files or reclassifies the torrent as a download.
  manifest:=buildFileManifest(raw,false)
  manifest.Total=len(files)
  item.fileManifest=&manifest
  item.fileInspectionError=""
 }else{
  item.fileInspectionError="Could not retrieve torrent filenames from peers. Check that the magnet still has available metadata sources, then retry."
 }
 if item.torrent==tt{
  tt.DisallowDataDownload()
  tt.Drop()
  item.torrent=nil
 }
}

// torrentFiles returns a transient view of torrent metainfo. Rejected entries
// left over from an earlier process are inspected again when Files is clicked.
// No filenames or payload content are written to the application database.
func (a *application) torrentFiles(w http.ResponseWriter,r *http.Request){
 hash:=r.PathValue("hash")
 if !mediaHashPattern.MatchString(hash){
  fail(w,http.StatusBadRequest,"invalid torrent hash")
  return
 }
 e:=a.engine
 e.mu.Lock()
 item:=e.entries[hash]
 if item==nil{
  e.mu.Unlock()
  fail(w,http.StatusNotFound,"torrent not found")
  return
 }
 reason:=item.rejectionReason
 if reason!="" && item.fileManifest==nil{
  if strings.EqualFold(r.URL.Query().Get("retry"),"1") && !item.inspectingFiles{
   item.inspectedFilesOnce=false
   item.fileInspectionError=""
  }
  if !item.inspectedFilesOnce{
   e.beginRejectedFileInspectionLocked(item)
  }
 }
 fetching:=item.inspectingFiles || (reason=="" && item.fileManifest==nil)
 message:=item.fileInspectionError
 var manifest *torrentFileManifest
 if item.fileManifest!=nil{
  snapshot:=*item.fileManifest
  snapshot.Files=append([]manifestFile(nil),snapshot.Files...)
  manifest=&snapshot
 }
 e.mu.Unlock()
 if manifest==nil{
  if message==""{
   if fetching{
    message="Fetching torrent filenames from peers. This downloads metadata only, not movie files."
   }else{
    message="The file list is not available."
   }
  }
  respond(w,http.StatusOK,map[string]any{
   "available":false,"fetching":fetching,"files":[]manifestFile{},
   "total":0,"rejected":reason!="","reason":reason,"message":message,
  })
  return
 }
 respond(w,http.StatusOK,map[string]any{
  "available":true,"fetching":false,"files":manifest.Files,
  "total":manifest.Total,"truncated":manifest.Total>len(manifest.Files),
  "rejected":reason!="","reason":reason,
 })
}
