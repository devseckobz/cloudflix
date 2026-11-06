package main

import (
 "fmt"
 "os"
 "path/filepath"
 "testing"
)

func TestCleanupRemovesFilesBeyondPreviewLimit(t *testing.T){
 downloads:=t.TempDir()
 dir:=filepath.Join(downloads,"orphaned-release")
 if err:=os.Mkdir(dir,0700);err!=nil{t.Fatal(err)}
 const count=maxLeftoverFiles+1
 for i:=0;i<count;i++{
  if err:=os.WriteFile(filepath.Join(dir,fmt.Sprintf("%03d.nfo",i)),[]byte("x"),0600);err!=nil{t.Fatal(err)}
 }
 e:=&engine{downloads:downloads}
 preview,err:=e.scanLeftovers()
 if err!=nil || preview.Count!=count || len(preview.Files)!=maxLeftoverFiles || !preview.Truncated{
  t.Fatalf("preview count=%d listed=%d truncated=%t: %v",preview.Count,len(preview.Files),preview.Truncated,err)
 }
 removed,err:=e.removeLeftovers()
 if err!=nil || removed.Count!=count || removed.Bytes!=count{
  t.Fatalf("removed count=%d bytes=%d: %v",removed.Count,removed.Bytes,err)
 }
 if _,err:=os.Stat(dir);!os.IsNotExist(err){t.Fatalf("release directory remains: %v",err)}
}
