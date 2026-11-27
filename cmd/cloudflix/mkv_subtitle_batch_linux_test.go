package main

// Subtitle extraction used to run one ffmpeg per track, re-scanning the whole
// container each time. These tests pin what the batched pass must preserve:
// every text track is extracted, and each output is real WebVTT.

import (
 "encoding/json"
 "os"
 "os/exec"
 "path/filepath"
 "strconv"
 "strings"
 "testing"

 "github.com/devseckobz/cloudflix/internal/mediatools"
)

// fixtureMultiSubtitleMKV builds a small MKV carrying three SubRip tracks with
// distinct languages, so language mapping is exercised too.
func fixtureMultiSubtitleMKV(t *testing.T,ffmpeg,dir string)string{
 t.Helper()
 subtitle:=filepath.Join(dir,"track.srt")
 if err:=os.WriteFile(subtitle,[]byte("1\n00:00:00,100 --> 00:00:01,500\nFixture subtitle line\n"),0600);err!=nil{t.Fatal(err)}
 output:=filepath.Join(dir,"multi.mkv")
 args:=[]string{"-y","-nostdin","-v","error",
  "-f","lavfi","-i","color=size=32x32:rate=5",
  "-i",subtitle,"-i",subtitle,"-i",subtitle,
  "-map","0:v","-map","1:s","-map","2:s","-map","3:s","-t","2","-c:v","libx264","-c:s","copy",
  "-metadata:s:s:0","language=eng","-metadata:s:s:1","language=fra","-metadata:s:s:2","language=swe",
  output}
 if out,err:=exec.Command(ffmpeg,args...).CombinedOutput();err!=nil{t.Fatalf("fixture: %v: %s",err,out)}
 return output
}

func TestBatchedSubtitleExtractionWritesEveryTrack(t *testing.T){
 dir:=t.TempDir()
 ffmpeg,err:=mediatools.Resolve(dir,"ffmpeg")
 if err!=nil{t.Skip("media tools unavailable on this host")}
 ffprobe,err:=mediatools.Resolve(dir,"ffprobe")
 if err!=nil{t.Skip("media tools unavailable on this host")}
 source:=fixtureMultiSubtitleMKV(t,ffmpeg,dir)
 file,err:=os.Open(source)
 if err!=nil{t.Fatal(err)}
 defer file.Close()

 probeBytes,err:=commandFromMKV(t.Context(),ffprobe,file,
  "-v","error","-show_entries","stream=index,codec_type,codec_name:stream_tags=language,title","-of","json","-i","/proc/self/fd/3")
 if err!=nil{t.Fatalf("probe: %v",err)}
 var parsed ffProbe
 if err:=json.Unmarshal(probeBytes,&parsed);err!=nil{t.Fatalf("probe json: %v",err)}

 targets:=make([]int,0)
 for _,stream:=range parsed.Streams{
  if stream.CodecType!="subtitle"{continue}
  switch strings.ToLower(stream.CodecName){
  case "subrip","ass","ssa","webvtt","mov_text","text":
   targets=append(targets,stream.Index)
  }
 }
 if len(targets)!=3{t.Fatalf("fixture should carry 3 text tracks, found %d",len(targets))}

 const key="batchfixture"
 batch:=[]string{"-nostdin","-hide_banner","-loglevel","error","-y","-i","/proc/self/fd/3"}
 outputs:=make([]string,len(targets))
 for i,index:=range targets{
  outputs[i]=filepath.Join(dir,key+"."+strconv.Itoa(index)+".vtt")
  batch=append(batch,"-map","0:"+strconv.Itoa(index),"-c:s","webvtt","-f","webvtt",outputs[i]+".part")
 }
 if _,err:=commandFromMKV(t.Context(),ffmpeg,file,batch...);err!=nil{t.Fatalf("batched extraction failed: %v",err)}

 for i,index:=range targets{
  if err:=finalizeSubtitle(outputs[i]+".part",outputs[i],false);err!=nil{
   t.Fatalf("track %d: %v",index,err)
  }
  body,err:=os.ReadFile(outputs[i])
  if err!=nil{t.Fatalf("track %d missing: %v",index,err)}
  if !strings.HasPrefix(string(body),"WEBVTT"){t.Fatalf("track %d is not WebVTT: %q",index,body[:min(len(body),40)])}
  if !strings.Contains(string(body),"Fixture subtitle line"){
   t.Fatalf("track %d lost its cue text: %q",index,body)
  }
  if strings.Contains(string(body),"00:00:00,100"){
   t.Fatalf("track %d kept SRT comma timestamps",index)
  }
 }
}
