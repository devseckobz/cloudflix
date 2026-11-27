package main

// The Downloader header shows "Queue <n>" where n is the number of movies the
// catalog returns, not the number of rows the filter is showing. A delete must
// therefore remove the movie from the catalog even when a file cannot be
// unlinked, or the header keeps counting a movie the list no longer contains —
// which is how "Queue 1" appeared over an empty queue.

import (
 "encoding/json"
 "net/http"
 "net/http/httptest"
 "os"
 "path/filepath"
 "strings"
 "testing"
)

// stateMovieCount asks the real /api/state handler what the UI would count.
func stateMovieCount(t *testing.T,app *application)int{
 t.Helper()
 w:=httptest.NewRecorder()
 app.state(w,httptest.NewRequest(http.MethodGet,"/api/state",nil))
 if w.Code!=http.StatusOK{t.Fatalf("state failed: %d %s",w.Code,w.Body.String())}
 var body struct{Movies []map[string]any `json:"movies"`}
 if err:=json.Unmarshal(w.Body.Bytes(),&body);err!=nil{t.Fatal(err)}
 return len(body.Movies)
}

// TestRemovingWithUndeletableFilesStillDropsTheQueueRow pins the delete
// contract under file-system failure, which is the case that produced the
// stale header.
func TestRemovingWithUndeletableFilesStillDropsTheQueueRow(t *testing.T){
 hash:=strings.Repeat("c",40)
 e,_,downloads:=deleteTestEngine(t,hash,[]string{"Movie.2024/movie.mkv"})
 if got:=stateMovieCount(t,&application{engine:e});got!=1{
  t.Fatalf("fixture should hold one row, got %d",got)
 }
 // A directory where the file is expected makes the unlink fail on Linux.
 movie:=filepath.Join(downloads,"Movie.2024","movie.mkv")
 if err:=os.Remove(movie);err!=nil{t.Fatal(err)}
 if err:=os.MkdirAll(filepath.Join(movie,"blocker"),0700);err!=nil{t.Fatal(err)}

 if err:=e.action(hash,"remove",true);err==nil{
  t.Fatal("expected an error from the undeletable payload")
 }
 if _,stillThere:=e.entries[hash];stillThere{
  t.Fatal("the queue row survived a failed delete")
 }
 if got:=stateMovieCount(t,&application{engine:e});got!=0{
  t.Fatalf("header would still show Queue %d after the delete",got)
 }
 var rows int
 if err:=e.db.QueryRow("SELECT count(*) FROM torrents WHERE hash=?",hash).Scan(&rows);err!=nil{
  t.Fatal(err)
 }
 if rows!=0{t.Fatalf("%d database row(s) left behind",rows)}
}

// TestQueueCountMatchesRenderedRows is the general invariant behind the
// screenshot: the number in the header is the number of movies the catalog
// returns, so the two can never disagree.
func TestQueueCountMatchesRenderedRows(t *testing.T){
 hash:=strings.Repeat("d",40)
 e,_,_:=deleteTestEngine(t,hash,[]string{"Movie.2024/movie.mkv"})
 app:=&application{engine:e}
 if err:=e.action(hash,"remove",false);err!=nil{t.Fatal(err)}
 if got:=stateMovieCount(t,app);got!=0{
  t.Fatalf("queue count %d does not match the empty list",got)
 }
}
