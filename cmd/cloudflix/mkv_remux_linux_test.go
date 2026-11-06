package main

import (
 "context"
 "os"
 "os/exec"
 "path/filepath"
 "testing"

 "github.com/devseckobz/cloudflix/internal/mediatools"
)

func TestRemuxCachedLayout(t *testing.T){
 dir:=t.TempDir()
 ffmpeg,err:=mediatools.Resolve(dir,"ffmpeg")
 if err!=nil{t.Skip("FFmpeg unavailable")}
 for _,test:=range []struct{source,target string; fragmented bool}{
  {"fragmented","fragmented"+faststartSuffix,true},
  {"unfragmented"+faststartSuffix,"unfragmented",false},
 }{
  t.Run(test.source,func(t *testing.T){
   sourceKey,targetKey:=test.source,test.target
   source:=filepath.Join(dir,sourceKey+".mp4")
   args:=[]string{"-nostdin","-v","error","-f","lavfi","-i","color=size=32x32:rate=5",
    "-f","lavfi","-i","sine=frequency=440","-t","3","-c:v","libx264","-g","5",
    "-pix_fmt","yuv420p","-c:a","aac","-movflags",playbackMovflags(sourceKey),source}
   if out,err:=exec.Command(ffmpeg,args...).CombinedOutput();err!=nil{t.Fatalf("fixture: %v: %s",err,out)}
   if err:=os.WriteFile(filepath.Join(dir,sourceKey+".json"),[]byte("[]"),0600);err!=nil{t.Fatal(err)}
   if fragmented,known:=cachedIsFragmented(dir,sourceKey);!known || fragmented!=test.fragmented{
    t.Fatalf("source detected as fragmented=%t known=%t",fragmented,known)
   }
   output:=filepath.Join(dir,targetKey+".mp4")
   if !remuxCachedLayout(context.Background(),ffmpeg,dir,targetKey,output){t.Fatal("remux failed")}
   boxes:=readBoxes(t,output)
   if (boxes.moofs>0)==test.fragmented || boxes.moovPos>=boxes.mdatPos{
    t.Fatalf("unexpected MP4 layout: %+v",boxes)
   }
  })
 }
}
