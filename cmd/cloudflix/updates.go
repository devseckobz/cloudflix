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
 githubLatestRelease = "https://api.github.com/repos/devseckobz/cloudflix/releases/latest"
 updaterPathUnit = "/etc/systemd/system/cloudflix-update.path"
 updaterRootHelper = "/usr/local/libexec/cloudflix/web-update-worker.sh"
 updaterStatusFile = "/var/lib/cloudflix-update/status.json"
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

func latestRelease(ctx context.Context)(string,error){
 releaseCache.Lock()
 defer releaseCache.Unlock()
 if time.Now().Before(releaseCache.expires) {
  return releaseCache.tag,releaseCache.err
 }
 reqCtx,cancel:=context.WithTimeout(ctx,6*time.Second)
 defer cancel()
 req,err:=http.NewRequestWithContext(reqCtx,http.MethodGet,githubLatestRelease,nil)
 if err!=nil{return "",err}
 req.Header.Set("Accept","application/vnd.github+json")
 req.Header.Set("User-Agent","Cloudflix-Updater")
 client:=&http.Client{
  Timeout:6*time.Second,
  CheckRedirect:func(*http.Request,[]*http.Request)error{return http.ErrUseLastResponse},
 }
 res,err:=client.Do(req)
 var tag string
 if err==nil{
  defer res.Body.Close()
  if res.StatusCode!=http.StatusOK{
   err=errors.New("GitHub Releases is temporarily unavailable")
  }else{
   var release struct{
    TagName string `json:"tag_name"`
    Draft bool `json:"draft"`
    Prerelease bool `json:"prerelease"`
   }
   err=json.NewDecoder(io.LimitReader(res.Body,32<<10)).Decode(&release)
   if err==nil{
    if release.Draft || release.Prerelease || !stableVersion.MatchString(release.TagName){
     err=errors.New("no stable Cloudflix release found")
    }else{
     tag=release.TagName
    }
   }
  }
 }
 if err!=nil{
  err=errors.New("Unable to check GitHub Releases right now")
 }
 releaseCache.tag=tag
 releaseCache.err=err
 if err!=nil{releaseCache.expires=time.Now().Add(25*time.Second)}else{releaseCache.expires=time.Now().Add(2*time.Minute)}
 return tag,err
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
 s:=readUpdateStatus()
 if _,err:=os.Lstat(a.updateRequestPath());err==nil{s=updateStatus{State:"queued",Message:"Waiting for the system updater"}}
 tag,err:=latestRelease(r.Context())
 result:=map[string]any{
  "currentVersion":version,
  "latestVersion":tag,
  "updateAvailable":err==nil && newerVersion(version,tag),
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
