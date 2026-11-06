package main

import (
 "os"
 "path/filepath"
 "sort"
 "strings"
 "testing"
)

// A release folder is not just the movie: it ships .nfo scene notes, artwork and
// extra subtitle formats. These tests cover the two defects that made a deleted
// movie's folder invisible:
//
//   1. delete unlinked only .mp4/.mkv/.srt, leaving the sidecars behind;
//   2. the leftover scan ignored those extensions, so it reported count=0 and
//      the Clean up button stayed disabled even though files were on disk.

// remainingFiles lists every regular file under the downloads root, relative to it.
func remainingFiles(t *testing.T,root string)[]string{
 t.Helper()
 var out []string
 err:=filepath.Walk(root,func(path string,info os.FileInfo,err error)error{
  if err!=nil{return nil}
  if info.IsDir(){return nil}
  rel,relErr:=filepath.Rel(root,path)
  if relErr!=nil{return nil}
  out=append(out,filepath.ToSlash(rel))
  return nil
 })
 if err!=nil{t.Fatal(err)}
 sort.Strings(out)
 return out
}

// TestDeletingATorrentRemovesItsSidecarFiles: the movie and its .nfo/.jpg must
// all go, so the emptied release folder can be pruned. Leaving sidecars behind
// is what produced an on-disk folder no queue entry accounted for.
func TestDeletingATorrentRemovesItsSidecarFiles(t *testing.T){
 hash:=strings.Repeat("c",40)
 // readme.txt is included on purpose: a file the TORRENT declares is ours to
 // delete whatever its extension, unlike a loose .txt the scanner refuses below.
 e,owned:=liveDeleteEngine(t,hash,"Release.2026",[]string{"movie.mkv","movie.en.srt","movie.nfo","poster.jpg","readme.txt"})
 if len(owned)!=5{t.Fatalf("fixture torrent reported %d files, want 5: %v",len(owned),owned)}
 for _,rel:=range owned{
  if !exists(filepath.Join(e.downloads,filepath.FromSlash(rel))){
   t.Fatalf("fixture file missing before delete: %s",rel)
  }
 }
 if err:=e.action(hash,"remove",true);err!=nil{t.Fatal(err)}
 if left:=remainingFiles(t,e.downloads);len(left)!=0{
  t.Fatalf("delete left files behind: %v",left)
 }
}

// TestCleanupSeesSidecarOnlyLeftovers is the reported bug: a release folder whose
// media is gone but whose sidecars remain must be reported, not silently ignored.
func TestCleanupSeesSidecarOnlyLeftovers(t *testing.T){
 downloads:=t.TempDir()
 files:=[]string{"Movie.2024.1080p/movie.nfo","Movie.2024.1080p/poster.jpg","Movie.2024.1080p/movie.en.vtt"}
 for _,rel:=range files{
  local:=filepath.Join(downloads,filepath.FromSlash(rel))
  if err:=os.MkdirAll(filepath.Dir(local),0700);err!=nil{t.Fatal(err)}
  if err:=os.WriteFile(local,[]byte("x"),0600);err!=nil{t.Fatal(err)}
 }
 e:=&engine{downloads:downloads,entries:map[string]*entry{}}

 preview,err:=e.scanLeftovers()
 if err!=nil{t.Fatal(err)}
 if preview.Count!=len(files){
  t.Fatalf("scan reported %d sidecar leftovers, want %d: %+v",preview.Count,len(files),preview.Files)
 }
 removed,err:=e.removeLeftovers()
 if err!=nil{t.Fatal(err)}
 if removed.Count!=len(files){
  t.Fatalf("cleanup removed %d, want %d",removed.Count,len(files))
 }
 if left:=remainingFiles(t,downloads);len(left)!=0{
  t.Fatalf("sidecars survived cleanup: %v",left)
 }
 if exists(filepath.Join(downloads,"Movie.2024.1080p")){
  t.Fatal("the emptied release folder must be pruned")
 }
}

// TestCleanupProtectsALiveTorrentsSidecars is the guard for widening the scan:
// once sidecars count as reclaimable, a queued torrent's own .nfo/.jpg must be
// treated as owned, or Clean up would delete files from a live download.
func TestCleanupProtectsALiveTorrentsSidecars(t *testing.T){
 hash:=strings.Repeat("d",40)
 e,owned:=liveDeleteEngine(t,hash,"Still.Here.2026",[]string{"movie.mkv","movie.nfo","poster.jpg"})
 if len(owned)!=3{t.Fatalf("fixture torrent reported %d files, want 3",len(owned))}
 preview,err:=e.scanLeftovers()
 if err!=nil{t.Fatal(err)}
 if preview.Count!=0{
  t.Fatalf("a queued torrent's own files were reported as leftovers: %+v",preview.Files)
 }
 if _,err:=e.removeLeftovers();err!=nil{t.Fatal(err)}
 for _,rel:=range owned{
  if !exists(filepath.Join(e.downloads,filepath.FromSlash(rel))){
   t.Fatalf("cleanup deleted a live torrent's file: %s",rel)
  }
 }
}

// TestCleanupLeavesUnrelatedFilesAlone: the allowlist must not become a licence
// to delete arbitrary files that happen to sit in the downloads folder.
func TestCleanupLeavesUnrelatedFilesAlone(t *testing.T){
 downloads:=t.TempDir()
 keep:=[]string{"notes.pdf","archive.zip","script.sh","database.sqlite"}
 for _,rel:=range keep{
  local:=filepath.Join(downloads,rel)
  if err:=os.WriteFile(local,[]byte("x"),0600);err!=nil{t.Fatal(err)}
 }
 e:=&engine{downloads:downloads,entries:map[string]*entry{}}
 if _,err:=e.removeLeftovers();err!=nil{t.Fatal(err)}
 for _,rel:=range keep{
  if !exists(filepath.Join(downloads,rel)){
   t.Fatalf("cleanup deleted an unrelated file: %s",rel)
  }
 }
}

// TestSidecarAllowanceStillRejectsEscapingPaths: widening which extensions are
// reclaimable must not widen which paths may be touched.
func TestSidecarAllowanceStillRejectsEscapingPaths(t *testing.T){
 for _,path:=range []string{"../outside.nfo","/etc/passwd.jpg","a/../../b.nfo","with\\backslash.nfo","","a/./b.nfo"}{
  if safeTorrentPath(path){
   t.Errorf("unsafe path accepted: %q",path)
  }
 }
 for _,path:=range []string{"Movie.2024/movie.nfo","Movie.2024/Subs/English.srt","poster.jpg"}{
  if !safeTorrentPath(path){
   t.Errorf("safe path rejected: %q",path)
  }
 }
}
