package main

import (
 "errors"
 "io/fs"
 "net/http"
 "os"
 "path/filepath"
 "regexp"
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

var playbackArtifact = regexp.MustCompile(`^([0-9a-f]{64})(?:\.faststart)?\.(?:mp4|part\.mp4|json(?:\.part)?|[0-9]{1,3}\.vtt(?:\.part)?)$`)

func (p *cleanupPreview) add(file leftoverFile){
 p.Count++
 p.Bytes+=file.Size
 if len(p.Files)<maxLeftoverFiles{
  p.Files=append(p.Files,file)
 }else{
  p.Truncated=true
 }
}

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
 switch strings.ToLower(filepath.Base(name)){
 case "yifystatus.com.txt","ytsyifyup... (tor).txt":
  return true
 }
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

func (e *engine) scanLeftovers()(cleanupPreview,error){
 e.mu.Lock()
 expected,err:=e.expectedFilesLocked()
 keys:=e.expectedPlaybackKeysLocked()
 e.mu.Unlock()
 if err!=nil{return cleanupPreview{},err}
 preview,err:=e.walkLeftovers(expected,nil)
 if err!=nil{return preview,err}
 legacy:=keys
 if e.libraryDir!=""{legacy=nil}
 err=e.walkPlaybackLeftovers(legacy,&preview,false)
 if err==nil{err=e.walkCacheDirectory(e.libraryDir,"library",keys,&preview,false)}
 return preview,err
}

func (e *engine) expectedPlaybackKeysLocked()map[string]bool{
 keys:=make(map[string]bool)
 if e.cacheDir=="" && e.libraryDir==""{return keys}
 for _,item:=range e.entries{
  if item==nil{continue}
  if item.library!=nil{
   for _,file:=range item.library.Files{
    if file.Stored!=""{keys[strings.SplitN(file.Stored,".",2)[0]]=true}
   }
  }
  for _,path:=range e.torrentAllFilesLocked(item){
   if strings.EqualFold(filepath.Ext(path),".mkv"){keys[mkvKey(item.hash,path)]=true}
  }
 }
 if e.playback!=nil{
  e.playback.mu.Lock()
  defer e.playback.mu.Unlock()
  for key,job:=range e.playback.jobs{
   if job.state=="queued" || job.state=="processing"{keys[strings.TrimSuffix(key,faststartSuffix)]=true}
  }
 }
 return keys
}

// Older releases could remove the queue entry while leaving generated playback
// files outside downloads. Match only filenames Cloudflix can generate.
func (e *engine) walkPlaybackLeftovers(keys map[string]bool,preview *cleanupPreview,remove bool)error{
 return e.walkCacheDirectory(e.cacheDir,"playback-cache",keys,preview,remove)
}

func (e *engine) walkCacheDirectory(dir,label string,keys map[string]bool,preview *cleanupPreview,remove bool)error{
 if dir==""{return nil}
 root,err:=os.OpenRoot(dir)
 if errors.Is(err,os.ErrNotExist){return nil}
 if err!=nil{return err}
 defer root.Close()
 entries,err:=fs.ReadDir(root.FS(),".")
 if err!=nil{return err}
 var firstErr error
 for _,entry:=range entries{
  if !entry.Type().IsRegular(){continue}
  match:=playbackArtifact.FindStringSubmatch(entry.Name())
  if match==nil{continue}
  preview.Scanned++
  if keys[match[1]]{continue}
  info,err:=entry.Info()
  if errors.Is(err,os.ErrNotExist){continue}
  if err==nil && remove{err=root.Remove(entry.Name())}
  if errors.Is(err,os.ErrNotExist){continue}
  if err!=nil{
   if firstErr==nil{firstErr=err}
   continue
  }
  preview.add(leftoverFile{Path:label+"/"+entry.Name(),Size:info.Size()})
 }
 return firstErr
}

// visit can remove a candidate. Only candidates it accepts are counted.
func (e *engine) walkLeftovers(expected map[string]bool,visit func(leftoverFile)bool)(cleanupPreview,error){
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
  file:=leftoverFile{Path:path,Size:size}
  if visit!=nil && !visit(file){return nil}
  preview.add(file)
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
 // Keep ownership stable from the scan through the last unlink.
 e.mu.Lock()
 defer e.mu.Unlock()
 expected,err:=e.expectedFilesLocked()
 if err!=nil{return cleanupPreview{},err}

 root,err:=os.OpenRoot(e.downloads)
 if err!=nil{return cleanupPreview{},err}
 defer root.Close()

 directories:=make(map[string]bool)
 var firstErr error
 preview,walkErr:=e.walkLeftovers(expected,func(file leftoverFile)bool{
  local:=filepath.FromSlash(file.Path)
  if !filepath.IsLocal(local){return false}
  if err:=root.Remove(local);err!=nil{
   if !errors.Is(err,os.ErrNotExist) && firstErr==nil{firstErr=err}
   return false
  }
  for dir:=filepath.Dir(local);dir!="." && dir!=string(filepath.Separator);dir=filepath.Dir(dir){
   directories[dir]=true
  }
  return true
 })
 // Deepest first so a nested release folder empties before its parent is tried.
 ordered:=make([]string,0,len(directories))
 for dir:=range directories{ordered=append(ordered,dir)}
 sort.Slice(ordered,func(i,j int)bool{return len(ordered[i])>len(ordered[j])})
 for _,dir:=range ordered{_=root.Remove(dir)}

 keys:=e.expectedPlaybackKeysLocked()
 legacy:=keys
 if e.libraryDir!=""{legacy=nil}
 cacheErr:=e.walkPlaybackLeftovers(legacy,&preview,true)
 libraryErr:=e.walkCacheDirectory(e.libraryDir,"library",keys,&preview,true)
 return preview,errors.Join(walkErr,firstErr,cacheErr,libraryErr)
}

// storageLeftovers reports reclaimable files without deleting anything.
func (a *application) storageLeftovers(w http.ResponseWriter,r *http.Request){
 w.Header().Set("Cache-Control","no-store")
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
