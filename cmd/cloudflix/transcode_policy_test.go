package main

// The policy decides, per host and per movie, how much of the machine one
// conversion may take. These tests cover the host sizes a self-hoster actually
// runs: a small VPS where a 4K encode would be fatal, a 2-core box that must
// keep its measured two threads, and a large box that was left half idle.

import (
 "strings"
 "testing"
)

func gib(n uint64)uint64{return n << 30}

func profile(cores int,total,available uint64)hostProfile{
 return hostProfile{Cores:cores,TotalBytes:total,AvailableBytes:available}
}

func TestPlanTranscodeRefuses4KOnSmallVPS(t *testing.T){
 // 2 cores, 1 GB total, 700 MB free: a 4K HEVC encode peaked at 1.56 GB.
 plan:=planTranscode(profile(2,gib(1),700<<20),2160,true)
 if plan.Refuse==""{t.Fatal("a 1 GB VPS must refuse a 4K conversion instead of being OOM-killed")}
 if !strings.Contains(plan.Refuse,"memory"){t.Fatalf("refusal must explain itself: %q",plan.Refuse)}
}

func TestPlanTranscodeAllows1080pOnSmallVPS(t *testing.T){
 plan:=planTranscode(profile(2,gib(1),700<<20),1080,true)
 if plan.Refuse!=""{t.Fatalf("1080p should fit in 700 MB: %s",plan.Refuse)}
 if plan.Threads<1{t.Fatalf("expected at least one thread, got %d",plan.Threads)}
 if plan.MemoryBytes>plan.Budget{t.Fatalf("estimate %d exceeds budget %d",plan.MemoryBytes,plan.Budget)}
}

// The 2-core VPS this project targets must keep the two threads that measured a
// third faster than one, with streaming unaffected.
func TestPlanTranscodeKeepsTwoThreadsOnTargetVPS(t *testing.T){
 plan:=planTranscode(profile(2,gib(4),3<<30),1080,true)
 if plan.Threads!=2{t.Fatalf("2-core/4GB host gave %d threads, want the measured 2",plan.Threads)}
}

// A 2 GB VPS cannot host a 4K encode (1.6 GB estimated) once the reserve is
// kept, but a 4 GB one can.
func TestPlanTranscode4KBoundaryAcrossVPSsizes(t *testing.T){
 if plan:=planTranscode(profile(2,gib(2),1500<<20),2160,true);plan.Refuse==""{
  t.Fatalf("2 GB VPS must refuse 4K; budget was %d MB",plan.Budget>>20)
 }
 if plan:=planTranscode(profile(2,gib(4),3<<30),2160,true);plan.Refuse!=""{
  t.Fatalf("4 GB VPS should convert 4K: %s",plan.Refuse)
 }
}

func TestPlanTranscodeUsesMoreThreadsOnBigHost(t *testing.T){
 big:=planTranscode(profile(32,gib(64),gib(48)),1080,true)
 small:=planTranscode(profile(2,gib(4),gib(2)),1080,true)
 if big.Threads<=small.Threads{
  t.Fatalf("a 32-core host should use more threads than a 2-core host: %d vs %d",big.Threads,small.Threads)
 }
 if big.Threads>maxTranscodeThreads{t.Fatalf("threads=%d exceeds the cap",big.Threads)}
}

func TestPlanTranscodeCapsThreadsWhenMemoryIsTight(t *testing.T){
 // Many cores but a 2 GB box: the memory budget must pull threads down from
 // what the core count alone would suggest.
 tight:=planTranscode(profile(16,gib(2),900<<20),1080,true)
 if tight.Refuse!=""{t.Fatalf("900 MB free should still host a 1080p encode: %s",tight.Refuse)}
 if tight.Threads>=8{
  t.Fatalf("16 cores but 900 MB free gave %d threads; memory must cap it",tight.Threads)
 }
 if tight.MemoryBytes>tight.Budget{
  t.Fatalf("estimate %d exceeds budget %d",tight.MemoryBytes,tight.Budget)
 }
}

// A host that is marginal for the movie must refuse rather than start an
// encode the OOM killer will end halfway through.
func TestPlanTranscodeRefusesWhenBudgetIsMarginal(t *testing.T){
 // 444 MB budget (700 free minus a 256 MB reserve) against a 464 MB peak.
 plan:=planTranscode(profile(16,gib(2),700<<20),1080,true)
 if plan.Refuse==""{t.Fatalf("marginal budget must refuse; budget was %d MB",plan.Budget>>20)}
 if !strings.Contains(plan.Refuse,"1080p"){t.Fatalf("refusal should name the resolution: %q",plan.Refuse)}
}

func TestPlanTranscodeNeverOversubscribesCores(t *testing.T){
 for _,cores:=range []int{1,2,3,4,8,16,32,64}{
  plan:=planTranscode(profile(cores,gib(64),gib(48)),1080,true)
  if plan.Threads>cores{t.Fatalf("cores=%d gave %d threads",cores,plan.Threads)}
  if plan.Threads<1{t.Fatalf("cores=%d gave no threads",cores)}
 }
}

func TestPlanTranscodeScalesWithHostSize(t *testing.T){
 if plan:=planTranscode(profile(1,gib(2),gib(1)),1080,true);plan.Threads!=1{
  t.Fatalf("1 core gave %d threads",plan.Threads)
 }
 if plan:=planTranscode(profile(8,gib(16),gib(12)),1080,true);plan.Threads!=4{
  t.Fatalf("8 cores gave %d threads, want 4",plan.Threads)
 }
 if plan:=planTranscode(profile(16,gib(64),gib(48)),1080,true);plan.Threads!=8{
  t.Fatalf("16 cores gave %d threads, want 8",plan.Threads)
 }
}

func TestPlanTranscodeRefusesWhenMemoryIsUnknown(t *testing.T){
 plan:=planTranscode(profile(4,0,0),1080,true)
 if plan.Refuse==""{t.Fatal("unknown memory must refuse rather than guess")}
}

// The remux path allocates a fraction of a full encode, so a host too small for
// an encode can still remux.
func TestPlanTranscodeRemuxNeedsLessThanEncode(t *testing.T){
 host:=profile(2,gib(1),1000<<20)
 encodeProfile:=planTranscode(host,1080,true)
 remuxProfile:=planTranscode(host,1080,false)
 if encodeProfile.MemoryBytes==0 || remuxProfile.MemoryBytes==0{t.Fatal("no estimate produced")}
 if remuxProfile.MemoryBytes>=encodeProfile.MemoryBytes{
  t.Fatalf("remux estimate %d should be below encode estimate %d",remuxProfile.MemoryBytes,encodeProfile.MemoryBytes)
 }
}

func TestMemoryEstimateTracksMeasuredPeaks(t *testing.T){
 // Measured worst cases: 1080p 465 MB, 4K HEVC 1563 MB single-threaded.
 for _,c:=range []struct{height,threads int;floor uint64}{
  {720,1,186<<20},{1080,1,465<<20},{1080,2,515<<20},{1080,4,558<<20},
  {2160,1,1563<<20},{2160,2,1740<<20},
 }{
  if got:=memoryEstimate(c.height,c.threads);got<c.floor{
   t.Fatalf("estimate %d at %dp/%d threads is below the measured peak %d",got,c.height,c.threads,c.floor)
  }
 }
}

func TestMemoryReserveIsProportionalAndBounded(t *testing.T){
 if memoryReserve(gib(1))<memoryReserveMin{t.Fatal("reserve on a 1 GB box is below the minimum")}
 if memoryReserve(gib(64))>memoryReserveMax{t.Fatal("reserve on a 64 GB box exceeds the maximum")}
 if memoryReserve(gib(4))<memoryReserve(gib(1)){t.Fatal("reserve must not shrink as the host grows")}
}

func TestHostMemoryProfileReadsThisMachine(t *testing.T){
 p:=hostMemoryProfile()
 if p.Cores<1{t.Fatalf("cores=%d",p.Cores)}
 if p.TotalBytes==0{t.Fatal("total memory was not read")}
 if p.AvailableBytes==0{t.Fatal("available memory was not read")}
 if p.AvailableBytes>p.TotalBytes{t.Fatalf("available %d exceeds total %d",p.AvailableBytes,p.TotalBytes)}
}
