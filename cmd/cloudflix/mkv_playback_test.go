package main

import (
 "encoding/json"
 "net/http"
 "net/http/httptest"
 "os"
 "path/filepath"
 "strings"
 "testing"
)

func TestMKVCacheKeyIsStableAndPathBound(t *testing.T){
 hash:=strings.Repeat("a",40)
 a:=mkvKey(hash,"movie.mkv")
 if len(a)!=64 || a!=mkvKey(hash,"movie.mkv"){t.Fatal("cache key must be deterministic")}
 if a==mkvKey(hash,"other.mkv")||a==mkvKey(strings.Repeat("b",40),"movie.mkv"){
  t.Fatal("cache keys must isolate infohashes and file paths")
 }
 if strings.ContainsAny(a,"/\\."){t.Fatal("cache key must not contain path separators")}
}
func TestMKVLanguageNormalization(t *testing.T){
 cases:=map[string]string{"eng":"en","dan":"da","fin":"fi","swe":"sv","fre":"fr","nor":"no","jpn":"ja","en":"en","es":"es","":"und","<script>":"und"}
 for input,want:=range cases{
  if got:=mkvLanguage(input);got!=want{t.Errorf("language %q: got %q, want %q",input,got,want)}
 }
}
func TestMKVCacheManifestMustHaveVideoAndValidTracks(t *testing.T){
 dir:=t.TempDir()
 key:=mkvKey(strings.Repeat("a",40),"movie.mkv")
 if _,ok:=cachedMKV(dir,key);ok{t.Fatal("no cached movie")}
 if err:=os.WriteFile(filepath.Join(dir,key+".mp4"),[]byte("mp4"),0600);err!=nil{t.Fatal(err)}
 if _,ok:=cachedMKV(dir,key);ok{t.Fatal("without manifest a video must not be trusted as ready")}
 tracks:=[]mkvTrack{{Index:2,Language:"en",Label:"English"}}
 raw,err:=json.Marshal(tracks)
 if err!=nil{t.Fatal(err)}
 if err:=os.WriteFile(filepath.Join(dir,key+".json"),raw,0600);err!=nil{t.Fatal(err)}
 found,ok:=cachedMKV(dir,key)
 if !ok||len(found)!=1||found[0].Language!="en"{t.Fatalf("invalid ready manifest: %+v",found)}
}
func TestMKVStatusOnlyBuildsOwnedVideoAndCaptionLinks(t *testing.T){
 a:=&application{dataDir:t.TempDir(),playback:newMKVManager()}
 hash:=strings.Repeat("d",40)
 path:="Film (2026)/Film & Subs.mkv"
 dir,err:=a.mkvCacheDir()
 if err!=nil{t.Fatal(err)}
 key:=mkvKey(hash,path)
 if err:=os.WriteFile(filepath.Join(dir,key+".mp4"),[]byte("test"),0600);err!=nil{t.Fatal(err)}
 tracks:=[]mkvTrack{{Index:5,Language:"da",Label:"Danish"}}
 raw,_:=json.Marshal(tracks)
 if err:=os.WriteFile(filepath.Join(dir,key+".json"),raw,0600);err!=nil{t.Fatal(err)}
 status:=a.mkvStatusFor(hash,path)
 if status.State!="ready" || len(status.Subtitles)!=1 {t.Fatalf("unexpected playback status: %+v",status)}
 if !strings.HasPrefix(status.VideoURL,"/api/playback/"+hash+"/video?path="){t.Fatal("wrong video URL")}
 if !strings.Contains(status.Subtitles[0].URL,"index=5"){t.Fatal("subtitles must use recognized stream ID")}
}
func TestMKVPrepareDeniesUnverifiedAndUnsafeSources(t *testing.T){
 hash:=strings.Repeat("b",40)
 a:=&application{engine:&engine{entries:map[string]*entry{}},dataDir:t.TempDir(),playback:newMKVManager()}
 for _,path:=range []string{"../private.mkv","movie.mkv","movie.mp4","file.exe"}{
  request:=httptest.NewRequest(http.MethodPost,"/api/playback/"+hash+"/prepare",strings.NewReader(`{"path":"`+path+`"}`))
  request.Header.Set("Content-Type","application/json")
  request.SetPathValue("hash",hash)
  w:=httptest.NewRecorder()
  a.mkvPrepare(w,request)
  if w.Code<400{t.Errorf("unverified %q was accepted: %d",path,w.Code)}
 }
}
