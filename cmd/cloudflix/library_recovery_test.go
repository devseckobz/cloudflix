package main

import (
 "context"
 "os"
 "path/filepath"
 "strings"
 "testing"
 "time"
)

func TestLibraryRecoveryKeepsAReplacementAtTheOriginalPath(t *testing.T){
 hash:=strings.Repeat("a",40)
 e:=&engine{db:testPINDB(t),downloads:t.TempDir(),libraryDir:t.TempDir(),entries:map[string]*entry{}}
 if _,err:=e.db.Exec("CREATE TABLE torrents(hash TEXT PRIMARY KEY, library TEXT); INSERT INTO torrents(hash) VALUES(?)",hash);err!=nil{t.Fatal(err)}
 original:="movie.mkv"
 current:=filepath.Join(e.downloads,original)
 if err:=os.WriteFile(current,[]byte("source"),0600);err!=nil{t.Fatal(err)}
 info,err:=os.Stat(current)
 if err!=nil{t.Fatal(err)}
 replacementTime:=info.ModTime().Add(time.Hour)
 if err:=os.WriteFile(current,[]byte("new mk"),0600);err!=nil{t.Fatal(err)}
 if err:=os.Chtimes(current,replacementTime,replacementTime);err!=nil{t.Fatal(err)}
 stored:=strings.Repeat("b",64)+".mp4"
 if err:=os.WriteFile(filepath.Join(e.libraryDir,stored),[]byte("converted"),0600);err!=nil{t.Fatal(err)}
 item:=&entry{hash:hash,library:&libraryMovie{
  Files:[]libraryFile{{Path:"movie.mp4",Stored:stored,Size:9}},
  Owned:[]string{original},
  Originals:[]libraryOriginal{{Path:original,Size:info.Size(),Modified:info.ModTime().UnixNano()}},
 }}
 e.entries[hash]=item
 e.removeLibraryOriginalsLocked(item)
 if raw,err:=os.ReadFile(current);err!=nil || string(raw)!="new mk"{t.Fatalf("replacement changed: %q, %v",raw,err)}
 if len(item.library.Originals)!=0 || len(item.library.Owned)!=0{t.Fatal("replacement is still marked for deletion")}
}

func TestRemovingTorrentCancelsPlaybackPreparation(t *testing.T){
 hash:=strings.Repeat("c",40)
 e,owned:=liveDeleteEngine(t,hash,"Release",[]string{"movie.mkv"})
 e.playback=newMKVManager()
 ctx,cancel:=context.WithCancel(t.Context())
 defer cancel()
 key:=mkvKey(hash,owned[0])
 e.playback.jobs[key]=&mkvJob{state:"processing",ctx:ctx,cancel:cancel}
 if err:=e.action(hash,"remove",true);err!=nil{t.Fatal(err)}
 if ctx.Err()==nil || e.playback.jobs[key]!=nil{t.Fatal("removed torrent still has an active conversion")}
}
