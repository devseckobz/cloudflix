package main

import (
 "encoding/json"
 "os"
 "os/exec"
 "path/filepath"
 "strings"
 "testing"

 "github.com/devseckobz/cloudflix/internal/mediatools"
)

// Real user agents seen in the wild, and the layout each one must get.
var clientLayoutCases = map[string]bool{
 // Safari on macOS and iOS.
 "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.4 Safari/605.1.15":                   true,
 "Mozilla/5.0 (iPhone; CPU iPhone OS 17_4 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.4 Mobile/15E148 Safari/604.1":  true,
 "Mozilla/5.0 (iPad; CPU OS 16_6 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/16.6 Mobile/15E148 Safari/604.1":           true,
 // Every iOS browser is WebKit underneath, whatever its brand token says.
 "Mozilla/5.0 (iPhone; CPU iPhone OS 17_4 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) CriOS/131.0.6778.73 Mobile/15E148 Safari/604.1": true,
 "Mozilla/5.0 (iPhone; CPU iPhone OS 17_4 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) FxiOS/129.0 Mobile/15E148 Safari/605.1.15":       true,
 "Mozilla/5.0 (iPhone; CPU iPhone OS 17_4 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) EdgiOS/131.0 Mobile/15E148 Safari/604.1":        true,
 // Chromium family: fragmented playback is proven to work here.
 "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36":                        false,
 "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36 Edg/131.0.0.0":          false,
 "Mozilla/5.0 (Linux; Android 14; Pixel 8) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Mobile Safari/537.36":                  false,
 "Mozilla/5.0 (X11; Linux x86_64; rv:128.0) Gecko/20100101 Firefox/128.0":                                                                false,
 // The prewarm sentinel has no client and builds the default layout.
 prewarmClient: false,
 // Unrecognised or absent agents get the universally safe layout.
 "":                                                    true,
 "curl/8.5.0":                                          true,
 "SomeTV/1.2 (SmartTV; Linux)":                         true,
 "Mozilla/5.0 (compatible; MSIE 10.0; Windows NT 6.1)": true,
}

func TestClientNeedsFaststartLayout(t *testing.T){
 for agent,want:=range clientLayoutCases{
  if got:=clientNeedsFaststart(agent);got!=want{
   t.Errorf("user agent %q: got faststart=%v, want %v",agent,got,want)
  }
 }
}

func TestPlaybackKeySeparatesTheTwoLayouts(t *testing.T){
 hash:=strings.Repeat("c",40)
 path:="Film/Film.mkv"
 chrome:="Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36"
 safari:="Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.4 Safari/605.1.15"

 key:=mkvKey(hash,path)
 if got:=playbackKey(hash,path,chrome);got!=key{t.Fatalf("Chromium must keep the default key, got %q",got)}
 if got:=playbackKey(hash,path,prewarmClient);got!=key{t.Fatalf("prewarm must build the default layout, got %q",got)}
 wantFaststart:=key+faststartSuffix
 if got:=playbackKey(hash,path,safari);got!=wantFaststart{t.Fatalf("Safari must get its own key, got %q",got)}
 if playbackKey(hash,path,safari)==playbackKey(hash,path,chrome){
  t.Fatal("the two layouts must not share a cache entry")
 }

 if got:=playbackMovflags(key);got!="frag_keyframe+empty_moov+default_base_moof"{
  t.Fatalf("default layout must be fragmented, got movflags %q",got)
 }
 if got:=playbackMovflags(wantFaststart);got!="+faststart"{
  t.Fatalf("Safari layout must stay plain faststart, got movflags %q",got)
 }
}

// TestLegacyFaststartCacheIsAdoptedAndReconverted covers the upgrade path.
// Entries written before fragmentation support hold a faststart MP4 under the
// plain key. A WebKit viewer must keep it (moved into the faststart slot) and a
// Chromium viewer must be told the movie is not prepared, so the new layout is
// built instead of silently serving the old format forever.
func TestLegacyFaststartCacheIsAdoptedAndReconverted(t *testing.T){
 a:=&application{dataDir:t.TempDir(),playback:newMKVManager()}
 hash:=strings.Repeat("f",40)
 path:="Film/Legacy.mkv"
 dir,err:=a.mkvCacheDir()
 if err!=nil{t.Fatal(err)}
 key:=mkvKey(hash,path)
 writeCache(t,dir,key,false) // Legacy faststart file, plain key.

 chrome:="Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36"
 safari:="Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.4 Safari/605.1.15"

 // Chromium sees the legacy entry, adopts it for WebKit, and reports "not ready"
 // so its own fragmented layout gets built.
 if status:=a.mkvStatusFor(hash,path,chrome);status.State=="ready"{
  t.Fatal("Chromium must not be served the legacy faststart file as fragmented")
 }
 if _,err:=os.Stat(filepath.Join(dir,key+".mp4"));err==nil{
  t.Fatal("the legacy entry must leave the plain key")
 }
 if status:=a.mkvStatusFor(hash,path,safari);status.State!="ready"{
  t.Fatalf("Safari must keep playing the legacy faststart file, got %q",status.State)
 }
}

// TestFragmentedCacheIsNotServedToSafari is the mirror case: a prewarmed
// fragmented file must never be handed to WebKit.
func TestFragmentedCacheIsNotServedToSafari(t *testing.T){
 a:=&application{dataDir:t.TempDir(),playback:newMKVManager()}
 hash:=strings.Repeat("0",40)
 path:="Film/Modern.mkv"
 dir,err:=a.mkvCacheDir()
 if err!=nil{t.Fatal(err)}
 key:=mkvKey(hash,path)
 if err:=os.WriteFile(filepath.Join(dir,key+".mp4"),[]byte(fragmentedHeader()),0600);err!=nil{t.Fatal(err)}
 raw,_:=json.Marshal([]mkvTrack{})
 if err:=os.WriteFile(filepath.Join(dir,key+".json"),raw,0600);err!=nil{t.Fatal(err)}

 safari:="Mozilla/5.0 (iPad; CPU OS 16_6 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/16.6 Mobile/15E148 Safari/604.1"
 chrome:="Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36"
 if status:=a.mkvStatusFor(hash,path,safari);status.State=="ready"{
  t.Fatal("Safari must not be served the fragmented file")
 }
 if status:=a.mkvStatusFor(hash,path,chrome);status.State!="ready"{
  t.Fatalf("Chromium must be served the fragmented file, got %q",status.State)
 }
}

// writeCache writes a cache entry whose MP4 has the given container layout.
func writeCache(t *testing.T,dir,key string,fragmented bool){
 t.Helper()
 var body []byte
 if fragmented{body=fragmentedHeader()}else{body=faststartHeader()}
 if err:=os.WriteFile(filepath.Join(dir,key+".mp4"),body,0600);err!=nil{t.Fatal(err)}
 raw,err:=json.Marshal([]mkvTrack{{Index:3,Language:"en",Label:"English"}})
 if err!=nil{t.Fatal(err)}
 if err:=os.WriteFile(filepath.Join(dir,key+".json"),raw,0600);err!=nil{t.Fatal(err)}
}

// box builds a minimal top-level MP4 box: size, type, payload.
func box(kind string,payload int)[]byte{
 size:=8+payload
 return append([]byte{byte(size>>24),byte(size>>16),byte(size>>8),byte(size)},append([]byte(kind),make([]byte,payload)...)...)
}

// faststartHeader is ftyp, then the index (moov) before the media data (mdat).
func faststartHeader()[]byte{
 return append(append(box("ftyp",8),box("moov",32)...),box("mdat",8)...)
}

// fragmentedHeader is ftyp, then a movie fragment (moof) before any media data.
func fragmentedHeader()[]byte{
 return append(append(box("ftyp",8),box("moof",16)...),box("mdat",8)...)
}

func TestMP4IsFragmentedDetectsBothLayouts(t *testing.T){
 dir:=t.TempDir()
 for _,test:=range []struct{name string;body []byte;want bool}{
  {"faststart",faststartHeader(),false},
  {"fragmented",fragmentedHeader(),true},
 }{
  path:=filepath.Join(dir,test.name+".mp4")
  if err:=os.WriteFile(path,test.body,0600);err!=nil{t.Fatal(err)}
  file,err:=os.Open(path)
  if err!=nil{t.Fatal(err)}
  if got,ok:=mp4Layout(file);!ok||got!=test.want{
   t.Errorf("%s: got fragmented=%v ok=%v, want %v",test.name,got,ok,test.want)
  }
  file.Close()
 }
 // A file that is not MP4 must not be misread as "faststart".
 path:=filepath.Join(dir,"garbage.mp4")
 if err:=os.WriteFile(path,[]byte("not a video"),0600);err!=nil{t.Fatal(err)}
 file,err:=os.Open(path)
 if err!=nil{t.Fatal(err)}
 defer file.Close()
 if _,ok:=mp4Layout(file);ok{t.Fatal("unparseable file must not report a layout")}
}

// TestPlaybackStatusIsPerLayout mirrors the real handler: a Safari client must

// TestLegacyCacheMigrationOnRealBytes upgrades a real pre-existing faststart
// cache entry the way production will: the movie was converted by the old code
// (faststart, plain key). A WebKit viewer must keep playing it without a
// re-encode, and a Chromium viewer must cause a fragmented rebuild.
func TestLegacyCacheMigrationOnRealBytes(t *testing.T){
 dir:=t.TempDir()
 ffmpeg,err:=mediatools.Resolve(dir,"ffmpeg")
 if err!=nil||!strings.Contains(ffmpeg,"runtime-tools"){
  t.Skip("release payload not generated")
 }
 t.Setenv("PATH",t.TempDir())
 mkv:=filepath.Join(dir,"movie.mkv")
 if out,err:=exec.Command(ffmpeg,"-nostdin","-v","error","-f","lavfi","-i","color=size=32x32:rate=5",
  "-f","lavfi","-i","sine=frequency=440","-t","4","-c:v","libx264","-g","10","-pix_fmt","yuv420p","-c:a","aac",mkv).CombinedOutput();err!=nil{
  t.Fatalf("fixture: %v: %s",err,out)
 }
 a:=&application{dataDir:dir,playback:newMKVManager()}
 hash:=strings.Repeat("9",40)
 path:="Film/Real.mkv"
 dirOut,err:=a.mkvCacheDir()
 if err!=nil{t.Fatal(err)}
 key:=mkvKey(hash,path)

 // Old code: faststart output under the plain key.
 legacy:=filepath.Join(dirOut,key+".mp4")
 if out,err:=exec.Command(ffmpeg,"-nostdin","-v","error","-i",mkv,"-c","copy","-movflags","+faststart",legacy).CombinedOutput();err!=nil{
  t.Fatalf("legacy fixture: %v: %s",err,out)
 }
 raw,_:=json.Marshal([]mkvTrack{})
 if err:=os.WriteFile(filepath.Join(dirOut,key+".json"),raw,0600);err!=nil{t.Fatal(err)}

 safari:="Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.4 Safari/605.1.15"
 chrome:="Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36"

 // Safari first: the legacy entry is adopted into the faststart slot.
 if status:=a.mkvStatusFor(hash,path,safari);status.State!="ready"{
  t.Fatalf("Safari must keep playing the legacy entry, got %q",status.State)
 }
 adopted:=filepath.Join(dirOut,key+faststartSuffix+".mp4")
 if boxes:=readBoxes(t,adopted);boxes.moofs!=0{
  t.Fatalf("the adopted WebKit copy must stay faststart, found %d moof boxes",boxes.moofs)
 }
 if _,err:=os.Stat(legacy);err==nil{
  t.Fatal("the legacy file must have left the plain key")
 }

 // Chromium now sees no plain-key entry, so the fragmented build is queued.
 if status:=a.mkvStatusFor(hash,path,chrome);status.State=="ready"{
  t.Fatal("Chromium must not be served the faststart file as fragmented")
 }
}

// TestFFmpegProducesTheTwoLayouts runs the real bundled ffmpeg through
// convertMKV for both cache keys and checks the container the browser receives:
// the default output is fragmented (tiny moov, moof boxes) and the Safari
// output is a plain faststart file (moov before mdat).
func TestFFmpegProducesTheTwoLayouts(t *testing.T){
 dir:=t.TempDir()
 ffmpeg,err:=mediatools.Resolve(dir,"ffmpeg")
 if err!=nil||!strings.Contains(ffmpeg,"runtime-tools"){
  t.Skip("release payload not generated")
 }
 t.Setenv("PATH",t.TempDir())
 mkv:=filepath.Join(dir,"movie.mkv")
 args:=[]string{"-nostdin","-v","error","-f","lavfi","-i","color=size=32x32:rate=5",
  "-f","lavfi","-i","sine=frequency=440","-t","6","-c:v","libx264","-g","10","-pix_fmt","yuv420p","-c:a","aac",mkv}
 if out,err:=exec.Command(ffmpeg,args...).CombinedOutput();err!=nil{
  t.Fatalf("fixture: %v: %s",err,out)
 }
 a:=&application{dataDir:dir,playback:newMKVManager()}
 hash:=strings.Repeat("a",40)
 for _,key:=range []string{mkvKey(hash,"movie.mkv"),mkvKey(hash,"movie.mkv")+faststartSuffix}{
  source,err:=os.Open(mkv)
  if err!=nil{t.Fatal(err)}
  job:=&mkvJob{state:"queued"}
  a.playback.jobs[key]=job
  a.convertMKV(source,hash,"movie.mkv",key,job)
  if job.state!="ready"{t.Fatalf("%s: conversion failed: %s: %s",key,job.state,job.message)}
 }
 cache,err:=a.mkvCacheDir()
 if err!=nil{t.Fatal(err)}

 frag:=readBoxes(t,filepath.Join(cache,mkvKey(hash,"movie.mkv")+".mp4"))
 fast:=readBoxes(t,filepath.Join(cache,mkvKey(hash,"movie.mkv")+faststartSuffix+".mp4"))

 if frag.moofs==0||frag.moovSize>64<<10{
  t.Fatalf("default layout is not fragmented: moofs=%d moov=%d bytes",frag.moofs,frag.moovSize)
 }
 if fast.moofs!=0{
  t.Fatalf("Safari layout must not be fragmented: %d moof boxes",fast.moofs)
 }
 if fast.moovPos==0||fast.moovPos>fast.mdatPos&&fast.mdatPos!=0{
  t.Fatalf("Safari layout must put moov before mdat: moov@%d mdat@%d",fast.moovPos,fast.mdatPos)
 }
}

type mkvBoxes struct{
 moovSize int
 moovPos int64
 mdatPos int64
 moofs int
}

// readBoxes walks the top-level MP4 boxes of a file.
func readBoxes(t *testing.T,path string)mkvBoxes{
 t.Helper()
 file,err:=os.Open(path)
 if err!=nil{t.Fatal(err)}
 defer file.Close()
 stat,err:=file.Stat()
 if err!=nil{t.Fatal(err)}
 var out mkvBoxes
 var offset int64
 header:=make([]byte,16)
 for offset+8<=stat.Size(){
  if _,err:=file.ReadAt(header[:8],offset);err!=nil{t.Fatal(err)}
  size:=int64(header[0])<<24|int64(header[1])<<16|int64(header[2])<<8|int64(header[3])
  kind:=string(header[4:8])
  switch kind{
  case "moov":
   out.moovPos=offset
   out.moovSize=int(size)
  case "mdat":
   if out.mdatPos==0{out.mdatPos=offset}
  case "moof":
   out.moofs++
  }
  if size<8{
   t.Fatalf("malformed box %q at %d (size %d)",kind,offset,size)
  }
  offset+=size
 }
 return out
}
