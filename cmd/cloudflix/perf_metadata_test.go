package main

// refreshMetadata runs on a 10s ticker and calls TMDB once per movie, one
// after another. With a 200ms API that is 1 second for 5 newly queued movies,
// serially, on the one goroutine that also refreshes the rest of the catalog.
// These benchmarks price the per-lookup latency so the parallelism change can
// be justified by a number.

import (
 "context"
 "net/http"
 "net/http/httptest"
 "testing"
 "time"
)

// tmdbStub serves a valid TMDB search response after a fixed delay, so a
// lookup costs what a real network call costs without leaving the machine.
func tmdbStub(b *testing.B,delay time.Duration)*httptest.Server{
 b.Helper()
 body:=`{"results":[{"title":"Movie","release_date":"2026-01-01","poster_path":"/p.jpg","backdrop_path":"/b.jpg","vote_average":7.5,"genre_ids":[28]}]}`
 server:=httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
  time.Sleep(delay)
  w.Header().Set("Content-Type","application/json")
  w.Write([]byte(body))
 }))
 b.Cleanup(server.Close)
 return server
}

// BenchmarkMetadataLookupSerial is the current shape: N lookups, one at a time.
func BenchmarkMetadataLookupSerial(b *testing.B){
 server:=tmdbStub(b,2*time.Millisecond)
 b.ReportAllocs()
 b.ResetTimer()
 for i:=0;i<b.N;i++{
  for n:=0;n<5;n++{
   lookupMovieURL(context.Background(),server.URL+"/search","Movie","2026")
  }
 }
}

// BenchmarkMetadataLookupParallel is the same five lookups issued together, as
// refreshMetadata now does.
func BenchmarkMetadataLookupParallel(b *testing.B){
 server:=tmdbStub(b,2*time.Millisecond)
 b.ReportAllocs()
 b.ResetTimer()
 for i:=0;i<b.N;i++{
  done:=make(chan struct{},5)
  for n:=0;n<5;n++{
   go func(){
    lookupMovieURL(context.Background(),server.URL+"/search","Movie","2026")
    done<-struct{}{}
   }()
  }
  for n:=0;n<5;n++{<-done}
 }
}
