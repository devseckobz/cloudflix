package main

import (
 "errors"
 "io/fs"
 "net/http"
 "os"
 "path/filepath"
 "sort"
 "strings"
)

// Files left in the downloads directory that no queued torrent claims. They
// appear when a download is removed with "keep files", when a torrent is
// re-added under a different release name, or when a crash lands between the
// database write and the unlink. Nothing in the UI could reclaim that space,
// so the downloader page exposes an explicit cleanup action.

// partSuffix is the temporary name anacrolix uses for an in-progress file
// (part files are on by default). Such a file belongs to a torrent that is
// still downloading and must never be treated as garbage.
const partSuffix = ".part"

// leftoverFile is one reclaimable path, relative to the downloads directory.
type leftoverFile struct {
 Path string `json:"path"`
 Size int64  `json:"size"`
}

type cleanupPreview struct {
 Files     []leftoverFile `json:"files"`
 Count     int            `json:"count"`
 Bytes     int64          `json:"bytes"`
 Scanned   int            `json:"scanned"`
 Truncated bool           `json:"truncated"`
}

// maxLeftoverFiles bounds the preview so a directory full of junk cannot build
// an unbounded response. The delete pass still walks everything.
const maxLeftoverFiles = 500

// reclaimableMediaName reports whether a path looks like something Cloudflix
// itself could have downloaded. Only these are ever considered garbage: a
// stray non-media file is not ours to delete.
//
// Sidecar files a media release ships next to the movie are included (.nfo scene
// notes, artwork, extra subtitle formats). Ignoring them meant a release folder
// whose media had been deleted still held files while the scan reported nothing,
// so Clean up stayed disabled and the directory could never be reclaimed.
//
// The list stays a fixed allowlist of extensions that only a release carries.
// Plain .txt is deliberately excluded: it is too easy to confuse with a note
// someone put in the downloads folder, and an existing test pins that a loose
// .txt is never ours to delete.
func reclaimableMediaName(path string)bool{
 name:=strings.TrimSuffix(path,partSuffix)
 switch strings.ToLower(filepath.Ext(name)){
 case ".mp4",".mkv",".srt",".avi",".m4v",".webm",".mov",".flv",".wmv",
  ".nfo",".jpg",".jpeg",".png",".sub",".idx",".ass",".ssa",".vtt":
  return true
 }
 return false
}

// expectedFilesLocked builds the set of paths, relative to the downloads root,
// that some queued torrent owns right now. Both the completed name and the
// in-progress ".part" name are included.
func (e *engine) expectedFilesLocked()(map[string]bool,error){
 expected:=make(map[string]bool)
 for _,item:=range e.entries{
  if item==nil{continue}
  // Metadata still loading: the file list is unknowable, so refuse to judge
  // what is garbage rather than risk deleting a live download.
  if item.torrent!=nil && item.torrent.Info()==nil && item.rejectionReason==""{
   return nil,errors.New("the queue is still loading torrent metadata. Nothing was deleted; try again once the queue settles.")
  }
  // Every declared path, not just media: sidecars are reclaimable too, so a
  // live torrent's own .nfo/.jpg must be protected or Clean up would delete
  // files belonging to a download the user still has.
  for _,path:=range e.torrentAllFilesLocked(item){
   expected[path]=true
   expected[path+partSuffix]=true
  }
 }
 return expected,nil
}

// scanLeftovers walks the downloads directory and returns every downloadable
// file the queue does not account for. Read-only.
func (e *engine) scanLeftovers()(cleanupPreview,error){
 e.mu.Lock()
 expected,err:=e.expectedFilesLocked()
 e.mu.Unlock()
 if err!=nil{return cleanupPreview{},err}

 var preview cleanupPreview
 preview.Files=make([]leftoverFile,0)
 walkErr:=filepath.WalkDir(e.downloads,func(absolute string,d fs.DirEntry,err error)error{
  if err!=nil{
   // A permission error on one entry must not abort the whole sweep.
   if d!=nil && d.IsDir(){return fs.SkipDir}
   return nil
  }
  if d.IsDir(){return nil}
  // Never follow or delete symlinks: they are not ours, and following one
  // could point outside the downloads directory.
  if !d.Type().IsRegular(){return nil}
  rel,relErr:=filepath.Rel(e.downloads,absolute)
  if relErr!=nil{return nil}
  path:=filepath.ToSlash(rel)
  if !reclaimableMediaName(path){return nil}
  preview.Scanned++
  if expected[path] || expected[strings.TrimSuffix(path,partSuffix)]{return nil}
  size:=int64(0)
  if info,statErr:=d.Info();statErr==nil{size=info.Size()}
  preview.Count++
  preview.Bytes+=size
  if len(preview.Files)<maxLeftoverFiles{
   preview.Files=append(preview.Files,leftoverFile{Path:path,Size:size})
  }else{
   preview.Truncated=true
  }
  return nil
 })
 if walkErr!=nil{return cleanupPreview{},walkErr}
 sort.Slice(preview.Files,func(i,j int)bool{return preview.Files[i].Path<preview.Files[j].Path})
 return preview,nil
}

// removeLeftovers deletes every downloadable file the queue does not account
// for, then prunes the directories that are left empty. Everything happens
// through an os.Root, so a discovered path cannot escape the downloads root.
func (e *engine) removeLeftovers()(cleanupPreview,error){
 preview,err:=e.scanLeftovers()
 if err!=nil{return cleanupPreview{},err}
 if preview.Count==0{return preview,nil}

 // Recompute the expected set immediately before deleting: a torrent added
 // between the scan and the unlink must not have its fresh file removed.
 e.mu.Lock()
 expected,err:=e.expectedFilesLocked()
 e.mu.Unlock()
 if err!=nil{return cleanupPreview{},err}

 root,err:=os.OpenRoot(e.downloads)
 if err!=nil{return preview,err}
 defer root.Close()

 directories:=make(map[string]bool)
 var removed int
 var freed int64
 var firstErr error
 for _,file:=range preview.Files{
  if expected[file.Path] || expected[strings.TrimSuffix(file.Path,partSuffix)]{continue}
  local:=filepath.FromSlash(file.Path)
  if !filepath.IsLocal(local){continue}
  if err:=root.Remove(local);err!=nil{
   if !errors.Is(err,os.ErrNotExist) && firstErr==nil{firstErr=err}
   continue
  }
  removed++
  freed+=file.Size
  for dir:=filepath.Dir(local);dir!="." && dir!=string(filepath.Separator);dir=filepath.Dir(dir){
   directories[dir]=true
  }
 }
 // Deepest first so a nested release folder empties before its parent is tried.
 ordered:=make([]string,0,len(directories))
 for dir:=range directories{ordered=append(ordered,dir)}
 sort.Slice(ordered,func(i,j int)bool{return len(ordered[i])>len(ordered[j])})
 for _,dir:=range ordered{_=root.Remove(dir)}

 // Report what was actually removed rather than what was scanned.
 preview.Count=removed
 preview.Bytes=freed
 if len(preview.Files)>removed{preview.Files=preview.Files[:removed]}
 return preview,firstErr
}

// storageLeftovers reports reclaimable files without deleting anything.
func (a *application) storageLeftovers(w http.ResponseWriter,r *http.Request){
 preview,err:=a.engine.scanLeftovers()
 if err!=nil{fail(w,http.StatusConflict,err.Error());return}
 respond(w,http.StatusOK,map[string]any{
  "files":preview.Files,"count":preview.Count,"bytes":preview.Bytes,
  "scanned":preview.Scanned,"truncated":preview.Truncated,
 })
}

// cleanupLeftovers deletes them.
func (a *application) cleanupLeftovers(w http.ResponseWriter,r *http.Request){
 preview,err:=a.engine.removeLeftovers()
 if err!=nil{
  fail(w,http.StatusInternalServerError,"Some leftover files could not be deleted: "+err.Error())
  return
 }
 respond(w,http.StatusOK,map[string]any{
  "deleted":preview.Count,"bytes":preview.Bytes,
 })
}
