package main

import (
 "os"
 "path/filepath"
 "strings"
 "testing"
)

func TestCleanupRecoversPlaybackFilesFromDeletedMovies(t *testing.T){
 hash:=strings.Repeat("a",40)
 e,owned:=liveDeleteEngine(t,hash,"Queued.Release",[]string{"movie.mkv"})
 e.cacheDir=t.TempDir()
 e.playback=newMKVManager()
 queued:=mkvKey(hash,owned[0])
 deleted:=mkvKey(strings.Repeat("b",40),"Deleted.Release/movie.mkv")
 active:=mkvKey(strings.Repeat("c",40),"Active.Release/movie.mkv")
 e.playback.jobs[active+faststartSuffix]=&mkvJob{state:"processing"}
 keep:=[]string{queued+".mp4",queued+".faststart.2.vtt",active+".faststart.part.mp4","unrelated.mp4"}
 orphans:=[]string{deleted+".mp4",deleted+".json",deleted+".faststart.mp4",deleted+".faststart.2.vtt",
  deleted+".json.part",deleted+".part.mp4",deleted+".3.vtt.part"}
 for _,name:=range append(keep,orphans...){
  if err:=os.WriteFile(filepath.Join(e.cacheDir,name),[]byte("test"),0600);err!=nil{t.Fatal(err)}
 }
 preview,err:=e.scanLeftovers()
 if err!=nil || preview.Count!=len(orphans) || preview.Bytes!=int64(4*len(orphans)){
  t.Fatalf("unexpected preview: %+v, %v",preview,err)
 }
 removed,err:=e.removeLeftovers()
 if err!=nil || removed.Count!=preview.Count || removed.Bytes!=preview.Bytes{
  t.Fatalf("unexpected cleanup: %+v, %v",removed,err)
 }
 for _,name:=range orphans{
  if exists(filepath.Join(e.cacheDir,name)){t.Errorf("orphan survived: %s",name)}
 }
 for _,name:=range keep{
  if !exists(filepath.Join(e.cacheDir,name)){t.Errorf("protected file removed: %s",name)}
 }
}
