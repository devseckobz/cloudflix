package main

import (
 "os"
 "path/filepath"
 "testing"
)

func TestCleanupFindsOrphanedYTSTextSidecars(t *testing.T){
 downloads:=t.TempDir()
 release:=filepath.Join(downloads,"The Odyssey (2026) [720p] [WEBRip] [YTS.GG - YTS.BZ]")
 if err:=os.Mkdir(release,0700);err!=nil{t.Fatal(err)}
 for _,path:=range []string{filepath.Join(release,"YIFYStatus.com.txt"),filepath.Join(release,"YTSYifyUP... (TOR).txt"),filepath.Join(downloads,"notes.txt")}{
  if err:=os.WriteFile(path,[]byte("fixture"),0600);err!=nil{t.Fatal(err)}
 }
 e:=&engine{downloads:downloads}
 preview,err:=e.scanLeftovers()
 if err!=nil || preview.Count!=2{t.Fatalf("preview: %+v, %v",preview,err)}
 removed,err:=e.removeLeftovers()
 if err!=nil || removed.Count!=2 || exists(release) || !exists(filepath.Join(downloads,"notes.txt")){
  t.Fatalf("cleanup: %+v, %v",removed,err)
 }
}
