package main

import (
 "net/http"
 "net/http/httptest"
 "strings"
 "testing"
)

func TestIdentifyMovie(t *testing.T) {
 title,year:=identify("Big.Buck.Bunny.2008.1080p.BluRay.mkv")
 if title!="Big Buck Bunny" || year!="2008"{t.Fatalf("got %q %q",title,year)}
}
func TestOriginProtection(t *testing.T){
 handler:=securityHeaders(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){w.WriteHeader(http.StatusOK)}))
 for _,tc:=range []struct{origin string; status int}{
  {"https://evil.example",403},
  {"https://cloudflix.example",200},
 }{
  req:=httptest.NewRequest("POST","https://cloudflix.example/api/magnets",strings.NewReader("{}"))
  req.Header.Set("Content-Type","application/json")
  req.Header.Set("Origin",tc.origin)
  rr:=httptest.NewRecorder()
  handler.ServeHTTP(rr,req)
  if rr.Code!=tc.status{t.Errorf("origin %s got %d expected %d",tc.origin,rr.Code,tc.status)}
 }
}

func TestOriginWithDefaultPort80(t *testing.T){
 for _,tc:=range []struct{origin,host string;allowed bool}{
  {"http://198.51.100.20","198.51.100.20:80",true},
  {"http://198.51.100.20:80","198.51.100.20",true},
  {"https://cloudflix.example","cloudflix.example:443",true},
  {"http://198.51.100.20:3000","198.51.100.20:80",false},
  {"http://untrusted.example","198.51.100.20:80",false},
  {"null","198.51.100.20:80",false},
 }{
  if sameOriginHost(tc.origin,tc.host)!=tc.allowed{t.Errorf("%s vs %s: expected %v",tc.origin,tc.host,tc.allowed)}
 }
}
