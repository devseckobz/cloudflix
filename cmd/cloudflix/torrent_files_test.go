package main

import (
 "encoding/json"
 "net/http"
 "net/http/httptest"
 "strings"
 "testing"
)

func TestFileManifestMigrationPreservesOldRejections(t *testing.T){
 db:=testPINDB(t)
 if _,err:=db.Exec("CREATE TABLE torrents(hash TEXT PRIMARY KEY,magnet TEXT NOT NULL,rejection_reason TEXT NOT NULL DEFAULT '')");err!=nil{t.Fatal(err)}
 hash:=strings.Repeat("a",40)
 if _,err:=db.Exec("INSERT INTO torrents(hash,magnet,rejection_reason) VALUES(?,?,?)",hash,"magnet:?xt=urn:btih:"+hash,"Rejected: no MP4");err!=nil{t.Fatal(err)}
 for i:=0;i<2;i++{
  if err:=ensureFileManifestSchema(db);err!=nil{t.Fatal(err)}
 }
 var raw,reason string
 if err:=db.QueryRow("SELECT file_manifest,rejection_reason FROM torrents WHERE hash=?",hash).Scan(&raw,&reason);err!=nil{t.Fatal(err)}
 if raw!="" || reason!="Rejected: no MP4"{t.Fatalf("previous torrent information was not preserved: %q, %q",raw,reason)}
}

func TestInspectManifestListsSkippedFilesAndRejectsNonMP4(t *testing.T){
 input:=[]manifestFile{
  {Path:"Film.mkv",Size:1<<20},
  {Path:"Readme.txt",Size:200},
  {Path:"Subs/English.srt",Size:105},
 }
 rejected:=buildFileManifest(input,false)
 if rejected.Total!=3 || len(rejected.Files)!=3{t.Fatalf("unexpected rejected file manifest: %+v",rejected)}
 for _,f:=range rejected.Files{
  if f.Selected{t.Fatalf("rejected torrent must not mark file %q selected",f.Path)}
 }
 accepted:=buildFileManifest(append(input,manifestFile{Path:"Film.mp4",Size:50<<20}),true)
 if accepted.Total!=4{t.Fatal("missing file count")}
 if accepted.Files[0].Selected || accepted.Files[1].Selected{t.Fatal("nonmedia files must not be selected")}
 if !accepted.Files[2].Selected || !accepted.Files[3].Selected{t.Fatal("MP4 and SRT files should be selected")}
}

func TestInspectManifestIsBoundedAndRejectsUnsafePaths(t *testing.T){
 raw:=make([]manifestFile,0,maxManifestFiles+8)
 for i:=0;i<maxManifestFiles+8;i++{raw=append(raw,manifestFile{Path:"movie.mp4",Size:1})}
 manifest:=buildFileManifest(raw,true)
 if manifest.Total!=maxManifestFiles+8 || len(manifest.Files)!=maxManifestFiles{
  t.Fatalf("manifest limit not enforced: %+v",manifest)
 }
 unsafe:=buildFileManifest([]manifestFile{
  {Path:"../private.mp4",Size:1},
  {Path:"sub/../private.srt",Size:1},
  {Path:"movie.exe",Size:1},
  {Path:"movie.mp4",Size:1},
 },true)
 for i:=0;i<3;i++{
  if unsafe.Files[i].Selected{t.Errorf("unsafe file %s was selected",unsafe.Files[i].Path)}
 }
 if !unsafe.Files[3].Selected{t.Error("safe MP4 should be selected")}
}

func TestGetRejectedTorrentFilesFromDatabaseAfterRestart(t *testing.T){
 db:=testPINDB(t)
 if _,err:=db.Exec("CREATE TABLE torrents(hash TEXT PRIMARY KEY,rejection_reason TEXT NOT NULL,file_manifest TEXT NOT NULL)");err!=nil{t.Fatal(err)}
 hash:=strings.Repeat("b",40)
 manifest:=buildFileManifest([]manifestFile{
  {Path:"Movie.mkv",Size:1000000},
  {Path:"proof.txt",Size:500},
 },false)
 raw,err:=json.Marshal(manifest)
 if err!=nil{t.Fatal(err)}
 if _,err:=db.Exec("INSERT INTO torrents(hash,rejection_reason,file_manifest) VALUES(?,?,?)",hash,"Rejected: torrent contains no MP4",string(raw));err!=nil{t.Fatal(err)}
 a:=&application{engine:&engine{db:db,entries:map[string]*entry{hash:{hash:hash,rejectionReason:"Rejected: torrent contains no MP4"}}}}
 req:=httptest.NewRequest(http.MethodGet,"/api/torrents/"+hash+"/files",nil)
 req.SetPathValue("hash",hash)
 w:=httptest.NewRecorder()
 a.torrentFiles(w,req)
 if w.Code!=200{t.Fatalf("unexpected status %d: %s",w.Code,w.Body.String())}
 var body struct{
  Available bool `json:"available"`
  Rejected bool `json:"rejected"`
  Files []manifestFile `json:"files"`
  Total int `json:"total"`
 }
 if err:=json.Unmarshal(w.Body.Bytes(),&body);err!=nil{t.Fatal(err)}
 if !body.Available || !body.Rejected || body.Total!=2 || len(body.Files)!=2{
  t.Fatalf("expected persisted rejected files: %+v",body)
 }
 if body.Files[0].Path!="Movie.mkv" || body.Files[0].Selected{t.Error("rejected movie metadata missing or incorrectly selected")}
}

func TestOldRejectedTorrentShowsHonestFallback(t *testing.T){
 db:=testPINDB(t)
 if _,err:=db.Exec("CREATE TABLE torrents(hash TEXT PRIMARY KEY,rejection_reason TEXT NOT NULL,file_manifest TEXT NOT NULL)");err!=nil{t.Fatal(err)}
 hash:=strings.Repeat("c",40)
 if _,err:=db.Exec("INSERT INTO torrents(hash,rejection_reason,file_manifest) VALUES(?,?,?)",hash,"Rejected: no MP4","");err!=nil{t.Fatal(err)}
 a:=&application{engine:&engine{db:db,entries:map[string]*entry{hash:{hash:hash}}}}
 request:=httptest.NewRequest("GET","/api/torrents/"+hash+"/files",nil)
 request.SetPathValue("hash",hash)
 response:=httptest.NewRecorder()
 a.torrentFiles(response,request)
 if response.Code!=200 || !strings.Contains(response.Body.String(),"Remove it and add the magnet"){
  t.Fatalf("old rejected torrent must report missing metadata: %d %s",response.Code,response.Body.String())
 }
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
