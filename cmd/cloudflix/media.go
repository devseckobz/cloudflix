package main

import (
 "bytes"
 "io"
 "net/http"
 "os"
 "path/filepath"
 "regexp"
 "strconv"
 "strings"
 "time"
)

const maxSubtitleBytes int64 = 8 << 20

var mediaHashPattern = regexp.MustCompile("^[0-9a-f]{40}$")
var srtTimestamp = regexp.MustCompile(`\b([0-9]{2}:[0-9]{2}:[0-9]{2}),([0-9]{3})\b`)

// mediaKind ensures the media endpoint can only serve supported movie and
// text subtitle formats, not arbitrary torrent contents (scripts, HTML, etc.).
func mediaKind(name string) (contentType string, subtitle bool) {
 switch strings.ToLower(filepath.Ext(name)) {
 case ".mp4":
  return "video/mp4", false
 case ".srt",".vtt":
  return "text/vtt; charset=utf-8", true
 default:
  return "", false
 }
}

func convertSRTToVTT(raw []byte) []byte {
 normalized:=strings.ReplaceAll(string(raw),"\r\n","\n")
 normalized=strings.ReplaceAll(normalized,"\r","\n")
 normalized=strings.TrimPrefix(normalized,"\ufeff")
 if strings.HasPrefix(normalized,"WEBVTT\n") {
  return []byte(normalized)
 }
 // SRT and WebVTT share cue structure; only comma milliseconds must change.
 normalized=srtTimestamp.ReplaceAllString(normalized,"${1}.${2}")
 return []byte("WEBVTT\n\n"+normalized)
}

// videoETag is a strong validator derived from the bytes on disk. A replaced
// download changes size or mtime, so a stale body can never be reused.
func videoETag(size int64,modtime time.Time)string{
 return `"`+strconv.FormatInt(size,16)+"-"+strconv.FormatInt(modtime.UnixNano(),16)+`"`
}

// serveVideoBody lets the browser keep the movie bytes it already fetched.
// no-store is right for the HTML shell, JSON APIs and magnet links, but it
// forced a full re-download of every byte range the player had already
// buffered, which is exactly what makes a seek stall. The response is still
// private to the authenticated session and revalidated on every use, so a
// replaced file is re-fetched rather than served from a stale cache.
func serveVideoBody(w http.ResponseWriter,r *http.Request,name string,size int64,modtime time.Time,content io.ReadSeeker){
 w.Header().Set("ETag",videoETag(size,modtime))
 w.Header().Set("Cache-Control","private, max-age=0, must-revalidate")
 http.ServeContent(w,r,name,modtime,content)
}

// serveMediaFile reads only from an os.Root capability, keeping torrent names
// unable to escape the downloads directory through traversal or symlinks.
func serveMediaFile(w http.ResponseWriter, r *http.Request, root *os.Root, name string) {
 local:=filepath.FromSlash(name)
 if (!allowedMediaPath(name) && !strings.EqualFold(filepath.Ext(name),".vtt")) || !safeTorrentPath(name) || !filepath.IsLocal(local) || filepath.ToSlash(local)!=name {
  fail(w,http.StatusNotFound,"media file not found")
  return
 }
 mime,subtitle:=mediaKind(local)
 if mime=="" {
  fail(w,http.StatusUnsupportedMediaType,"unsupported media format")
  return
 }
 file,err:=root.Open(local)
 if err!=nil {
  fail(w,http.StatusNotFound,"media file not found")
  return
 }
 defer file.Close()
 stat,err:=file.Stat()
 if err!=nil || !stat.Mode().IsRegular(){
  fail(w,http.StatusNotFound,"media file not found")
  return
 }
 w.Header().Set("Content-Type",mime)
 w.Header().Set("Content-Disposition","inline")
 if subtitle && strings.EqualFold(filepath.Ext(local),".srt") {
  w.Header().Set("Cache-Control","private, no-store")
  if stat.Size()>maxSubtitleBytes {
   fail(w,http.StatusRequestEntityTooLarge,"subtitle file is too large")
   return
  }
  data,err:=io.ReadAll(io.LimitReader(file,maxSubtitleBytes+1))
  if err!=nil || int64(len(data))>maxSubtitleBytes{
   fail(w,http.StatusRequestEntityTooLarge,"subtitle file is too large")
   return
  }
  http.ServeContent(w,r,strings.TrimSuffix(stat.Name(),filepath.Ext(stat.Name()))+".vtt",stat.ModTime(),bytes.NewReader(convertSRTToVTT(data)))
  return
 }
 serveVideoBody(w,r,stat.Name(),stat.Size(),stat.ModTime(),file)
}

// A completed torrent is required before media streams or subtitle files can
// be requested. File paths must also appear in that torrent's metadata.
func (a *application) streamMedia(w http.ResponseWriter,r *http.Request) {
 hash:=r.PathValue("hash")
 requested:=r.URL.Query().Get("path")
 if !mediaHashPattern.MatchString(hash) || requested=="" || len(requested)>4096 {
  fail(w,http.StatusBadRequest,"invalid media request")
  return
 }
 e:=a.engine
 e.mu.Lock()
 item:=e.entries[hash]
 valid:=false
 dir,name:=e.downloads,requested
 if item!=nil && item.library!=nil{
  if file,ok:=item.library.file(requested);ok{
   valid=true
   if file.Stored!=""{dir,name=e.libraryDir,file.Stored}
  }
 }else if item!=nil && item.validated && item.rejectionReason=="" && item.torrent!=nil && item.torrent.Info()!=nil {
  // Memoized per file: the piece walk used to run on every authenticated
  // range request while holding the engine mutex, stalling concurrent seeks.
  valid=e.fileVerifiedLocked(item,item.torrent,requested)
 }
 e.mu.Unlock()
 if !valid {
  fail(w,http.StatusNotFound,"media unavailable until the download completes")
  return
 }
 root,err:=os.OpenRoot(dir)
 if err!=nil {
  fail(w,http.StatusInternalServerError,"storage unavailable")
  return
 }
 defer root.Close()
 serveMediaFile(w,r,root,name)
}

