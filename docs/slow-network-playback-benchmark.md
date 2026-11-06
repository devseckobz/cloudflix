# Slow-network playback: root cause and fix

Question: how do we make downloaded MKV/MP4 play well on a slow connection?

Measured against **live production** (`https://m.kobz.xyz`, real library, 5
movies) with ego-browser Chromium and CDP network throttling, plus encode-side
tests using the shipped static `ffmpeg` 9.0.2.

---

## 1. The server is not the problem

Media responses are correct:

```
status 206, accept-ranges: bytes,
cache-control: private, max-age=0, must-revalidate
```

Range requests are served properly. Changing playback buffering settings
(`preload="metadata"` vs `"auto"`) made **no measurable difference** (0 s buffer
either way before play starts) — that was tested and ruled out.

Bitrate capping was also tested and is the wrong lever: capping does not change
the frame count, so it does not shrink the index (see below). It addresses
transfer time, not startup.

## 2. Root cause: the MP4 index is 4–5 MB

Production files are correctly `faststart` (`moov` before `mdat`), so the index
is at the front. But the index for a 2-hour movie is **large**:

| Movie | file | `moov` index | index/file |
|---|---|---|---|
| Resident Evil | 864 MB | **5.28 MB** | 0.61% |
| Evil Dead Burn | 1016 MB | **4.23 MB** | 0.42% |
| Return to Silent Hill | 972 MB | **4.09 MB** | 0.42% |

The browser must download the **entire** `moov` before it can decode or render
a single frame, because it holds the sample table for every frame. The index
grows with frame count (~432,000 frames for 2 h at 60 fps), not with bitrate.

### The cost model holds

Cold-start time-to-first-frame on a throttled link, measured on production:

| bandwidth | predicted (`moov` bytes ÷ bandwidth) | measured TTFF | error |
|---|---|---|---|
| 2 Mbps | 22.1 s | **18.1 s** | −18% |
| 800 kbps | 55.4 s | **48.3 s** | −13% |

Startup is dominated by fetching the index. That is the "buffers slowly on slow
networks" symptom.

## 3. The fix: fragmented MP4

Fragmenting keeps `moov` tiny and puts per-fragment sample tables in `moof`
boxes, so playback can begin after a few KB instead of megabytes.

Fixture: 2 h at 60 fps (**432,000 frames** — production frame counts), tiny
resolution so the file stays small while the index stays realistic.

| | `moov` | file |
|---|---|---|
| faststart | **1.66 MB** | 45.5 MB |
| fragmented | **0.001 MB** | 45.7 MB (+0.46%) |

**2276x smaller index for +0.46% file size.** No re-encode required —
`-c copy` remux, which took **2 seconds**.

### Verified in a real browser

Same frames, same server, throttled, muted autoplay:

| bandwidth | faststart TTFF | fragmented TTFF |
|---|---|---|
| 2 Mbps | 0.1 s | 0.1 s |
| **800 kbps** | **> 50 s (never started)** | **0.1 s** |

At 800 kbps the faststart file never produced a frame within 50 seconds; the
fragmented file started in 0.1 s.

---

## 4. What to implement

The transcode path already runs through `convertMKV`, so this is a contained
change: add `-movflags frag_keyframe+empty_moov+default_base_moof` when the
output is produced, instead of `+faststart` alone.

Caveats to check before shipping, stated honestly because they are **not yet
verified**:

- Chrome, Firefox and Safari all support fragmented MP4, but this was only
  measured in Chromium via ego-browser. Safari is the usual risk for fMP4
  variants and needs a real device test.
- Seeking in a fragmented file may be marginally slower for the *server-side*
  case because the browser must range-fetch a `moof` first. Not measured.
- Existing already-downloaded files keep their large index until remuxed. A
  one-off background remux pass (2 s per movie, `-c copy`) would migrate them.
- The `.part`/cache handling in `mkv_playback.go` keys on the file path, so
  regenerating cached MP4s is safe, but the cache would need invalidating to
  force the new format.

## 5. Not the answer

- **WebM/VP9**: see [`transcode-preset-benchmark.md`](transcode-preset-benchmark.md).
  The theater already runs 59.99 fps with 0 dropped frames, the shipped build has
  no libvpx encoder, and it does not address index size at all.
- **Bitrate capping**: changes bytes/second, not the startup index fetch.
- **`preload` tuning**: measured, no effect.
- **Server headers / range support**: already correct.

---

## Re-running

```bash
wsl -d Debian -- bash /mnt/d/cloudflix/tmp/build-long2.sh   # fixture + moov comparison
```

Browser probes (need a logged-in session on the live server):
`tmp/qa2/probe-cold.mjs` (cold-start TTFF), `tmp/qa2/probe-index-cost.mjs`
(index size + model check), `tmp/qa2/probe-fmp4-play.mjs` (A/B playback).
