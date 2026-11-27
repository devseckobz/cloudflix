package main

// The Downloader page polls /api/storage/leftovers every 10s, and each scan
// walks the downloads tree plus the playback/library caches. An out-of-process
// cache is one alternative to that walk; this prices the walk so the choice can
// be made on numbers instead of intuition.

import (
 "fmt"
 "os"
 "path/filepath"
 "testing"
)

// leftoverScanFixture builds an engine whose downloads tree holds files on
// disk, so a scan does real directory work.
func leftoverScanFixture(tb testing.TB,movies,filesPerMovie int)*engine{
 tb.Helper()
 e:=perfEngine(tb,movies,500)
 for i:=0;i<movies;i++{
  name:=fmt.Sprintf("Movie%02d",i)
  dir:=filepath.Join(e.downloads,name)
  if err:=os.MkdirAll(dir,0o700);err!=nil{tb.Fatal(err)}
  for f:=0;f<filesPerMovie;f++{
   path:=filepath.Join(dir,fmt.Sprintf("file%02d.srt",f))
   if err:=os.WriteFile(path,[]byte("subtitle\n"),0o600);err!=nil{tb.Fatal(err)}
  }
 }
 return e
}

// BenchmarkLeftoverScan prices one scanLeftovers call: the work the Downloader
// page repeats every 10 seconds while it is open.
func BenchmarkLeftoverScan(b *testing.B){
 for _,movies:=range []int{1,10,50}{
  b.Run(fmt.Sprintf("movies=%d",movies),func(b *testing.B){
   e:=leftoverScanFixture(b,movies,8)
   b.ReportAllocs()
   b.ResetTimer()
   for i:=0;i<b.N;i++{
    if _,err:=e.scanLeftovers();err!=nil{b.Fatal(err)}
   }
  })
 }
}
