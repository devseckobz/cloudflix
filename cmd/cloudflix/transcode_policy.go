package main

import (
 "fmt"
 "runtime"
)

// How much of the machine one conversion may take, chosen from the host's
// cores and memory rather than a fixed constant.
//
// Measured on this repo's fixtures (x264 veryfast, HEVC->H264), peak RSS:
//
//   threads=1   threads=2   threads=4
//   1080p   464 MB      515 MB      558 MB
//   2160p  1219 MB     1395 MB     1540 MB
//
// Three things follow. Memory scales with resolution, so a 4K job on a small
// VPS must be refused rather than started and OOM-killed halfway. Memory also
// grows with thread count, so threads must be charged against the budget. And
// the reserve kept for the rest of the process must be proportional: a flat
// 384 MB is a third of a 1 GB box, which refused conversions that would have
// fitted comfortably.
//
// Encode speed on the 20 s 1080p fixture, 2 pinned cores, threads 1 -> 2:
// 11.2 s -> 8.4 s, while real /api/media range throughput stayed at 255-256
// MB/s and TTFB at 0.22-0.23 ms. The encoder time-slices with the HTTP server,
// so reserving a core is not needed; not starving memory is.

const (
 // mib is one mebibyte, used for every budget below.
 mib uint64 = 1 << 20
 // memoryReserveFraction of the host is kept for the HTTP server, SQLite, the
 // torrent client and page cache, bounded so it is neither trivial on a huge
 // host nor ruinous on a small one.
 memoryReserveFraction = 8 // 1/8 of the machine
 memoryReserveMin = 128 * mib
 memoryReserveMax = 1024 * mib
 // minTranscodeBudget is the least memory worth starting a conversion with.
 minTranscodeBudget = 256 * mib
 // maxTranscodeThreads caps a single job even on very large hosts: past this
 // the encoder spends more time on sync than on pixels.
 maxTranscodeThreads = 16
)

// memoryReserve is how much of the host the transcoder must leave alone.
func memoryReserve(total uint64)uint64{
 if total==0{return memoryReserveMin}
 reserve:=total/memoryReserveFraction
 if reserve<memoryReserveMin{reserve=memoryReserveMin}
 if reserve>memoryReserveMax{reserve=memoryReserveMax}
 return reserve
}

// baseMemoryEstimate is the measured peak RSS for a single-threaded encode at
// the given height, rounded up to the next tier.
//
// 4K is set from the worst observed case, not the average one: 4K H.264 source
// peaked at 1219 MB but 4K HEVC at 1563 MB, because the decoder's reference
// frames are far larger. Sizing to the H.264 number would under-estimate every
// 4K HEVC movie, which is exactly the input a 4K MKV tends to be.
func baseMemoryEstimate(height int)uint64{
 switch{
 case height>1600: // 4K
  return 1600 * mib
 case height>900: // 1080p
  return 480 * mib
 case height>700: // 720p
  return 300 * mib
 default: // SD and below
  return 220 * mib
 }
}

// memoryPerExtraThread is the measured growth per additional encoder thread:
// 1080p goes 465 -> 515 -> 558 MB at 1/2/4 threads (+50, +43), while 4K goes
// 1219 -> 1395 -> 1540 MB (+176, +145). The per-thread figures are rounded up
// so the estimate stays above the measured peak rather than tracking it
// exactly; being 25 MB short at 4K is the difference between converting and
// being OOM-killed.
func memoryPerExtraThread(height int)uint64{
 switch{
 case height>1600:
  return 200 * mib
 case height>900:
  return 56 * mib
 default:
  return 24 * mib
 }
}

// memoryEstimate is the measured peak RSS for an encode with the given thread
// count at the given height.
func memoryEstimate(height,threads int)uint64{
 if threads<1{threads=1}
 return baseMemoryEstimate(height)+uint64(threads-1)*memoryPerExtraThread(height)
}

// transcodeDecision is what the policy concluded, with the reason kept so the
// job message can explain a refusal instead of failing opaquely.
type transcodeDecision struct {
 Threads int
 // MemoryBytes is the estimated peak for this job at the chosen thread count.
 MemoryBytes uint64
 // Budget is what the policy was willing to spend.
 Budget uint64
 // Refuse is set when the host cannot host the conversion safely.
 Refuse string
}

// planTranscode chooses threads for a job of the given source height.
func planTranscode(profile hostProfile,height int,permanent bool)transcodeDecision{
 cores:=profile.Cores
 if cores<=0{cores=runtime.NumCPU()}
 // A copy-only job (the remux path) allocates a fraction of a full encode:
 // no lookahead buffers and no reference frames are held.
 scale:=uint64(1)
 if !permanent{scale=4}
 estimateFor:=func(threads int)uint64{return memoryEstimate(height,threads)/scale}

 total:=profile.TotalBytes
 if total==0{total=profile.AvailableBytes}
 // Free memory is the real constraint; the ceiling is only a fallback for a
 // host that cannot report what is free (no /proc, an exotic runtime). Falling
 // back to the total when free memory is merely *small* would ignore the
 // constraint entirely, which is how a busy 2 GB box would start a 4K encode.
 availability:=profile.AvailableBytes
 if availability==0{availability=total}
 reserve:=memoryReserve(total)
 budget:=uint64(0)
 if availability>reserve{budget=availability-reserve}
 decision:=transcodeDecision{Budget:budget}
 if budget<minTranscodeBudget{
  decision.Refuse=fmt.Sprintf("The server has only %d MB of memory free, which is not enough to convert this movie safely. The original file was kept; free memory or add RAM and try again.",
   availability/mib)
  return decision
 }
 // Start from the core-count rule that was measured on real hardware, then
 // walk down only as far as the memory budget requires. Starting from cores/2
 // would give a 2-core box a single thread, which measured a third slower than
 // two threads with no streaming penalty (11.2 s vs 8.4 s); using the same
 // starting rule keeps that win and lets memory take it back only when it must.
 threads:=transcodeThreadsFor(cores)
 if threads>maxTranscodeThreads{threads=maxTranscodeThreads}
 for threads>1 && estimateFor(threads)>budget{threads--}
 estimate:=estimateFor(threads)
 decision.Threads=threads
 decision.MemoryBytes=estimate
 if estimate>budget{
  decision.Refuse=fmt.Sprintf("This %s movie needs about %d MB of memory to convert, but only %d MB can be spared. The original file was kept; add RAM to the server to convert it automatically.",
   resolutionLabel(height),estimate/mib,budget/mib)
  return decision
 }
 return decision
}

// resolutionLabel names a height for a human reading an error.
func resolutionLabel(height int)string{
 switch{
 case height>=2000:
  return "4K"
 case height>=1000:
  return "1080p"
 case height>=700:
  return "720p"
 default:
  return "standard-definition"
 }
}
