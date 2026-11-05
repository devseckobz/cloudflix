package main

import (
 "encoding/json"
 "net/http"
 "net/http/httptest"
 "os"
 "path/filepath"
 "strings"
 "testing"
)

// deleteTestEngine builds an engine whose downloads directory holds real files,
// with the torrent's manifest describing which of them it owns.
func deleteTestEngine(t *testing.T,hash string,paths []string) (*engine,*entry,string) {
 t.Helper()
 db:=testPINDB(t)
 if _,err:=db.Exec("CREATE TABLE torrents(hash TEXT PRIMARY KEY,rejection_reason TEXT NOT NULL DEFAULT '')");err!=nil{t.Fatal(err)}
 if _,err:=db.Exec("INSERT INTO torrents(hash,rejection_reason) VALUES(?,?)",hash,"");err!=nil{t.Fatal(err)}
 downloads:=t.TempDir()
 manifest:=torrentFileManifest{Total:len(paths)}
 for _,path:=range paths{
  local:=filepath.Join(downloads,filepath.FromSlash(path))
  if err:=os.MkdirAll(filepath.Dir(local),0700);err!=nil{t.Fatal(err)}
  if err:=os.WriteFile(local,[]byte("payload bytes for "+path),0600);err!=nil{t.Fatal(err)}
  manifest.Files=append(manifest.Files,manifestFile{Path:path,Size:22,Selected:true})
 }
 item:=&entry{hash:hash,fileManifest:&manifest,validated:true}
 e:=&engine{db:db,entries:map[string]*entry{hash:item},downloads:downloads,cacheDir:t.TempDir()}
 return e,item,downloads
}

func exists(path string)bool{
 _,err:=os.Stat(path)
 return err==nil
}

func TestRemovingTorrentWithDeleteFilesRemovesPayload(t *testing.T){
 hash:=strings.Repeat("a",40)
 e,_,downloads:=deleteTestEngine(t,hash,[]string{"Movie.2024.1080p/movie.mkv","Movie.2024.1080p/movie.en.srt"})
 movie:=filepath.Join(downloads,"Movie.2024.1080p","movie.mkv")
 subs:=filepath.Join(downloads,"Movie.2024.1080p","movie.en.srt")
 if !exists(movie) || !exists(subs){t.Fatal("fixture files must exist before delete")}
 if err:=e.action(hash,"remove",true);err!=nil{t.Fatal(err)}
 if exists(movie){t.Fatal("movie file survived a delete-files removal")}
 if exists(subs){t.Fatal("subtitle file survived a delete-files removal")}
 // The emptied release folder is tidied up, but the downloads root itself stays.
 if exists(filepath.Join(downloads,"Movie.2024.1080p")){t.Fatal("emptied release folder was left behind")}
 if !exists(downloads){t.Fatal("downloads root must never be removed")}
 if e.entries[hash]!=nil{t.Fatal("queue entry survived removal")}
}

func TestRemovingTorrentWithoutDeleteFilesKeepsPayload(t *testing.T){
 hash:=strings.Repeat("b",40)
 e,_,downloads:=deleteTestEngine(t,hash,[]string{"movie.mkv"})
 movie:=filepath.Join(downloads,"movie.mkv")
 // The historical default must not change: a plain remove keeps the file.
 if err:=e.action(hash,"remove",false);err!=nil{t.Fatal(err)}
 if !exists(movie){t.Fatal("plain remove must leave the downloaded file on disk")}
 if e.entries[hash]!=nil{t.Fatal("queue entry survived removal")}
}

func TestRemovingTorrentDeletesOnlyItsOwnFiles(t *testing.T){
 hash:=strings.Repeat("c",40)
 e,_,downloads:=deleteTestEngine(t,hash,[]string{"shared/movie.mkv"})
 shared:=filepath.Join(downloads,"shared","movie.mkv")
 // A second torrent claims the same path; the shared file must survive.
 other:=strings.Repeat("d",40)
 otherManifest:=torrentFileManifest{Files:[]manifestFile{{Path:"shared/movie.mkv",Size:22,Selected:true}},Total:1}
 e.entries[other]=&entry{hash:other,fileManifest:&otherManifest,validated:true}
 if err:=e.action(hash,"remove",true);err!=nil{t.Fatal(err)}
 if !exists(shared){t.Fatal("delete destroyed a file another queued torrent still owns")}
}

func TestRemovingRejectedTorrentNeverTouchesFiles(t *testing.T){
 hash:=strings.Repeat("e",40)
 e,item,_:=deleteTestEngine(t,hash,[]string{"other-movie.mp4"})
 // Make the item look rejected; a rejected torrent never downloaded payload.
 item.rejectionReason="Rejected: no MP4"
 item.validated=false
 path:=filepath.Join(e.downloads,"other-movie.mp4")
 if err:=e.action(hash,"remove",true);err!=nil{t.Fatal(err)}
 if !exists(path){t.Fatal("rejected-torrent removal deleted an unrelated movie")}
}

func TestDeletingAMissingFileIsNotAnError(t *testing.T){
 hash:=strings.Repeat("f",40)
 e,_,downloads:=deleteTestEngine(t,hash,[]string{"movie.mkv"})
 // Simulate a torrent that was queued but never wrote its payload.
 if err:=os.Remove(filepath.Join(downloads,"movie.mkv"));err!=nil{t.Fatal(err)}
 if err:=e.action(hash,"remove",true);err!=nil{
  t.Fatalf("a never-downloaded torrent must delete cleanly, got %v",err)
 }
}

func TestDeleteRejectsPathsThatEscapeTheDownloadsRoot(t *testing.T){
 hash:=strings.Repeat("1",40)
 e,item,downloads:=deleteTestEngine(t,hash,[]string{"movie.mkv"})
 outside:=filepath.Join(filepath.Dir(downloads),"outside.mp4")
 if err:=os.WriteFile(outside,[]byte("must survive"),0600);err!=nil{t.Fatal(err)}
 // A malicious manifest path must be filtered by allowedMediaPath.
 item.fileManifest=&torrentFileManifest{Files:[]manifestFile{
  {Path:"../outside.mp4",Size:12,Selected:true},
  {Path:"/etc/passwd",Size:12,Selected:true},
 },Total:2}
 if err:=e.action(hash,"remove",true);err!=nil{t.Fatal(err)}
 if !exists(outside){t.Fatal("traversal path deleted a file outside the downloads root")}
}

func TestDeletePlaybackCacheClearsTranscodes(t *testing.T){
 hash:=strings.Repeat("2",40)
 e,_,_:=deleteTestEngine(t,hash,[]string{"movie.mkv"})
 key:=mkvKey(hash,"movie.mkv")
 converted:=filepath.Join(e.cacheDir,key+".mp4")
 manifest:=filepath.Join(e.cacheDir,key+".json")
 for _,path:=range []string{converted,manifest}{
  if err:=os.WriteFile(path,[]byte("cache"),0600);err!=nil{t.Fatal(err)}
 }
 if err:=e.action(hash,"remove",true);err!=nil{t.Fatal(err)}
 if exists(converted) || exists(manifest){t.Fatal("cached transcode survived the delete")}
}

// The HTTP layer is the contract the UI calls, so exercise it directly.
func TestTorrentActionDeleteFilesIsOptIn(t *testing.T){
 hash:=strings.Repeat("3",40)
 e,_,downloads:=deleteTestEngine(t,hash,[]string{"movie.mkv"})
 movie:=filepath.Join(downloads,"movie.mkv")
 app:=&application{engine:e}

 post:=func(body string)int{
  req:=httptest.NewRequest("POST","/api/torrents/"+hash+"/remove",strings.NewReader(body))
  req.Header.Set("Content-Type","application/json")
  req.SetPathValue("hash",hash)
  req.SetPathValue("action","remove")
  w:=httptest.NewRecorder()
  app.torrentAction(w,req)
  return w.Code
 }

 // A bodyless request keeps the historical behaviour.
 if code:=post("");code!=200{t.Fatalf("bodyless remove failed: %d",code)}
 if !exists(movie){t.Fatal("bodyless remove deleted the payload")}
}

func TestTorrentActionDeleteFilesRemovesPayloadOverHTTP(t *testing.T){
 hash:=strings.Repeat("4",40)
 e,_,downloads:=deleteTestEngine(t,hash,[]string{"movie.mkv"})
 movie:=filepath.Join(downloads,"movie.mkv")
 app:=&application{engine:e}

 body,_:=json.Marshal(map[string]bool{"deleteFiles":true})
 req:=httptest.NewRequest("POST","/api/torrents/"+hash+"/remove",strings.NewReader(string(body)))
 req.Header.Set("Content-Type","application/json")
 req.SetPathValue("hash",hash)
 req.SetPathValue("action","remove")
 w:=httptest.NewRecorder()
 app.torrentAction(w,req)
 if w.Code!=http.StatusOK{t.Fatalf("expected 200, got %d: %s",w.Code,w.Body.String())}
 if exists(movie){t.Fatal("deleteFiles:true over HTTP left the movie on disk")}
}
