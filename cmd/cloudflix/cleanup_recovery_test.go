package main

import (
 "os"
 "path/filepath"
 "strings"
 "testing"
)

// TestCleanupReclaimsFilesLeftByAFailedDelete covers the reason the Clean up
// button has to exist: when a delete cannot unlink its files (or the queue row
// is gone but the payload survived), the files become orphans that no torrent
// claims. Clean up must find and remove them.
//
// This is the recovery half of the delete fix. The delete path now collects its
// file list before dropping the torrent, but any file that still escapes -- a
// permissions failure, a crash between the row and the unlink -- has to remain
// reclaimable rather than becoming invisible to every UI surface.
func TestCleanupReclaimsFilesLeftByAFailedDelete(t *testing.T){
 hash:=strings.Repeat("7",40)
 e,owned:=liveDeleteEngine(t,hash,"Orphaned.2026",[]string{"movie.mkv","movie.en.srt"})
 if len(owned)==0{t.Fatal("fixture torrent reported no files")}

 // Simulate the failure: the entry is gone from the queue, the payload is not.
 delete(e.entries,hash)

 preview,err:=e.scanLeftovers()
 if err!=nil{t.Fatalf("scan refused: %v",err)}
 if preview.Count!=len(owned){
  t.Fatalf("scan found %d orphaned files, want %d: %+v",preview.Count,len(owned),preview.Files)
 }

 removed,err:=e.removeLeftovers()
 if err!=nil{t.Fatalf("cleanup failed: %v",err)}
 if removed.Count!=len(owned){
  t.Fatalf("cleanup removed %d files, want %d",removed.Count,len(owned))
 }
 for _,rel:=range owned{
  if exists(filepath.Join(e.downloads,filepath.FromSlash(rel))){
   t.Fatalf("orphaned file survived cleanup: %s",rel)
  }
 }
}

// TestCleanupNeverTouchesAQueuedTorrentsFiles: a file the queue still owns is
// not garbage, so Clean up must leave it alone even though it is on disk.
func TestCleanupNeverTouchesAQueuedTorrentsFiles(t *testing.T){
 hash:=strings.Repeat("8",40)
 e,owned:=liveDeleteEngine(t,hash,"Still.Downloading.2026",[]string{"movie.mkv"})
 if len(owned)!=1{t.Fatalf("fixture torrent reported %d files, want 1",len(owned))}
 preview,err:=e.scanLeftovers()
 if err!=nil{t.Fatal(err)}
 if preview.Count!=0{
  t.Fatalf("a queued torrent's own file was reported as an orphan: %+v",preview.Files)
 }
 if _,err:=e.removeLeftovers();err!=nil{t.Fatal(err)}
 if !exists(filepath.Join(e.downloads,filepath.FromSlash(owned[0]))){
  t.Fatal("cleanup deleted a file the queue still owns")
 }
}

// TestCleanupReclaimsPartFilesOfDroppedDownloads: an interrupted download leaves
// a ".part" file, which is garbage only once nothing in the queue claims it.
func TestCleanupReclaimsPartFilesOfDroppedDownloads(t *testing.T){
 hash:=strings.Repeat("9",40)
 e,owned:=liveDeleteEngine(t,hash,"Interrupted.2026",[]string{"movie.mkv"})
 if len(owned)!=1{t.Fatalf("fixture torrent reported %d files, want 1",len(owned))}
 delete(e.entries,hash)
 part:=filepath.Join(e.downloads,filepath.FromSlash(owned[0])+partSuffix)
 if err:=os.WriteFile(part,[]byte("partial"),0600);err!=nil{t.Fatal(err)}
 removed,err:=e.removeLeftovers()
 if err!=nil{t.Fatal(err)}
 if removed.Count==0{t.Fatal("a dropped download's part file was not reclaimable")}
 if exists(part){t.Fatal("part file survived cleanup")}
}
