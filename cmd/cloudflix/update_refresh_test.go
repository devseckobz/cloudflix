package main

import (
 "context"
 "encoding/json"
 "net/http"
 "net/http/httptest"
 "strconv"
 "sync/atomic"
 "testing"
 "time"
)

func TestManualUpdateCheckRefreshesTheSharedCache(t *testing.T){
 var calls atomic.Int32
 hits:=stubReleaseServer(t,func(w http.ResponseWriter,r *http.Request){
  writeRelease(w,"v0.1."+strconv.Itoa(int(calls.Add(1))))
 })
 check:=func(query,want string){
  t.Helper()
  w:=httptest.NewRecorder()
  (&application{dataDir:t.TempDir()}).updateInfo(w,httptest.NewRequest(http.MethodGet,"/api/updates"+query,nil))
  var status struct{LatestVersion string}
  if err:=json.Unmarshal(w.Body.Bytes(),&status);err!=nil || status.LatestVersion!=want{
   t.Fatalf("latest = %q, want %q: %v",status.LatestVersion,want,err)
  }
 }
 check("","v0.1.1")
 check("","v0.1.1")
 check("?refresh=1","v0.1.2")
 check("?refresh=1","v0.1.2")
 check("","v0.1.2")
 if got:=atomic.LoadInt32(hits);got!=2{t.Fatalf("GitHub requests = %d, want 2",got)}
}

func TestManualAndAutomaticChecksRespectGitHubQuota(t *testing.T){
 for _,status:=range []int{http.StatusOK,http.StatusTooManyRequests}{
  t.Run(strconv.Itoa(status),func(t *testing.T){
   hits:=stubReleaseServer(t,func(w http.ResponseWriter,r *http.Request){
    w.Header().Set("X-RateLimit-Remaining","1")
    w.Header().Set("X-RateLimit-Reset",strconv.FormatInt(time.Now().Add(30*time.Minute).Unix(),10))
    if status==http.StatusOK{writeRelease(w,"v0.1.42");return}
    w.Header().Set("Retry-After","10")
    w.WriteHeader(status)
   })
   _,_=latestRelease(context.Background())
   releaseCache.Lock()
   delay:=max(time.Until(releaseCache.expires),time.Until(releaseCache.quotaReset))
   releaseCache.Unlock()
   if delay<29*time.Minute{t.Fatalf("retry delay too short: %v",delay)}
   if _,err:=checkLatestRelease(context.Background(),true);err==nil{t.Fatal("manual check did not report backoff")}
   _,_=latestRelease(context.Background())
   if got:=atomic.LoadInt32(hits);got!=1{t.Fatalf("GitHub requests = %d, want 1",got)}
  })
 }
}
