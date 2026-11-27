package main

// transcodeThreads decides how much of the machine libx264 may take while the
// server keeps streaming. Two properties matter: it must never oversubscribe a
// small host, and it must not leave a larger one idle.

import (
 "runtime"
 "testing"
)

func TestTranscodeThreadsNeverOversubscribes(t *testing.T){
 cores:=runtime.NumCPU()
 threads:=transcodeThreads()
 if threads<1{t.Fatalf("cores=%d gave ffmpeg %d threads",cores,threads)}
 if threads>cores{t.Fatalf("cores=%d gave ffmpeg %d threads: oversubscribed",cores,threads)}
 if threads>8{t.Fatalf("cores=%d gave ffmpeg %d threads, above the 8-thread cap",cores,threads)}
}

// The allocation must be right at each host size, not just on this machine.
func TestTranscodeThreadsByHostSize(t *testing.T){
 cases:=[]struct{cores,want int}{
  {1,1},  // a single core cannot host two encoder threads
  {2,2},  // the targeted VPS: measured to leave streaming unaffected
  {3,2},
  {4,2},
  {8,4},
  {16,8},
  {32,8}, // capped
 }
 for _,c:=range cases{
  if got:=transcodeThreadsFor(c.cores);got!=c.want{
   t.Fatalf("cores=%d: got %d threads, want %d",c.cores,got,c.want)
  }
 }
}
