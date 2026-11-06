package main

import (
 "os"
 "os/exec"
 "path/filepath"
 "strings"
 "testing"

 "github.com/devseckobz/cloudflix/internal/mediatools"
)

func TestLibraryVerifiesCopiedVideoPackets(t *testing.T){
 dir:=t.TempDir()
 ffmpeg,err:=mediatools.Resolve(dir,"ffmpeg")
 if err!=nil || !strings.Contains(ffmpeg,"runtime-tools"){t.Skip("release payload not generated")}
 ffprobe,err:=mediatools.Resolve(dir,"ffprobe")
 if err!=nil{t.Fatal(err)}
 run:=func(args ...string){
  t.Helper()
  if out,err:=exec.Command(ffmpeg,append([]string{"-nostdin","-v","error"},args...)...).CombinedOutput();err!=nil{
   t.Fatalf("FFmpeg fixture: %v: %s",err,out)
  }
 }
 source:=filepath.Join(dir,"source.mkv")
 output:=filepath.Join(dir,"copy.mp4")
 changed:=filepath.Join(dir,"changed.mp4")
 run("-f","lavfi","-i","color=red:size=64x64:rate=5","-t","12","-c:v","libx264","-g","5",source)
 run("-i",source,"-c","copy","-movflags","+faststart",output)
 run("-f","lavfi","-i","color=blue:size=64x64:rate=5","-t","12","-c:v","libx264","-g","5","-movflags","+faststart",changed)
 open:=func(path string)*os.File{
  t.Helper()
  file,err:=os.Open(path)
  if err!=nil{t.Fatal(err)}
  t.Cleanup(func(){file.Close()})
  return file
 }
 original:=open(source)
 if !verifyCopiedLibraryVideo(t.Context(),ffprobe,original,open(output),12){t.Fatal("identical copied video missed the fast verification path")}
 if verifyCopiedLibraryVideo(t.Context(),ffprobe,original,open(changed),12){t.Fatal("changed packets passed the copy verification path")}
 if !verifyLibraryMP4(t.Context(),ffprobe,changed,"12",original,true){t.Fatal("decodable video did not pass fallback verification")}
 if verifyLibraryMP4(t.Context(),ffprobe,output,"24",original,true){t.Fatal("truncated-duration MP4 passed verification")}
}
