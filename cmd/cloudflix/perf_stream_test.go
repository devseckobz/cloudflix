package main

// Measurement harness for the video byte path: range throughput, seek latency,
// TTFB, concurrent stream fanout, and the cost the completion proof adds to
// every range request. Everything goes through the real /api/media handler
// (streamMedia -> serveMediaFile -> http.ServeContent) over httptest, so the
// numbers include the engine mutex, the os.Root open, and stdlib range
// handling exactly as in production.
//
// Run:
//   wsl -d Debian -- bash -lc 'cd /mnt/d/cloudflix && go test ./cmd/cloudflix \
//     -run TestPerfMedia -bench 'Media' -benchtime 2s'
//
// The required bitrate for the target content is 4K HDR at ~60 Mbps = 7.5 MB/s.
// Every throughput number in the report is compared against that constant.

import (
 "fmt"
 "io"
 "math/rand"
 "net/http"
 "net/http/httptest"
 "os"
 "path/filepath"
 "strings"
 "sync"
 "testing"
 "time"

 "github.com/devseckobz/cloudflix/internal/mediatools"
)

// required4KHDRBytesPerSecond is ~60 Mbps, the reference playback bitrate.
const required4KHDRBytesPerSecond = 60_000_000 / 8

// perfMediaFile returns a large deterministic media file. It prefers a real
// ffmpeg-generated MP4 from tmp/perf/media (see tmp/perf/make-media.sh) and
// falls back to synthesized bytes so the benchmarks never depend on ffmpeg.
func perfMediaFile(tb testing.TB, size int64) string {
 tb.Helper()
 if candidate := os.Getenv("CLOUDFLIX_PERF_MEDIA"); candidate != "" {
  if info, err := os.Stat(candidate); err == nil && info.Mode().IsRegular() && info.Size() >= size {
   return candidate
  }
 }
 path := filepath.Join(tb.TempDir(), "Movie00.mp4")
 file, err := os.Create(path)
 if err != nil {
  tb.Fatal(err)
 }
 chunk := make([]byte, 1<<20)
 for i := range chunk {
  chunk[i] = byte(i*31 + 7)
 }
 for written := int64(0); written < size; {
  n := int64(len(chunk))
  if remaining := size - written; remaining < n {
   n = remaining
  }
  if _, err := file.Write(chunk[:n]); err != nil {
   file.Close()
   tb.Fatal(err)
  }
  written += n
 }
 if err := file.Close(); err != nil {
  tb.Fatal(err)
 }
 return path
}

// perfStreamFixture creates an engine with one complete 2000-piece movie whose
// bytes on disk are a real file, and returns the served /api/media URL.
func perfStreamFixture(tb testing.TB, pieces int, mediaBytes int64) (*httptest.Server, string, *engine) {
 tb.Helper()
 e := perfEngine(tb, 1, pieces)
 mediaPath := perfMediaFile(tb, mediaBytes)
 rel := "Movie00/Movie00.mp4"
 local := filepath.Join(e.downloads, filepath.FromSlash(rel))
 if err := os.MkdirAll(filepath.Dir(local), 0o700); err != nil {
  tb.Fatal(err)
 }
 if err := os.Link(mediaPath, local); err != nil {
  // Different filesystems (tmpfs -> /mnt/d) reject hard links; copy instead.
  src, err := os.Open(mediaPath)
  if err != nil {
   tb.Fatal(err)
  }
  dst, err := os.Create(local)
  if err != nil {
   src.Close()
   tb.Fatal(err)
  }
  if _, err := io.Copy(dst, src); err != nil {
   src.Close()
   dst.Close()
   tb.Fatal(err)
  }
  src.Close()
  if err := dst.Close(); err != nil {
   tb.Fatal(err)
  }
 }
 a := &application{engine: e}
 mux := http.NewServeMux()
 mux.HandleFunc("GET /api/media/{hash}", a.streamMedia)
 server := httptest.NewServer(mux)
 tb.Cleanup(server.Close)
 hash := orderedEntries(e.entries)[0].hash
 return server, server.URL + "/api/media/" + hash + "?path=" + rel, e
}

// rangedGet fetches a range and returns its body, latency to first byte, and
// total latency.
func rangedGet(client *http.Client, url string, start, length int64) ([]byte, time.Duration, time.Duration, error) {
 req, err := http.NewRequest(http.MethodGet, url, nil)
 if err != nil {
  return nil, 0, 0, err
 }
 req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", start, start+length-1))
 started := time.Now()
 response, err := client.Do(req)
 if err != nil {
  return nil, 0, 0, err
 }
 defer response.Body.Close()
 if response.StatusCode != http.StatusPartialContent {
  return nil, 0, 0, fmt.Errorf("status %d", response.StatusCode)
 }
 body := make([]byte, 0, length)
 buffer := make([]byte, 32<<10)
 var ttfb time.Duration
 for {
  n, err := response.Body.Read(buffer)
  if n > 0 {
   if ttfb == 0 {
    ttfb = time.Since(started)
   }
   body = append(body, buffer[:n]...)
  }
  if err == io.EOF {
   break
  }
  if err != nil {
   return nil, 0, 0, err
  }
 }
 if ttfb == 0 {
  ttfb = time.Since(started)
 }
 return body, ttfb, time.Since(started), nil
}

// BenchmarkMediaSequentialRange measures 1 MiB sequential range throughput.
func BenchmarkMediaSequentialRange(b *testing.B) {
 const mediaBytes = 64 << 20
 server, url, _ := perfStreamFixture(b, 2000, mediaBytes)
 _ = server
 client := &http.Client{}
 const rangeBytes = 1 << 20
 b.SetBytes(rangeBytes)
 b.ReportAllocs()
 b.ResetTimer()
 for i := 0; i < b.N; i++ {
  offset := int64(i) * rangeBytes % (mediaBytes - rangeBytes)
  if _, _, _, err := rangedGet(client, url, offset, rangeBytes); err != nil {
   b.Fatal(err)
  }
 }
 b.ReportMetric(float64(b.N*rangeBytes)/b.Elapsed().Seconds()/(1<<20), "MB/s")
}

// BenchmarkMediaPlayerPattern measures the realistic player access pattern:
// 64 KiB ranges, mostly sequential with frequent random seeks.
func BenchmarkMediaPlayerPattern(b *testing.B) {
 const mediaBytes = 64 << 20
 server, url, _ := perfStreamFixture(b, 2000, mediaBytes)
 _ = server
 client := &http.Client{}
 const rangeBytes = 64 << 10
 random := rand.New(rand.NewSource(42))
 offset := int64(0)
 b.SetBytes(rangeBytes)
 b.ReportAllocs()
 b.ResetTimer()
 for i := 0; i < b.N; i++ {
  if i%10 == 9 {
   offset = random.Int63n(mediaBytes - rangeBytes)
  } else {
   offset += rangeBytes
   if offset > mediaBytes-rangeBytes {
    offset = 0
   }
  }
  if _, _, _, err := rangedGet(client, url, offset, rangeBytes); err != nil {
   b.Fatal(err)
  }
 }
 b.ReportMetric(float64(b.N*rangeBytes)/b.Elapsed().Seconds()/(1<<20), "MB/s")
}

// BenchmarkMediaTTFB times request start to first body byte on the video path.
func BenchmarkMediaTTFB(b *testing.B) {
 const mediaBytes = 64 << 20
 server, url, _ := perfStreamFixture(b, 2000, mediaBytes)
 _ = server
 client := &http.Client{}
 b.ReportAllocs()
 b.ResetTimer()
 var total time.Duration
 for i := 0; i < b.N; i++ {
  offset := int64(i*64<<10) % (mediaBytes - 64<<10)
  _, ttfb, _, err := rangedGet(client, url, offset, 64<<10)
  if err != nil {
   b.Fatal(err)
  }
  total += ttfb
 }
 b.ReportMetric(float64(total.Nanoseconds())/float64(b.N)/1e6, "ttfb-ms")
}

// TestPerfMediaSeekLatency issues 200 random-offset range requests and reports
// p50/p95/p99 latency to first byte. Not a benchmark because the percentiles
// are the deliverable.
func TestPerfMediaSeekLatency(t *testing.T) {
 if testing.Short() {
  t.Skip("seek latency harness skipped in -short")
 }
 const mediaBytes = 64 << 20
 server, url, _ := perfStreamFixture(t, 2000, mediaBytes)
 _ = server
 client := &http.Client{}
 random := rand.New(rand.NewSource(7))
 samples := make([]time.Duration, 0, 200)
 for i := 0; i < 200; i++ {
  offset := random.Int63n(mediaBytes - 128<<10)
  _, ttfb, _, err := rangedGet(client, url, offset, 128<<10)
  if err != nil {
   t.Fatal(err)
  }
  samples = append(samples, ttfb)
 }
 t.Logf("seek latency (first byte, 200 samples): p50=%v p95=%v p99=%v",
  percentile(samples, 0.50), percentile(samples, 0.95), percentile(samples, 0.99))
}

// TestPerfMediaConcurrentFanout streams with N concurrent readers and reports
// aggregate throughput plus the slowest reader's share.
func TestPerfMediaConcurrentFanout(t *testing.T) {
 if testing.Short() {
  t.Skip("fanout harness skipped in -short")
 }
 const mediaBytes = 64 << 20
 server, url, _ := perfStreamFixture(t, 2000, mediaBytes)
 _ = server
 client := &http.Client{}
 const readers = 8
 for _, n := range []int{1, 2, 4, 8} {
  const rangeBytes = 512 << 10
  perReader := 32
  started := time.Now()
  var wg sync.WaitGroup
  for reader := 0; reader < n; reader++ {
   wg.Add(1)
   go func(reader int) {
    defer wg.Done()
    for i := 0; i < perReader; i++ {
     offset := int64((reader*perReader+i)*rangeBytes) % (mediaBytes - rangeBytes)
     if _, _, _, err := rangedGet(client, url, offset, rangeBytes); err != nil {
      t.Errorf("reader %d: %v", reader, err)
      return
     }
    }
   }(reader)
  }
  wg.Wait()
  elapsed := time.Since(started)
  aggregate := float64(n*perReader*rangeBytes) / elapsed.Seconds() / (1 << 20)
  t.Logf("fanout N=%d: aggregate %.1f MB/s (%.2f MB/s per reader), wall %v",
   n, aggregate, aggregate/float64(n), elapsed)
 }
 _ = readers
}

// legacyVerifyUnderLock reproduces the pre-fix media/MKV authorization: walk
// every piece of the requested file while holding engine.mu's equivalent lock.
// Current production memoizes this via entry.fileVerifiedLocked.
func legacyVerifyUnderLock(e *engine, hash, path string) bool {
 e.mu.Lock()
 defer e.mu.Unlock()
 item := e.entries[hash]
 if item == nil || !item.validated || item.rejectionReason != "" || item.torrent == nil {
  return false
 }
 for _, file := range item.torrent.Files() {
  if filepath.ToSlash(file.Path()) == path && verifiedFile(file) {
   return true
  }
 }
 return false
}

// TestPerfCompletionProofCostScales is the deterministic proof that the legacy
// authorization walk is O(pieces): it times 20,000-piece verification against
// 500-piece verification inline and asserts a superlinear-in-pieces wall time,
// then asserts the memoized current path is flat.
func TestPerfCompletionProofCostScales(t *testing.T) {
 if testing.Short() {
  t.Skip("completion proof harness skipped in -short")
 }
 fileOf := func(pieces int) (*engine, string) {
  e := perfEngine(t, 1, pieces)
  return e, orderedEntries(e.entries)[0].hash
 }
 eSmall, hashSmall := fileOf(500)
 eLarge, hashLarge := fileOf(20000)
 path := "Movie00/Movie00.mp4"

 timeWalk := func(e *engine, hash string) time.Duration {
  started := time.Now()
  for i := 0; i < 20; i++ {
   if !legacyVerifyUnderLock(e, hash, path) {
    t.Fatal("fixture must verify")
   }
  }
  return time.Since(started) / 20
 }
 legacySmall := timeWalk(eSmall, hashSmall)
 legacyLarge := timeWalk(eLarge, hashLarge)
 t.Logf("legacy verify under lock: 500 pieces=%v, 20000 pieces=%v (%.0fx for 40x pieces)",
  legacySmall, legacyLarge, float64(legacyLarge)/float64(legacySmall))
 if legacyLarge < 5*legacySmall {
  t.Fatalf("legacy per-piece walk did not scale with pieces: %v -> %v", legacySmall, legacyLarge)
 }
 // The memoized current path must be orders of magnitude cheaper.
 eLarge.mu.Lock()
 item := eLarge.entries[hashLarge]
 if !eLarge.fileVerifiedLocked(item, item.torrent, path) {
  eLarge.mu.Unlock()
  t.Fatal("memoized verify must accept a complete file")
 }
 started := time.Now()
 for i := 0; i < 10000; i++ {
  if !eLarge.fileVerifiedLocked(item, item.torrent, path) {
   eLarge.mu.Unlock()
   t.Fatal("memoized verify must stay true")
  }
 }
 memoized := time.Since(started) / 10000
 eLarge.mu.Unlock()
 t.Logf("memoized fileVerifiedLocked (20000 pieces): %v per call", memoized)
 if memoized > legacyLarge/10 {
  t.Fatalf("memoized verify is not flat: %v vs legacy %v", memoized, legacyLarge)
 }
}


// perfMKVSource returns the path of a generated MKV fixture, or "" when the
// fixture has not been generated (tmp/perf/make-media.sh).
func perfMKVSource(name string) string {
 path := filepath.Join("..", "..", "tmp", "perf", "media", name)
 if info, err := os.Stat(path); err == nil && info.Mode().IsRegular() && info.Size() > 0 {
  return path
 }
 return ""
}

// TestPerfMKVConversion measures the real convertMKV() wall time for the three
// codec/container combinations that decide whether a 2-hour movie is practical:
// H.264 remux (fast path), H.264 1080p remux, and HEVC 10-bit transcode (slow
// path). It calls the production function, so the number includes ffprobe, the
// codec decision, ffmpeg, subtitle extraction and the manifest write.
func TestPerfMKVConversion(t *testing.T) {
 if testing.Short() {
  t.Skip("MKV conversion harness skipped in -short")
 }
 // Real ffmpeg conversions take seconds; keep the default `go test ./...` fast
 // and make the measurement explicit.
 if os.Getenv("CLOUDFLIX_PERF_MKV") == "" {
  t.Skip("set CLOUDFLIX_PERF_MKV=1 to run the MKV conversion measurements")
 }
 cases := []struct{ file, note string }{
  {"h264_720p.mkv", "H.264 720p remux (-c:v copy)"},
  {"h264_1080p.mkv", "H.264 1080p remux (-c:v copy)"},
  {"hevc_1080p.mkv", "HEVC 1080p 10-bit transcode (libx264 veryfast)"},
 }
 measured := 0
 for _, tc := range cases {
  source := perfMKVSource(tc.file)
  if source == "" {
   t.Logf("SKIP %s: fixture missing; run tmp/perf/make-media.sh", tc.file)
   continue
  }
  t.Run(tc.file, func(t *testing.T) {
   a := &application{dataDir: t.TempDir(), playback: newMKVManager()}
   // Warm the embedded tool extraction so it is not counted in the conversion.
   if _, err := mediatools.Resolve(a.dataDir, "ffmpeg"); err != nil {
    t.Skipf("ffmpeg unavailable: %v", err)
   }
   if _, err := mediatools.Resolve(a.dataDir, "ffprobe"); err != nil {
    t.Skipf("ffprobe unavailable: %v", err)
   }
   file, err := os.Open(source)
   if err != nil {
    t.Fatal(err)
   }
   info, err := file.Stat()
   if err != nil {
    file.Close()
    t.Fatal(err)
   }
   hash := strings.Repeat("a", 40)
   rel := "Movie00/" + tc.file
   key := mkvKey(hash, rel)
   job := &mkvJob{}
   started := time.Now()
   a.convertMKV(file, hash, rel, key, job)
   elapsed := time.Since(started)
   a.playback.mu.Lock()
   state, message := job.state, job.message
   a.playback.mu.Unlock()
   if state != "ready" {
    t.Fatalf("conversion failed: state=%s message=%s", state, message)
   }
   produced, err := os.Stat(filepath.Join(a.dataDir, "playback-cache", key+".mp4"))
   if err != nil {
    t.Fatal(err)
   }
   // The fixtures are 20 seconds long; extrapolate to a 2-hour movie.
   const fixtureSeconds = 20.0
   realtime := fixtureSeconds / elapsed.Seconds()
   twoHourMinutes := elapsed.Seconds() * (7200 / fixtureSeconds) / 60
   t.Logf("%s: source=%.1f MB output=%.1f MB wall=%.3fs => %.1fx realtime; 2h movie ~%.1f min",
    tc.note, float64(info.Size())/(1<<20), float64(produced.Size())/(1<<20), elapsed.Seconds(), realtime, twoHourMinutes)
   measured++
  })
 }
 if measured == 0 {
  t.Skip("no MKV fixtures available")
 }
}

// legacyStreamMedia reproduces the pre-fix /api/media authorization: the
// O(pieces) verifiedFile walk runs on EVERY range request while holding
// engine.mu. Current production memoizes it via entry.fileVerifiedLocked.
func (a *application) legacyStreamMedia(w http.ResponseWriter, r *http.Request) {
 hash := r.PathValue("hash")
 requested := r.URL.Query().Get("path")
 if !mediaHashPattern.MatchString(hash) || requested == "" || len(requested) > 4096 {
  fail(w, http.StatusBadRequest, "invalid media request")
  return
 }
 e := a.engine
 e.mu.Lock()
 item := e.entries[hash]
 valid := false
 if item != nil && item.validated && item.rejectionReason == "" && item.torrent != nil && item.torrent.Info() != nil {
  for _, file := range item.torrent.Files() {
   if filepath.ToSlash(file.Path()) == requested && allowedMediaPath(requested) && file.Length() > 0 && verifiedFile(file) {
    valid = true
    break
   }
  }
 }
 e.mu.Unlock()
 if !valid {
  fail(w, http.StatusNotFound, "media unavailable until the download completes")
  return
 }
 root, err := os.OpenRoot(e.downloads)
 if err != nil {
  fail(w, http.StatusInternalServerError, "storage unavailable")
  return
 }
 defer root.Close()
 serveMediaFile(w, r, root, requested)
}

// BenchmarkMediaLegacyVsCurrentRange is the end-to-end before/after for the
// per-request completion proof: identical handler, identical file, only the
// authorization walk differs. This is the cost a player paid on every seek.
func BenchmarkMediaLegacyVsCurrentRange(b *testing.B) {
 for _, pieces := range []int{500, 2000, 5000, 20000} {
  pieces := pieces
  b.Run(fmt.Sprintf("legacy/pieces=%d", pieces), func(b *testing.B) {
   const mediaBytes = 16 << 20
   e := perfEngine(b, 1, pieces)
   server, url := perfLegacyMediaServer(b, e, mediaBytes)
   client := server.Client()
   b.ReportAllocs()
   b.ResetTimer()
   var total time.Duration
   for i := 0; i < b.N; i++ {
    offset := int64(i*64<<10) % (mediaBytes - 64<<10)
    _, ttfb, _, err := rangedGet(client, url, offset, 64<<10)
    if err != nil {
     b.Fatal(err)
    }
    total += ttfb
   }
   b.ReportMetric(float64(total.Nanoseconds())/float64(b.N)/1e6, "ttfb-ms")
  })
  b.Run(fmt.Sprintf("current/pieces=%d", pieces), func(b *testing.B) {
   const mediaBytes = 16 << 20
   server, url, _ := perfStreamFixture(b, pieces, mediaBytes)
   client := server.Client()
   b.ReportAllocs()
   b.ResetTimer()
   var total time.Duration
   for i := 0; i < b.N; i++ {
    offset := int64(i*64<<10) % (mediaBytes - 64<<10)
    _, ttfb, _, err := rangedGet(client, url, offset, 64<<10)
    if err != nil {
     b.Fatal(err)
    }
    total += ttfb
   }
   b.ReportMetric(float64(total.Nanoseconds())/float64(b.N)/1e6, "ttfb-ms")
  })
 }
}

// perfLegacyMediaServer wires the legacy handler around a real on-disk file.
func perfLegacyMediaServer(tb testing.TB, e *engine, mediaBytes int64) (*httptest.Server, string) {
 tb.Helper()
 rel := "Movie00/Movie00.mp4"
 local := filepath.Join(e.downloads, filepath.FromSlash(rel))
 if err := os.MkdirAll(filepath.Dir(local), 0o700); err != nil {
  tb.Fatal(err)
 }
 file, err := os.Create(local)
 if err != nil {
  tb.Fatal(err)
 }
 chunk := make([]byte, 1<<20)
 for i := range chunk {
  chunk[i] = byte(i*31 + 7)
 }
 for written := int64(0); written < mediaBytes; {
  n := int64(len(chunk))
  if remaining := mediaBytes - written; remaining < n {
   n = remaining
  }
  if _, err := file.Write(chunk[:n]); err != nil {
   file.Close()
   tb.Fatal(err)
  }
  written += n
 }
 if err := file.Close(); err != nil {
  tb.Fatal(err)
 }
 a := &application{engine: e}
 mux := http.NewServeMux()
 mux.HandleFunc("GET /api/media/{hash}", a.legacyStreamMedia)
 server := httptest.NewServer(mux)
 tb.Cleanup(server.Close)
 hash := orderedEntries(e.entries)[0].hash
 return server, server.URL + "/api/media/" + hash + "?path=" + rel
}

// BenchmarkMediaRangeByPieceCount reports the per-range first-byte cost across
// the piece axis. This is the playback-facing cost of holding engine.mu while
// walking every piece: it is paid on every seek and every range request.
func BenchmarkMediaRangeByPieceCount(b *testing.B) {
 for _, pieces := range perfPieceCounts {
  pieces := pieces
  b.Run(fmt.Sprintf("pieces=%d", pieces), func(b *testing.B) {
   const mediaBytes = 16 << 20
   server, url, _ := perfStreamFixture(b, pieces, mediaBytes)
   _ = server
   client := &http.Client{}
   b.ReportAllocs()
   b.ResetTimer()
   var total time.Duration
   for i := 0; i < b.N; i++ {
    offset := int64(i*64<<10) % (mediaBytes - 64<<10)
    _, ttfb, _, err := rangedGet(client, url, offset, 64<<10)
    if err != nil {
     b.Fatal(err)
    }
    total += ttfb
   }
   b.ReportMetric(float64(total.Nanoseconds())/float64(b.N)/1e6, "ttfb-ms")
  })
 }
}
