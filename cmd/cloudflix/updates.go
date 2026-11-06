package main

import (
 "context"
 "encoding/json"
 "errors"
 "io"
 "net/http"
 "os"
 "path/filepath"
 "regexp"
 "strconv"
 "strings"
 "sync"
 "time"
)

const (
 updaterPathUnit = "/etc/systemd/system/cloudflix-update.path"
 updaterRootHelper = "/usr/local/libexec/cloudflix/web-update-worker.sh"
 updaterStatusFile = "/var/lib/cloudflix-update/status.json"
)

// A variable so tests can point the check at a local server.
var githubLatestRelease = "https://api.github.com/repos/devseckobz/cloudflix/releases/latest"

const (
 // Automatic checks share one cache per server. At most 48 checks/hour
 // leaves room for manual refreshes in GitHub's unauthenticated budget.
 releaseSuccessTTL = 75*time.Second
 releaseManualCooldown = 5*time.Second
 // The first failure is retried quickly, then the delay doubles. Retrying a
 // failure every 25s would issue 144 requests/hour and keep the IP rate
 // limited forever, which is exactly how a real update goes undetected.
 releaseErrorTTL = 25*time.Second
 releaseMaxErrorTTL = 15*time.Minute
)

var stableVersion = regexp.MustCompile(`^v[0-9]+\.[0-9]+\.[0-9]+$`)

type updateStatus struct {
 State string `json:"state"`
 Message string `json:"message"`
 UpdatedAt int64 `json:"updatedAt"`
}

var releaseCache struct{
 sync.Mutex
 tag string
 err error
 expires time.Time
 manualAfter time.Time
 quotaReset time.Time
 // Consecutive failures drive the backoff, and the last good tag is kept so a
 // transient GitHub failure never erases an update we already detected.
 failures int
 lastGood string
}

func newerVersion(current,latest string) bool {
 if !stableVersion.MatchString(current) || !stableVersion.MatchString(latest){return false}
 lhs:=strings.Split(strings.TrimPrefix(current,"v"),".")
 rhs:=strings.Split(strings.TrimPrefix(latest,"v"),".")
 for i:=0;i<3;i++{
  a,errA:=strconv.ParseUint(lhs[i],10,64)
  b,errB:=strconv.ParseUint(rhs[i],10,64)
  if errA!=nil || errB!=nil{return false}
  if b>a{return true}
  if b<a{return false}
 }
 return false
}

// latestRelease returns the newest stable release tag.
//
// A failed check must never look like "you are up to date": the last tag we
// successfully read is returned alongside the error so callers can keep
// showing a known update while GitHub is unreachable or rate limited.
func latestRelease(ctx context.Context)(string,error){
 return checkLatestRelease(ctx,false)
}

func checkLatestRelease(ctx context.Context,manual bool)(string,error){
 releaseCache.Lock()
 defer releaseCache.Unlock()
 now:=time.Now()
 if now.Before(releaseCache.quotaReset) && releaseCache.err==nil{
  if manual{return releaseCache.tag,errors.New("GitHub's request quota is low. Checks will resume when it resets.")}
  return releaseCache.tag,nil
 }
 if now.Before(releaseCache.expires) && (!manual || releaseCache.err!=nil || now.Before(releaseCache.manualAfter)){
  return releaseCache.tag,releaseCache.err
 }
 tag,retryAfter,err:=fetchLatestRelease(ctx)
 now=time.Now()
 if manual{releaseCache.manualAfter=now.Add(releaseManualCooldown)}
 if err!=nil{
  releaseCache.failures++
  // Keep the last good tag so a transient failure does not erase a detected
  // update. The error is still reported so the UI can explain the check.
  releaseCache.tag=releaseCache.lastGood
  releaseCache.err=errors.New("Unable to check GitHub Releases right now")
  releaseCache.expires=now.Add(releaseRetryDelay(releaseCache.failures,retryAfter))
  return releaseCache.tag,releaseCache.err
 }
 releaseCache.failures=0
 releaseCache.tag=tag
 releaseCache.lastGood=tag
 releaseCache.err=nil
 releaseCache.expires=now.Add(releaseSuccessTTL)
 releaseCache.quotaReset=now.Add(retryAfter)
 return tag,nil
}

// releaseRetryDelay backs off exponentially so a rate-limited or offline VPS
// stops hammering GitHub. Without this the 4.5s Settings poll retries a
// failure every 25s (144 requests/hour against a 60/hour limit) and the IP
// stays rate limited, so a real release is never detected.
func releaseRetryDelay(failures int,retryAfter time.Duration)time.Duration{
 delay:=releaseErrorTTL
 for i:=1;i<failures && delay<releaseMaxErrorTTL;i++{
  delay*=2
 }
 if delay>releaseMaxErrorTTL{delay=releaseMaxErrorTTL}
 return max(delay,retryAfter)
}

// fetchLatestRelease performs one GitHub request. It returns the tag, any
// server-requested retry delay, and an error.
func fetchLatestRelease(ctx context.Context)(string,time.Duration,error){
 reqCtx,cancel:=context.WithTimeout(ctx,6*time.Second)
 defer cancel()
 req,err:=http.NewRequestWithContext(reqCtx,http.MethodGet,githubLatestRelease,nil)
 if err!=nil{return "",0,err}
 req.Header.Set("Accept","application/vnd.github+json")
 req.Header.Set("User-Agent","Cloudflix-Updater")
 client:=&http.Client{
  Timeout:6*time.Second,
  CheckRedirect:func(*http.Request,[]*http.Request)error{return http.ErrUseLastResponse},
 }
 res,err:=client.Do(req)
 if err!=nil{return "",0,err}
 defer res.Body.Close()
 if res.StatusCode!=http.StatusOK{
  delay:=retryAfterDelay(res.Header)
  if delay==0 && (res.StatusCode==http.StatusForbidden || res.StatusCode==http.StatusTooManyRequests){
   delay=time.Minute
  }
  return "",delay,errors.New("GitHub Releases is temporarily unavailable")
 }
 var release struct{
  TagName string `json:"tag_name"`
  Draft bool `json:"draft"`
  Prerelease bool `json:"prerelease"`
 }
 if err=json.NewDecoder(io.LimitReader(res.Body,32<<10)).Decode(&release);err!=nil{
  return "",0,err
 }
 if release.Draft || release.Prerelease || !stableVersion.MatchString(release.TagName){
  return "",0,errors.New("no stable Cloudflix release found")
 }
 return release.TagName,retryAfterDelay(res.Header),nil
}

// Keep the longer of Retry-After and the quota reset. Reserve the last
// reported request for other GitHub activity on the VPS.
func retryAfterDelay(header http.Header)time.Duration{
 var delay time.Duration
 if value:=strings.TrimSpace(header.Get("Retry-After"));value!=""{
  if seconds,err:=strconv.Atoi(value);err==nil && seconds>0{
   delay=time.Duration(seconds)*time.Second
  }
 }
 if value:=strings.TrimSpace(header.Get("X-RateLimit-Remaining"));value=="0" || value=="1"{
  if reset,err:=strconv.ParseInt(strings.TrimSpace(header.Get("X-RateLimit-Reset")),10,64);err==nil{
   delay=max(delay,time.Until(time.Unix(reset,0)))
  }
 }
 return delay
}

func updateServiceInstalled()bool{
 for _,name:=range []string{updaterPathUnit,updaterRootHelper}{
  info,err:=os.Lstat(name)
  if err!=nil || !info.Mode().IsRegular(){return false}
 }
 return true
}

func readUpdateStatus() updateStatus {
 data,err:=os.ReadFile(updaterStatusFile)
 if err!=nil || len(data)>4096{return updateStatus{State:"idle"}}
 var s updateStatus
 if json.Unmarshal(data,&s)!=nil{return updateStatus{State:"idle"}}
 switch s.State{
 case "running","success","error":
 default:
  s=updateStatus{State:"idle"}
 }
 if s.State=="running" && s.UpdatedAt>0 && time.Since(time.Unix(s.UpdatedAt,0))>15*time.Minute{
  s=updateStatus{State:"error",Message:"An earlier update stopped unexpectedly. Check the VPS updater service."}
 }
 return s
}

func (a *application) updateRequestPath()string{
 return filepath.Join(a.dataDir,"update.request")
}

func (a *application) updateInfo(w http.ResponseWriter,r *http.Request){
 w.Header().Set("Cache-Control","no-store")
 s:=readUpdateStatus()
 if _,err:=os.Lstat(a.updateRequestPath());err==nil{s=updateStatus{State:"queued",Message:"Waiting for the system updater"}}
 tag,err:=checkLatestRelease(r.Context(),r.URL.Query().Get("refresh")=="1")
 // A failed check must not hide an update we already know about. latestRelease
 // returns the last good tag alongside the error, so keep reporting it and let
 // checkError explain that the check itself could not be refreshed.
 result:=map[string]any{
  "currentVersion":version,
  "latestVersion":tag,
  "updateAvailable":newerVersion(version,tag),
  "updaterReady":updateServiceInstalled(),
  "state":s.State,
  "message":s.Message,
  "updatedAt":s.UpdatedAt,
 }
 if err!=nil{result["checkError"]=err.Error()}
 respond(w,http.StatusOK,result)
}

// The web process can only place a fixed-name, zero-byte trigger in its
// own private data folder. A root-owned systemd.path unit consumes it.
// No arguments, URLs, scripts or paths are supplied by the client.
func (a *application) requestUpdate(w http.ResponseWriter,r *http.Request){
 var empty struct{}
 if !readJSON(w,r,&empty){return}
 if !updateServiceInstalled(){
  fail(w,http.StatusServiceUnavailable,"Enable the Settings updater by running update.sh from your VPS terminal once")
  return
 }
 state:=readUpdateStatus()
 if state.State=="running"{
  fail(w,http.StatusConflict,"A Cloudflix update is already running")
  return
 }
 latest,err:=latestRelease(r.Context())
 if err!=nil{
  fail(w,http.StatusServiceUnavailable,"Unable to check the latest GitHub release")
  return
 }
 if !newerVersion(version,latest){
  fail(w,http.StatusConflict,"Cloudflix is already up to date")
  return
 }
 file,err:=os.OpenFile(a.updateRequestPath(),os.O_CREATE|os.O_EXCL|os.O_WRONLY,0600)
 if errors.Is(err,os.ErrExist){
  fail(w,http.StatusConflict,"An update request is already queued")
  return
 }
 if err!=nil{
  fail(w,http.StatusInternalServerError,"Could not request the update")
  return
 }
 if err=file.Close();err!=nil{
  _=os.Remove(a.updateRequestPath())
  fail(w,http.StatusInternalServerError,"Could not finish the update request")
  return
 }
 respond(w,http.StatusAccepted,map[string]any{"queued":true,"message":"Update requested. The application will restart briefly."})
}
