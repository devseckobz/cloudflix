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

## 4. What was implemented

`convertMKV` now emits one of two container layouts, chosen per client and
cached side by side:

| client | cache key | `-movflags` |
|---|---|---|
| Chromium, Firefox | `<sha256>` | `frag_keyframe+empty_moov+default_base_moof` |
| Safari / all iOS browsers / unknown | `<sha256>.faststart` | `+faststart` |

Why an **allowlist** rather than a Safari denylist: every browser on iOS and
iPadOS renders with WebKit whatever its brand token says, so `CriOS`, `FxiOS`
and `EdgiOS` are Safari's engine underneath and must not get fMP4. Only an agent
that positively claims `Chrome/`, `Chromium/` or `Firefox/…Gecko/` is fragmented;
anything unrecognised keeps the layout it gets today. That is deliberately
conservative — a broken player is worse than a slow start.

Both layouts are keyed separately so a Safari request never waits on, or
receives, a Chromium transcode, and the prewarm that runs when a download
completes builds the default (fragmented) one.

### Migration of already-cached files

Cache entries written before this change hold a **faststart** MP4 under the
plain key, while the plain key now means *fragmented*. The key alone therefore
cannot describe the bytes, so `readyLayout` reads the file's top-level boxes:

- a legacy faststart entry under the plain key is **moved** into the `.faststart`
  slot, so WebKit viewers keep playing immediately;
- the freed plain key then gets a freshly fragmented transcode for Chromium;
- a cache file that does not parse as MP4 is trusted as-is, because re-encoding
  a movie on every request is worse than playing a possibly-old layout.

Both layouts are also removed by `deletePlaybackCacheLocked`.

### Measured after implementation

Real browser (ego-browser Chromium), real fragmented file, same frames as before:

| property | faststart | fragmented |
|---|---|---|
| `video.duration` | 7200 s, finite | **7200 s, finite** |
| `seekable` range end | 7200 s | **7200 s** |
| mid-film seek error | 0 s | **0 s** |

The metadata contract the app's resume-seek and progress bar depend on is
unchanged (`tmp/qa2/probe-fmp4-meta.mjs`). `TestFFmpegProducesTheTwoLayouts`
runs the real bundled ffmpeg through `convertMKV` for both keys and asserts the
browser-facing container: the default output has `moof` boxes and a tiny `moov`,
the Safari output has none and puts `moov` before `mdat`.

### Still not verified

- **Safari on a real device.** The safety argument is that Safari receives
  byte-identical output to what it gets today, not that fMP4 works there. No
  device was available to test.
- Seek *latency* on a throttled link (a `moof` must be range-fetched first).
  Seek correctness was measured above; its speed was not.

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
