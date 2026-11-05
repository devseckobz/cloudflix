package main

import (
 "os"
 "path/filepath"
 "strings"
 "testing"

 "github.com/anacrolix/torrent"
)

// cleanupEngine builds an engine with a downloads directory holding the given
// files, plus a manifest for a torrent that owns `owned`.
func cleanupEngine(t *testing.T,owned []string,files []string)(*engine,string){
 t.Helper()
 db:=testPINDB(t)
 if _,err:=db.Exec("CREATE TABLE torrents(hash TEXT PRIMARY KEY,rejection_reason TEXT NOT NULL DEFAULT '')");err!=nil{t.Fatal(err)}
 downloads:=t.TempDir()
 for _,path:=range files{
  local:=filepath.Join(downloads,filepath.FromSlash(path))
  if err:=os.MkdirAll(filepath.Dir(local),0700);err!=nil{t.Fatal(err)}
  if err:=os.WriteFile(local,[]byte(strings.Repeat("x",100)),0600);err!=nil{t.Fatal(err)}
 }
 hash:=strings.Repeat("7",40)
 if _,err:=db.Exec("INSERT INTO torrents(hash,rejection_reason) VALUES(?,?)",hash,"");err!=nil{t.Fatal(err)}
 manifest:=torrentFileManifest{Total:len(owned)}
 for _,path:=range owned{
  manifest.Files=append(manifest.Files,manifestFile{Path:path,Size:100,Selected:true})
 }
 e:=&engine{db:db,downloads:downloads,entries:map[string]*entry{hash:{hash:hash,fileManifest:&manifest,validated:true}}}
 return e,downloads
}

func TestCleanupFindsOnlyFilesNoTorrentOwns(t *testing.T){
 e,downloads:=cleanupEngine(t,
  []string{"Keep.2024/movie.mkv","Keep.2024/movie.en.srt"},
  []string{
   "Keep.2024/movie.mkv",
   "Keep.2024/movie.en.srt",
   "Orphan.2019/movie.mkv",
   "Orphan.2019/movie.en.srt",
   "not-a-movie.txt", // not a downloadable type: never ours to delete
  })

 preview,err:=e.scanLeftovers()
 if err!=nil{t.Fatal(err)}
 paths:=[]string{}
 for _,f:=range preview.Files{paths=append(paths,f.Path)}
 if len(paths)!=2{t.Fatalf("expected the two orphan media files, got %v",paths)}
 if !contains(paths,"Orphan.2019/movie.mkv") || !contains(paths,"Orphan.2019/movie.en.srt"){
  t.Fatalf("wrong files flagged: %v",paths)
 }
 if preview.Count!=2{t.Fatalf("count=%d",preview.Count)}
 if preview.Bytes!=200{t.Fatalf("bytes=%d, want 200",preview.Bytes)}
 // Scanning must never delete anything.
 if _,err:=os.Stat(filepath.Join(downloads,"Orphan.2019","movie.mkv"));err!=nil{
  t.Fatal("scan deleted a file")
 }

 cleaned,err:=e.removeLeftovers()
 if err!=nil{t.Fatal(err)}
 if cleaned.Count!=2{t.Fatalf("expected 2 deleted, got %d",cleaned.Count)}
 // Owned files survive.
 if _,err:=os.Stat(filepath.Join(downloads,"Keep.2024","movie.mkv"));err!=nil{
  t.Fatal("cleanup deleted a file the queue still owns")
 }
 // Orphans are gone, and their emptied folder is pruned.
 if _,err:=os.Stat(filepath.Join(downloads,"Orphan.2019","movie.mkv"));!os.IsNotExist(err){
  t.Fatal("orphan file survived cleanup")
 }
 if _,err:=os.Stat(filepath.Join(downloads,"Orphan.2019"));!os.IsNotExist(err){
  t.Fatal("emptied orphan folder was not pruned")
 }
 // The downloads root always stays.
 if _,err:=os.Stat(downloads);err!=nil{t.Fatal("downloads root was removed")}
}

func TestCleanupKeepsInProgressPartFiles(t *testing.T){
 // anacrolix defaults to part files: an active download is on disk as
 // "movie.mkv.part" while the manifest names "movie.mkv".
 e,_:=cleanupEngine(t,[]string{"Active.2024/movie.mkv"},[]string{
  "Active.2024/movie.mkv.part",
 })
 preview,err:=e.scanLeftovers()
 if err!=nil{t.Fatal(err)}
 if preview.Count!=0{
  t.Fatalf("an in-progress .part file must not be reclaimed, got %v",preview.Files)
 }
}

func TestCleanupReportsNothingWhenEverythingIsOwned(t *testing.T){
 e,_:=cleanupEngine(t,[]string{"movie.mkv"},[]string{"movie.mkv"})
 preview,err:=e.scanLeftovers()
 if err!=nil{t.Fatal(err)}
 if preview.Count!=0{t.Fatalf("expected no leftovers, got %v",preview.Files)}
 if _,err:=e.removeLeftovers();err!=nil{t.Fatalf("cleanup with nothing to do must not error: %v",err)}
}

func TestCleanupRefusesWhileMetadataIsStillLoading(t *testing.T){
 e,downloads:=cleanupEngine(t,[]string{"movie.mkv"},[]string{"movie.mkv","Orphan.2019/movie.mkv"})
 // A torrent whose metadata has not arrived: its file list is unknowable, so
 // cleanup must refuse rather than guess and delete a live download.
 cfg:=torrent.NewDefaultClientConfig()
 cfg.DataDir=t.TempDir()
 cfg.Seed=false
 client,err:=torrent.NewClient(cfg)
 if err!=nil{t.Fatal(err)}
 defer client.Close()
 tt,err:=client.AddMagnet("magnet:?xt=urn:btih:"+strings.Repeat("9",40))
 if err!=nil{t.Fatal(err)}
 if tt.Info()!=nil{t.Fatal("expected a torrent still awaiting metadata")}
 hash:=strings.Repeat("9",40)
 e.entries[hash]=&entry{hash:hash,torrent:tt}

 if _,err:=e.scanLeftovers();err==nil{
  t.Fatal("cleanup must refuse while a torrent's metadata is still loading")
 }
 // And the orphan must still be on disk after the refusal.
 if _,err:=os.Stat(filepath.Join(downloads,"Orphan.2019","movie.mkv"));err!=nil{
  t.Fatal("refused cleanup still deleted a file")
 }
}

func TestCleanupEndpointsAreWiredToTheRightPaths(t *testing.T){
 main,err:=os.ReadFile("main.go")
 if err!=nil{t.Fatal(err)}
 source:=string(main)
 // The GET is a dry run and the POST deletes: mixing them up would delete on
 // page load, so pin both verbs to their handlers.
 if !strings.Contains(source,`GET /api/storage/leftovers",a.require(a.storageLeftovers)`){
  t.Fatal("leftovers dry-run route is not wired")
 }
 if !strings.Contains(source,`POST /api/storage/cleanup",a.require(a.cleanupLeftovers)`){
  t.Fatal("cleanup delete route is not wired")
 }
}

func TestScanIsReadOnlyAndDeleteReclaimsExactlyThePreview(t *testing.T){
 e,downloads:=cleanupEngine(t,[]string{"Keep/movie.mkv"},[]string{
  "Keep/movie.mkv","Junk/movie.mkv","Junk/other.srt","Junk/notes.txt",
 })
 preview,err:=e.scanLeftovers()
 if err!=nil{t.Fatal(err)}
 // The dry run reports, and nothing is removed.
 for _,f:=range preview.Files{
  if _,statErr:=os.Stat(filepath.Join(downloads,filepath.FromSlash(f.Path)));statErr!=nil{
   t.Fatalf("dry run removed %s",f.Path)
  }
 }
 if preview.Count!=2{t.Fatalf("expected 2 media leftovers, got %d: %v",preview.Count,preview.Files)}
 // Running it twice in a row is stable.
 again,err:=e.scanLeftovers()
 if err!=nil{t.Fatal(err)}
 if again.Count!=preview.Count{t.Fatalf("scan is not idempotent: %d vs %d",preview.Count,again.Count)}
 // The delete pass reports what it really removed.
 cleaned,err:=e.removeLeftovers()
 if err!=nil{t.Fatal(err)}
 if cleaned.Count!=preview.Count{t.Fatalf("deleted %d but previewed %d",cleaned.Count,preview.Count)}
 if _,err:=os.Stat(filepath.Join(downloads,"Keep","movie.mkv"));err!=nil{
  t.Fatal("cleanup removed an owned file")
 }
}

func contains(values []string,want string)bool{
 for _,value:=range values{if value==want{return true}}
 return false
}
