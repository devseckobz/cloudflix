package main

import (
 "context"
 "encoding/json"
 "net/http"
 "net/http/httptest"
 "strconv"
 "strings"
 "sync/atomic"
 "testing"
 "time"
)

// resetReleaseCache clears the package-level cache between tests.
func resetReleaseCache(t *testing.T){
 t.Helper()
 releaseCache.Lock()
 releaseCache.tag=""
 releaseCache.err=nil
 releaseCache.expires=time.Time{}
 releaseCache.failures=0
 releaseCache.lastGood=""
 releaseCache.Unlock()
}

// stubReleaseServer points the release check at a local server for one test.
func stubReleaseServer(t *testing.T,handler http.HandlerFunc)*int32{
 t.Helper()
 var hits int32
 server:=httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
  atomic.AddInt32(&hits,1)
  handler(w,r)
 }))
 t.Cleanup(server.Close)
 previous:=githubLatestRelease
 githubLatestRelease=server.URL
 t.Cleanup(func(){githubLatestRelease=previous})
 resetReleaseCache(t)
 return &hits
}

func writeRelease(w http.ResponseWriter,tag string){
 w.Header().Set("Content-Type","application/json")
 _=json.NewEncoder(w).Encode(map[string]any{"tag_name":tag,"draft":false,"prerelease":false})
}

func TestReleaseVersionComparison(t *testing.T){
 tests:=[]struct{installed,latest string;available bool}{
  {"v0.1.7","v0.1.8",true},
  {"v0.1.8","v0.1.8",false},
  {"v0.1.9","v0.1.8",false},
  {"v0.9.99","v1.0.0",true},
  {"v1.10.0","v1.9.99",false},
  {"v0.1.7","invalid",false},
  {"dev","v0.1.8",false},
  {"v0.1.8-alpha","v0.1.9",false},
  {"v0.1.8","v0.1.8-alpha",false},
 }
 for _,test:=range tests{
  if got:=newerVersion(test.installed,test.latest);got!=test.available{
   t.Errorf("%s -> %s: got %v, want %v",test.installed,test.latest,got,test.available)
  }
 }
}

func TestWebUpdaterCannotAcceptRemoteCommandsOrURLs(t *testing.T){
 a:=&application{dataDir:t.TempDir()}
 for _,body:=range []string{
  `{"url":"https://evil.example/update.sh"}`,
  `{"command":"rm -rf /"}`,
  `{"version":"v9.9.9"}`,
 }{
  req:=httptest.NewRequest(http.MethodPost,"http://127.0.0.1/api/updates",strings.NewReader(body))
  req.Header.Set("Content-Type","application/json")
  response:=httptest.NewRecorder()
  a.requestUpdate(response,req)
  if response.Code!=http.StatusBadRequest{
   t.Errorf("Unexpected response to remote command payload %s: HTTP %d",body,response.Code)
  }
 }
}

func TestUpdateRequestPathIsFixedInsideApplicationData(t *testing.T){
 dir:=t.TempDir()
 a:=&application{dataDir:dir}
 path:=a.updateRequestPath()
 if !strings.HasPrefix(path,dir) || !strings.HasSuffix(path,"update.request"){
  t.Fatalf("Unexpected update request path: %s",path)
 }
}

// A transient GitHub failure must not erase an update we already detected.
// This is the reported bug: the user saw "no update" while a release existed.
func TestTransientFailureKeepsLastKnownRelease(t *testing.T){
 var fail atomic.Bool
 stubReleaseServer(t,func(w http.ResponseWriter,r *http.Request){
  if fail.Load(){
   w.WriteHeader(http.StatusForbidden)
   return
  }
  writeRelease(w,"v0.1.22")
 })

 tag,err:=latestRelease(context.Background())
 if err!=nil || tag!="v0.1.22"{
  t.Fatalf("initial check: tag=%q err=%v",tag,err)
 }

 // Expire the cache, then make GitHub fail.
 releaseCache.Lock()
 releaseCache.expires=time.Time{}
 releaseCache.Unlock()
 fail.Store(true)

 tag,err=latestRelease(context.Background())
 if err==nil{
  t.Fatal("expected an error while GitHub is failing")
 }
 if tag!="v0.1.22"{
  t.Fatalf("a failed check discarded the last known release: tag=%q",tag)
 }
 if !newerVersion("v0.1.21",tag){
  t.Fatalf("update should still be reported available, got tag=%q",tag)
 }
}

// updateInfo must keep reporting updateAvailable during a transient failure.
func TestUpdateInfoReportsUpdateDuringTransientFailure(t *testing.T){
 var fail atomic.Bool
 stubReleaseServer(t,func(w http.ResponseWriter,r *http.Request){
  if fail.Load(){
   w.WriteHeader(http.StatusForbidden)
   return
  }
  writeRelease(w,"v0.1.22")
 })

 previous:=version
 version="v0.1.21"
 t.Cleanup(func(){version=previous})

 a:=&application{dataDir:t.TempDir()}
 read:=func()map[string]any{
  req:=httptest.NewRequest(http.MethodGet,"http://127.0.0.1/api/updates",nil)
  response:=httptest.NewRecorder()
  a.updateInfo(response,req)
  var body map[string]any
  if err:=json.Unmarshal(response.Body.Bytes(),&body);err!=nil{
   t.Fatalf("decode: %v",err)
  }
  return body
 }

 first:=read()
 if first["updateAvailable"]!=true{
  t.Fatalf("expected updateAvailable=true, got %v",first["updateAvailable"])
 }

 releaseCache.Lock()
 releaseCache.expires=time.Time{}
 releaseCache.Unlock()
 fail.Store(true)

 second:=read()
 if second["updateAvailable"]!=true{
  t.Fatalf("a transient failure hid the update: %v",second)
 }
 if second["latestVersion"]!="v0.1.22"{
  t.Fatalf("latestVersion lost during failure: %v",second["latestVersion"])
 }
 if second["checkError"]==nil{
  t.Fatal("expected checkError to explain the failed refresh")
 }
}

// The Settings page polls every 4.5s. A failing check must back off instead of
// retrying every 25s, which would exceed GitHub's 60 requests/hour and keep the
// IP rate limited forever.
func TestFailedCheckBacksOffInsteadOfHammeringGitHub(t *testing.T){
 hits:=stubReleaseServer(t,func(w http.ResponseWriter,r *http.Request){
  w.WriteHeader(http.StatusForbidden)
 })

 for i:=0;i<20;i++{
  if _,err:=latestRelease(context.Background());err==nil{
   t.Fatal("expected an error")
  }
 }
 if got:=atomic.LoadInt32(hits);got!=1{
  t.Fatalf("a failing check was retried %d times inside the cache window, want 1",got)
 }

 // After the first failure the delay must be the base error TTL (allowing for
 // the time elapsed since the check), not the old fixed 25s retry loop.
 releaseCache.Lock()
 delay:=time.Until(releaseCache.expires)
 releaseCache.Unlock()
 if delay<releaseErrorTTL-time.Second{
  t.Fatalf("retry delay %v is shorter than the base error TTL %v",delay,releaseErrorTTL)
 }
}

func TestReleaseRetryDelayGrowsAndIsCapped(t *testing.T){
 if got:=releaseRetryDelay(1,0);got!=releaseErrorTTL{
  t.Fatalf("first failure delay = %v, want %v",got,releaseErrorTTL)
 }
 if got:=releaseRetryDelay(2,0);got!=2*releaseErrorTTL{
  t.Fatalf("second failure delay = %v, want %v",got,2*releaseErrorTTL)
 }
 if got:=releaseRetryDelay(100,0);got!=releaseMaxErrorTTL{
  t.Fatalf("delay should cap at %v, got %v",releaseMaxErrorTTL,got)
 }
 // A server-provided Retry-After wins, and is still capped.
 if got:=releaseRetryDelay(1,90*time.Second);got!=90*time.Second{
  t.Fatalf("Retry-After should be honoured, got %v",got)
 }
 if got:=releaseRetryDelay(1,time.Hour);got!=releaseMaxErrorTTL{
  t.Fatalf("Retry-After should be capped at %v, got %v",releaseMaxErrorTTL,got)
 }
}

func TestRetryAfterDelayHonoursGitHubRateLimitHeaders(t *testing.T){
 header:=http.Header{}
 header.Set("Retry-After","42")
 if got:=retryAfterDelay(header);got!=42*time.Second{
  t.Fatalf("Retry-After: got %v, want 42s",got)
 }

 header=http.Header{}
 header.Set("X-RateLimit-Remaining","0")
 header.Set("X-RateLimit-Reset",strconv.FormatInt(time.Now().Add(5*time.Minute).Unix(),10))
 if got:=retryAfterDelay(header);got<=0 || got>5*time.Minute{
  t.Fatalf("rate-limit reset: got %v, want a positive delay up to 5m",got)
 }

 // A non-zero remaining count is not a rate limit.
 header=http.Header{}
 header.Set("X-RateLimit-Remaining","12")
 if got:=retryAfterDelay(header);got!=0{
  t.Fatalf("unexpected delay for a non-limited response: %v",got)
 }
}

// A successful check must reset the backoff so recovery is immediate.
func TestSuccessfulCheckResetsBackoff(t *testing.T){
 var fail atomic.Bool
 stubReleaseServer(t,func(w http.ResponseWriter,r *http.Request){
  if fail.Load(){
   w.WriteHeader(http.StatusForbidden)
   return
  }
  writeRelease(w,"v0.1.22")
 })

 fail.Store(true)
 for i:=0;i<3;i++{
  releaseCache.Lock()
  releaseCache.expires=time.Time{}
  releaseCache.Unlock()
  if _,err:=latestRelease(context.Background());err==nil{
   t.Fatal("expected an error")
  }
 }
 releaseCache.Lock()
 failures:=releaseCache.failures
 releaseCache.Unlock()
 if failures!=3{
  t.Fatalf("expected 3 consecutive failures, got %d",failures)
 }

 fail.Store(false)
 releaseCache.Lock()
 releaseCache.expires=time.Time{}
 releaseCache.Unlock()
 tag,err:=latestRelease(context.Background())
 if err!=nil || tag!="v0.1.22"{
  t.Fatalf("recovery check: tag=%q err=%v",tag,err)
 }
 releaseCache.Lock()
 failures=releaseCache.failures
 ttl:=time.Until(releaseCache.expires)
 releaseCache.Unlock()
 if failures!=0{
  t.Fatalf("success should reset the failure count, got %d",failures)
 }
 if ttl<=releaseErrorTTL{
  t.Fatalf("success should restore the long TTL, got %v",ttl)
 }
}

// A successful check is cached, so the 4.5s poll does not hit GitHub each time.
func TestSuccessfulCheckIsCached(t *testing.T){
 hits:=stubReleaseServer(t,func(w http.ResponseWriter,r *http.Request){
  writeRelease(w,"v0.1.22")
 })
 for i:=0;i<10;i++{
  if _,err:=latestRelease(context.Background());err!=nil{
   t.Fatalf("unexpected error: %v",err)
  }
 }
 if got:=atomic.LoadInt32(hits);got!=1{
  t.Fatalf("successful check hit GitHub %d times, want 1",got)
 }
}

// A draft, prerelease or non-stable tag is not an update.
func TestUnstableReleaseIsRejected(t *testing.T){
 cases:=[]struct{name string;body map[string]any}{
  {"draft",map[string]any{"tag_name":"v0.1.22","draft":true,"prerelease":false}},
  {"prerelease",map[string]any{"tag_name":"v0.1.22","draft":false,"prerelease":true}},
  {"unstable tag",map[string]any{"tag_name":"v0.1.22-beta","draft":false,"prerelease":false}},
 }
 for _,test:=range cases{
  t.Run(test.name,func(t *testing.T){
   stubReleaseServer(t,func(w http.ResponseWriter,r *http.Request){
    w.Header().Set("Content-Type","application/json")
    _=json.NewEncoder(w).Encode(test.body)
   })
   tag,err:=latestRelease(context.Background())
   if err==nil{
    t.Fatalf("expected an error for %s, got tag=%q",test.name,tag)
   }
  })
 }
}
