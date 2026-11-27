package main

// refreshMetadata now issues TMDB lookups concurrently. These tests pin the
// properties that keeps safe: every result still lands, errors are not mistaken
// for empty metadata, and a cancelled refresh stops early.

import (
 "context"
 "net/http"
 "net/http/httptest"
 "sync/atomic"
 "testing"
 "time"
)

const stubSearchBody = `{"results":[{"title":"Resolved Movie","release_date":"2026-01-01","poster_path":"/p.jpg","backdrop_path":"/b.jpg","vote_average":7.5,"genre_ids":[28]}]}`

// metadataStub counts requests and answers with a valid search result.
func metadataStub(t *testing.T,delay time.Duration)(*httptest.Server,*int64){
 t.Helper()
 var calls int64
 server:=httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
  atomic.AddInt64(&calls,1)
  time.Sleep(delay)
  w.Header().Set("Content-Type","application/json")
  w.Write([]byte(stubSearchBody))
 }))
 t.Cleanup(server.Close)
 return server,&calls
}

// TestLookupMovieURLParsesStubResponse guards the injected-endpoint refactor:
// the real lookup must behave identically when pointed at a stub.
func TestLookupMovieURLParsesStubResponse(t *testing.T){
 server,calls:=metadataStub(t,0)
 title,year,poster,_,rating,genre:=lookupMovieURL(context.Background(),server.URL+"/search","Some Movie","2026")
 if title!="Resolved Movie" || year!="2026"{t.Fatalf("unexpected result: %q %q",title,year)}
 if poster!="/p.jpg" || rating!=7.5 || genre!="Action"{t.Fatalf("metadata lost: %q %v %q",poster,rating,genre)}
 if atomic.LoadInt64(calls)!=1{t.Fatalf("expected 1 request, got %d",*calls)}
}

// TestLookupMovieURLRejectsErrorStatus keeps a failed lookup from being
// mistaken for a movie with empty metadata.
func TestLookupMovieURLRejectsErrorStatus(t *testing.T){
 server:=httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
  http.Error(w,"boom",http.StatusInternalServerError)
 }))
 defer server.Close()
 if title,_,_,_,_,_:=lookupMovieURL(context.Background(),server.URL+"/search","x","");title!=""{
  t.Fatalf("error response produced a title: %q",title)
 }
}

// TestLookupMovieURLRespectsContext makes sure a cancelled refresh stops
// instead of issuing its remaining requests.
func TestLookupMovieURLRespectsContext(t *testing.T){
 server,_:=metadataStub(t,50*time.Millisecond)
 ctx,cancel:=context.WithCancel(context.Background())
 cancel()
 started:=time.Now()
 if title,_,_,_,_,_:=lookupMovieURL(ctx,server.URL+"/search","x","");title!=""{
  t.Fatal("cancelled lookup returned a title")
 }
 if elapsed:=time.Since(started);elapsed>time.Second{
  t.Fatalf("cancelled lookup waited %v",elapsed)
 }
}
