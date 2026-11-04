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

func TestLegacyStoredManifestIsPurgedOnUpgrade(t *testing.T){
 db:=testPINDB(t)
 if _,err:=db.Exec("CREATE TABLE torrents(hash TEXT PRIMARY KEY,magnet TEXT NOT NULL,rejection_reason TEXT NOT NULL DEFAULT '',file_manifest TEXT NOT NULL DEFAULT '')");err!=nil{t.Fatal(err)}
 hash:=strings.Repeat("a",40)
 if _,err:=db.Exec("INSERT INTO torrents(hash,magnet,rejection_reason,file_manifest) VALUES(?,?,?,?)",hash,"magnet:?xt=urn:btih:"+hash,"Rejected: no MP4",`{"files":[{"path":"private.mkv"}],"total":1}`);err!=nil{t.Fatal(err)}
 for i:=0;i<2;i++{
  if err:=purgeLegacyFileManifests(db);err!=nil{t.Fatal(err)}
 }
 var raw,reason string
 if err:=db.QueryRow("SELECT file_manifest,rejection_reason FROM torrents WHERE hash=?",hash).Scan(&raw,&reason);err!=nil{t.Fatal(err)}
 if raw!="" || reason!="Rejected: no MP4"{t.Fatalf("legacy file list not purged or reason lost: %q %q",raw,reason)}
}

func TestFreshCatalogDoesNotNeedFileManifestColumn(t *testing.T){
 db:=testPINDB(t)
 if _,err:=db.Exec("CREATE TABLE torrents(hash TEXT PRIMARY KEY,magnet TEXT NOT NULL)");err!=nil{t.Fatal(err)}
 if err:=purgeLegacyFileManifests(db);err!=nil{t.Fatal(err)}
 var count int
 rows,err:=db.Query("PRAGMA table_info(torrents)")
 if err!=nil{t.Fatal(err)}
 defer rows.Close()
 for rows.Next(){
  var index,required,pk int
  var name,typ string
  var def any
  if err:=rows.Scan(&index,&name,&typ,&required,&def,&pk);err!=nil{t.Fatal(err)}
  if name=="file_manifest"{count++}
 }
 if count!=0{t.Fatal("fresh catalogs must not persist torrent file inventories")}
}

func TestTemporaryManifestListsSkippedFilesAndRejectsNonMP4(t *testing.T){
 input:=[]manifestFile{
  {Path:"Film.mkv",Size:1<<20},
  {Path:"Readme.txt",Size:200},
  {Path:"Subs/English.srt",Size:105},
 }
 rejected:=buildFileManifest(input,false)
 if rejected.Total!=3 || len(rejected.Files)!=3{t.Fatalf("unexpected rejected manifest: %+v",rejected)}
 for _,f:=range rejected.Files{
  if f.Selected{t.Fatalf("rejected torrent must not select %q",f.Path)}
 }
 accepted:=buildFileManifest(append(input,manifestFile{Path:"Film.mp4",Size:50<<20}),true)
 if accepted.Total!=4 || !accepted.Files[0].Selected || accepted.Files[1].Selected || !accepted.Files[2].Selected || !accepted.Files[3].Selected{
  t.Fatalf("invalid MP4/SRT file selection: %+v",accepted)
 }
}

func TestTemporaryManifestIsBoundedAndRejectsUnsafePaths(t *testing.T){
 raw:=make([]manifestFile,0,maxManifestFiles+8)
 for i:=0;i<maxManifestFiles+8;i++{raw=append(raw,manifestFile{Path:"movie.mp4",Size:1})}
 manifest:=buildFileManifest(raw,true)
 if manifest.Total!=maxManifestFiles+8 || len(manifest.Files)!=maxManifestFiles{t.Fatal("manifest limit not enforced")}
 unsafe:=buildFileManifest([]manifestFile{
  {Path:"../private.mp4",Size:1},
  {Path:"sub/../private.srt",Size:1},
  {Path:"movie.exe",Size:1},
  {Path:"movie.mp4",Size:1},
 },true)
 for i:=0;i<3;i++{if unsafe.Files[i].Selected{t.Errorf("unsafe file %s was selected",unsafe.Files[i].Path)}}
 if !unsafe.Files[3].Selected{t.Error("safe MP4 should be selected")}
}

func TestRejectedFilesAreOnlyAvailableDuringCurrentProcess(t *testing.T){
 hash:=strings.Repeat("b",40)
 manifest:=buildFileManifest([]manifestFile{
  {Path:"Movie.mkv",Size:1000000},
  {Path:"proof.txt",Size:500},
 },false)
 item:=&entry{hash:hash,rejectionReason:"Rejected: torrent contains no MP4",fileManifest:&manifest}
 e:=&engine{entries:map[string]*entry{hash:item}}
 a:=&application{engine:e}
 req:=httptest.NewRequest(http.MethodGet,"/api/torrents/"+hash+"/files",nil)
 req.SetPathValue("hash",hash)
 response:=httptest.NewRecorder()
 a.torrentFiles(response,req)
 if response.Code!=200{t.Fatalf("unexpected status %d: %s",response.Code,response.Body.String())}
 var body struct{
  Available bool `json:"available"`
  Rejected bool `json:"rejected"`
  Files []manifestFile `json:"files"`
  Total int `json:"total"`
 }
 if err:=json.Unmarshal(response.Body.Bytes(),&body);err!=nil{t.Fatal(err)}
 if !body.Available || !body.Rejected || body.Total!=2 || len(body.Files)!=2{t.Fatalf("expected temporary metadata: %+v",body)}
 if body.Files[0].Path!="Movie.mkv" || body.Files[0].Selected{t.Fatal("unexpected rejected file selection")}
 // A new process does not load file names from a database. When a
 // tracker client is available the Files endpoint re-fetches metadata; this
 // fixture has no client, so the endpoint must respond with an honest error.
 fresh:=&application{engine:&engine{entries:map[string]*entry{hash:{hash:hash,rejectionReason:item.rejectionReason,magnet:"magnet:?xt=urn:btih:"+hash}}}}
 again:=httptest.NewRecorder()
 fresh.torrentFiles(again,req)
 if again.Code!=200 || !strings.Contains(again.Body.String(),"The saved magnet is unavailable") || strings.Contains(again.Body.String(),"Movie.mkv"){
  t.Fatalf("restarted rejected torrent must not serve persisted filenames: %d %s",again.Code,again.Body.String())
 }
}

func TestDeleteRejectedClearsDatabaseAndMemoryWithoutTouchingOtherMovies(t *testing.T){
 db:=testPINDB(t)
 if _,err:=db.Exec("CREATE TABLE torrents(hash TEXT PRIMARY KEY,rejection_reason TEXT NOT NULL DEFAULT '')");err!=nil{t.Fatal(err)}
 hash:=strings.Repeat("c",40)
 if _,err:=db.Exec("INSERT INTO torrents(hash,rejection_reason) VALUES(?,?)",hash,"Rejected: no MP4");err!=nil{t.Fatal(err)}
 manifest:=buildFileManifest([]manifestFile{{Path:"other-movie.mp4",Size:42}},false)
 item:=&entry{hash:hash,rejectionReason:"Rejected: no MP4",fileManifest:&manifest}
 downloads:=t.TempDir()
 other:=filepath.Join(downloads,"other-movie.mp4")
 if err:=os.WriteFile(other,[]byte("existing legitimate download"),0600);err!=nil{t.Fatal(err)}
 e:=&engine{db:db,entries:map[string]*entry{hash:item},downloads:downloads}
 if err:=e.action(hash,"remove");err!=nil{t.Fatal(err)}
 if e.entries[hash]!=nil || item.fileManifest!=nil{t.Fatal("rejected torrent or temporary file list not cleared")}
 var count int
 if err:=db.QueryRow("SELECT COUNT(*) FROM torrents WHERE hash=?",hash).Scan(&count);err!=nil{t.Fatal(err)}
 if count!=0{t.Fatal("rejected database row was not removed")}
 body,err:=os.ReadFile(other)
 if err!=nil || string(body)!="existing legitimate download"{t.Fatal("delete used untrusted paths and damaged another movie")}
 req:=httptest.NewRequest("GET","/api/torrents/"+hash+"/files",nil)
 req.SetPathValue("hash",hash)
 w:=httptest.NewRecorder()
 (&application{engine:e}).torrentFiles(w,req)
 if w.Code!=http.StatusNotFound{t.Fatalf("deleted torrent metadata still readable, status %d",w.Code)}
 // Deleted infohash can be inserted again; stale records never block retries.
 if _,err:=db.Exec("INSERT INTO torrents(hash,rejection_reason) VALUES(?,?)",hash,"");err!=nil{t.Fatal(err)}
}

func TestUnknownTorrentFileInspectionIsDenied(t *testing.T){
 a:=&application{engine:&engine{entries:map[string]*entry{}}}
 hash:=strings.Repeat("d",40)
 req:=httptest.NewRequest("GET","/api/torrents/"+hash+"/files",nil)
 req.SetPathValue("hash",hash)
 response:=httptest.NewRecorder()
 a.torrentFiles(response,req)
 if response.Code!=http.StatusNotFound{t.Fatalf("expected 404, got %d",response.Code)}
}
