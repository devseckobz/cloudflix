package main

// Decision record for out-of-process caching (memcached/Redis).
//
// Cloudflix is one process on one VPS: the catalog lives in local SQLite, hot
// state is already memoised in memory, and the SSE broadcaster builds the
// snapshot once per tick and fans the same bytes to every client. A remote
// cache can only help if the work it removes costs more than reaching it.
//
// Measured on this machine (Linux, Ryzen 7 5700X3D):
//
//   localhost TCP GET     ~20.3 us  (tmp/cacheprobe, memcached ASCII shape)
//   localhost unix-socket ~13.1 us  (tmp/cacheprobe)
//   filesystemSpace()      ~1.8 us  <- 7x cheaper than the cheapest hop
//   json.Marshal(state)   ~32.7 us  <- but built once per 2s tick, not per request
//   scanLeftovers(10)    ~350.0 us  <- already polled at 10s, see below
//
// Conclusion: every candidate is either cheaper than the hop, or already
// amortised to one build per tick for all clients. A remote cache would add a
// round trip and a network dependency to remove work that costs less than the
// round trip. These benchmarks exist so a future change that makes one of these
// expensive shows up as a number instead of an opinion.

import (
 "encoding/json"
 "testing"
)

// BenchmarkStatfsCost is the disk-space read behind every /api/state response
// and every capacity check before a download starts.
func BenchmarkStatfsCost(b *testing.B){
 b.ReportAllocs()
 dir:=b.TempDir()
 b.ResetTimer()
 for i:=0;i<b.N;i++{
  if _,_,err:=filesystemSpace(dir);err!=nil{b.Fatal(err)}
 }
}

// BenchmarkStatePayloadMarshal builds the exact body livePayload produces:
// snapshot plus marshal. An SSE client costs this once per 2s tick no matter
// how many clients are connected, which is why sharing beats caching.
func BenchmarkStatePayloadMarshal(b *testing.B){
 e:=perfEngine(b,5,2000)
 b.ReportAllocs()
 b.ResetTimer()
 for i:=0;i<b.N;i++{
  payload,err:=json.Marshal(map[string]any{"movies":e.snapshot(),"storage":e.storage(),"version":version})
  if err!=nil{b.Fatal(err)}
  if len(payload)==0{b.Fatal("empty payload")}
 }
}
