package main

// The policy refuses conversions on memory grounds, so its estimates must sit
// at or above what FFmpeg really allocates. Under-estimating is the dangerous
// direction: the job starts, the OOM killer ends it twenty minutes later, and
// the user is left with a half-converted file.
//
// The fixtures are synthesized at test time rather than shipped, so this runs
// wherever the media payload resolves - including CI. The source is a
// 'gradients' pattern encoded as HEVC: measured at 453 MB peak for 1080p versus
// 465 MB for real HEVC content, where a flat colour source reached only 269 MB
// and would let the test pass without exercising the footprint that matters.

import (
 "fmt"
 "os"
 "os/exec"
 "path/filepath"
 "strconv"
 "strings"
 "testing"
 "time"

 "github.com/devseckobz/cloudflix/internal/mediatools"
)

// peakRSSMB runs cmd and returns the peak resident set size in MiB. VmHWM is
// the kernel's own high-water mark, so the result does not depend on how often
// this samples.
func peakRSSMB(t *testing.T,cmd *exec.Cmd)uint64{
 t.Helper()
 if err:=cmd.Start();err!=nil{t.Fatal(err)}
 done:=make(chan error,1)
 go func(){done<-cmd.Wait()}()
 ticker:=time.NewTicker(50*time.Millisecond)
 defer ticker.Stop()
 var peak uint64
 for running:=true;running;{
  select{
  case err:=<-done:
   if err!=nil{t.Fatalf("encode failed: %v",err)}
   running=false
  case <-ticker.C:
   raw,err:=os.ReadFile(fmt.Sprintf("/proc/%d/status",cmd.Process.Pid))
   if err!=nil{continue}
   for _,line:=range strings.Split(string(raw),"\n"){
    if !strings.HasPrefix(line,"VmHWM:"){continue}
    fields:=strings.Fields(line)
    if len(fields)<2{continue}
    kb,err:=strconv.ParseUint(fields[1],10,64)
    if err==nil && kb/1024>peak{peak=kb/1024}
   }
  }
 }
 return peak
}

// syntheticSource needs a full-featured ffmpeg to synthesize. The bundled
// payload is built with --disable-everything and carries only the 'color'
// source, which understates memory, so a host without a system ffmpeg skips
// this test instead of asserting against a misleading fixture.
func syntheticSource(t *testing.T,dir string,width,height int)string{
 t.Helper()
 system,err:=exec.LookPath("ffmpeg")
 if err!=nil{t.Skip("no system ffmpeg to synthesize a representative fixture")}
 filter:=fmt.Sprintf("gradients=size=%dx%d:rate=24",width,height)
 probe:=exec.Command(system,"-nostdin","-hide_banner","-loglevel","error","-f","lavfi","-i",filter,"-t","1","-f","null","-")
 if out,err:=probe.CombinedOutput();err!=nil{
  t.Skipf("system ffmpeg cannot build the fixture: %v: %s",err,out)
 }
 output:=filepath.Join(dir,fmt.Sprintf("src-%dp.mkv",height))
 args:=[]string{"-nostdin","-hide_banner","-loglevel","error","-y","-f","lavfi","-i",filter,
  "-t","3","-c:v","libx265","-preset","ultrafast","-crf","30","-x265-params","log-level=error",output}
 if out,err:=exec.Command(system,args...).CombinedOutput();err!=nil{
  t.Fatalf("synthetic source: %v: %s",err,out)
 }
 return output
}

// TestMemoryEstimateCoversMeasuredPeak is the check that keeps the policy
// honest: it fails if any estimate is below the peak FFmpeg actually reached.
func TestMemoryEstimateCoversMeasuredPeak(t *testing.T){
 if testing.Short(){t.Skip("spawns real encodes")}
 dir:=t.TempDir()
 ffmpeg,err:=mediatools.Resolve(dir,"ffmpeg")
 if err!=nil{t.Skip("media tools unavailable on this host")}
 fixtures:=[]struct{width,height int}{{1280,720},{1920,1080}}
 // A 4K encode needs ~1.7 GB; only measure it where that much is free.
 if hostMemoryProfile().AvailableBytes>=6<<30{
  fixtures=append(fixtures,struct{width,height int}{3840,2160})
 }
 for _,fixture:=range fixtures{
  source:=syntheticSource(t,dir,fixture.width,fixture.height)
  for _,threads:=range []int{1,2}{
   output:=filepath.Join(dir,fmt.Sprintf("out-%d-%d.mp4",fixture.height,threads))
   cmd:=exec.Command(ffmpeg,"-nostdin","-hide_banner","-loglevel","error","-y",
    "-i",source,"-map","0:v:0","-sn","-dn","-c:v","libx264","-preset","veryfast",
    "-crf","24","-pix_fmt","yuv420p","-threads",strconv.Itoa(threads),"-an",
    "-movflags","+faststart","-fs","12884901888",output)
   measured:=peakRSSMB(t,cmd)
   estimate:=memoryEstimate(fixture.height,threads) >> 20
   t.Logf("%dp threads=%d measured=%d MB estimate=%d MB",fixture.height,threads,measured,estimate)
   if estimate<measured{
    t.Fatalf("%dp at %d threads: estimate %d MB is below measured peak %d MB; an encode could be OOM-killed",
     fixture.height,threads,estimate,measured)
   }
  }
 }
}
