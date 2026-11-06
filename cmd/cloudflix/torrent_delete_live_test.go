package main

import (
 "os"
 "path/filepath"
 "strings"
 "testing"

 "github.com/anacrolix/torrent"
 "github.com/anacrolix/torrent/bencode"
 "github.com/anacrolix/torrent/metainfo"
)

// liveDeleteEngine builds the production shape: a real anacrolix torrent that
// owns its files, rather than a hand-built fileManifest. That distinction is
// the whole point of these tests -- the manifest is only populated when torrent
// metadata arrives or the Files dialog is opened, so a synthetic manifest hides
// what actually happens when a user deletes a normal completed download.
//
// It returns the engine and the torrent's file paths relative to the downloads
// root, which is where the fixture wrote them.
func liveDeleteEngine(t *testing.T,hash string,torrentName string,paths []string)(*engine,[]string){
 t.Helper()
 client:=perfClient(t)
 files:=make([]metainfo.FileInfo,0,len(paths))
 for _,path:=range paths{
  files=append(files,metainfo.FileInfo{Path:strings.Split(path,"/"),Length:22})
 }
 info:=metainfo.Info{Name:torrentName,PieceLength:1<<20,Files:files,Pieces:make([]byte,20)}
 raw,err:=bencode.Marshal(info)
 if err!=nil{t.Fatal(err)}
 tt,_,err:=client.AddTorrentSpec(&torrent.TorrentSpec{AddTorrentOpts:torrent.AddTorrentOpts{
  InfoHash:metainfo.HashBytes(raw),InfoBytes:raw,
  Storage:allCompleteStorage{},DisableInitialPieceCheck:true,
 }})
 if err!=nil{t.Fatal(err)}
 downloads:=t.TempDir()
 // Write every file exactly where anacrolix reports it, so the paths the engine
 // deletes are the paths on disk.
 owned:=make([]string,0,len(paths))
 for _,file:=range tt.Files(){
  rel:=filepath.ToSlash(file.Path())
  owned=append(owned,rel)
  local:=filepath.Join(downloads,filepath.FromSlash(rel))
  if err:=os.MkdirAll(filepath.Dir(local),0700);err!=nil{t.Fatal(err)}
  if err:=os.WriteFile(local,[]byte("payload bytes"),0600);err!=nil{t.Fatal(err)}
 }
 item:=&entry{hash:hash,torrent:tt,validated:true}
 db:=testPINDB(t)
 if _,err:=db.Exec("CREATE TABLE torrents(hash TEXT PRIMARY KEY,rejection_reason TEXT NOT NULL DEFAULT '')");err!=nil{t.Fatal(err)}
 if _,err:=db.Exec("INSERT INTO torrents(hash,rejection_reason) VALUES(?,?)",hash,"");err!=nil{t.Fatal(err)}
 e:=&engine{client:client,db:db,entries:map[string]*entry{hash:item},downloads:downloads,cacheDir:t.TempDir()}
 return e,owned
}

// TestRemovingLiveTorrentDeletesItsFiles is the production case: a completed
// torrent whose files are on disk. Removing it must delete the payload.
//
// The regression it guards: the remove branch dropped the torrent and set
// item.torrent=nil before collecting the file list, so torrentFilesLocked fell
// back to fileManifest, which is nil unless the Files dialog was opened. The
// queue row was deleted and the files stayed on disk with nothing left in the
// UI pointing at them.
func TestRemovingLiveTorrentDeletesItsFiles(t *testing.T){
 hash:=strings.Repeat("e",40)
 e,owned:=liveDeleteEngine(t,hash,"Movie.2024.1080p",[]string{"movie.mkv","movie.en.srt"})
 if len(owned)==0{t.Fatal("fixture torrent reported no files")}
 for _,rel:=range owned{
  if !exists(filepath.Join(e.downloads,filepath.FromSlash(rel))){
   t.Fatalf("fixture file missing before delete: %s",rel)
  }
 }
 if err:=e.action(hash,"remove",true);err!=nil{t.Fatal(err)}
 for _,rel:=range owned{
  if exists(filepath.Join(e.downloads,filepath.FromSlash(rel))){
   t.Fatalf("file survived the delete of a live torrent: %s",rel)
  }
 }
 if e.entries[hash]!=nil{t.Fatal("queue entry survived the delete")}
}

// TestRemovingLiveTorrentKeepsForeignFiles: another queued torrent claims the
// same path, so the shared file must survive.
func TestRemovingLiveTorrentKeepsForeignFiles(t *testing.T){
 hash:=strings.Repeat("f",40)
 e,owned:=liveDeleteEngine(t,hash,"Shared",[]string{"movie.mkv"})
 if len(owned)!=1{t.Fatalf("fixture torrent reported %d files, want 1",len(owned))}
 shared:=filepath.Join(e.downloads,filepath.FromSlash(owned[0]))
 other:=strings.Repeat("1",40)
 otherManifest:=torrentFileManifest{Files:[]manifestFile{{Path:owned[0],Size:22,Selected:true}},Total:1}
 e.entries[other]=&entry{hash:other,fileManifest:&otherManifest,validated:true}
 if err:=e.action(hash,"remove",true);err!=nil{t.Fatal(err)}
 if !exists(shared){t.Fatal("deleted a file another torrent still owns")}
}
