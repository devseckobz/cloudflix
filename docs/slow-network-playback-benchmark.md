# Slow-network playback: what actually helps

Question: how do we make downloaded MKV/MP4 play well on a slow connection?

Measured on this machine (16 vCPU, 8 threads as `transcodeThreads()` computes)
with the **shipped** static ffmpeg 9.0.2 from
`internal/mediatools/payload/ffmpeg.gz`, production argument shape from
`cmd/cloudflix/mkv_playback.go`.

Source media, both real 1920x1080 **60 fps**:

| file | codec | size | bitrate |
|---|---|---|---|
| `tmp/perf/browser/media/clip-h264-1080p60.mp4` | h264 | 143.6 MB / 30 s | **40.2 Mbps** |
| `tmp/perf/browser/media/clip-hevc-1080p60.mkv` | hevc | 45.6 MB / 15 s | ~24 Mbps |

---

## 1. The problem is bitrate, not codec

Playback already runs at 60 fps with zero drops *on a fast link*
([`verification-60fps-and-ui.md`](verification-60fps-and-ui.md)). A slow
connection fails differently: the stream cannot be fed fast enough to keep the
buffer ahead of playback. Codec choice changes bytes-per-quality, but only
slightly — bitrate is the direct lever. A 40 Mbps file needs a 40 Mbps link
regardless of whether it is H.264 or VP9.

## 2. The real gap: MP4 is served untouched

`App.svelte:1233` selects the source as:

```
selectedIsMKV ? playbackVideoURL : mediaURL(selected.hash, path)
```

So an **MP4 is streamed byte-for-byte** from disk, and an MKV goes through the
transcoder. There is no bitrate cap on the MP4 path at all, and
`mkv_playback.go:347` uses `-crf 24` with **no `-maxrate`**, so even the
transcode path has no bandwidth ceiling — only whatever CRF happens to produce.

## 3. Measured renditions of the 40.2 Mbps MP4

| rendition | encode time | bandwidth | size | SSIM vs source |
|---|---|---|---|---|
| **source (served as-is)** | — | **40.2 Mbps** | 143.6 MB | 1.000 |
| 1080p, cap 4M | 10.63 s | **4.24 Mbps** | 15.2 MB | 0.9857 |
| 720p, cap 2M | 5.66 s | **1.36 Mbps** | 4.9 MB | 0.9653 |
| 720p, cap 1M | 5.52 s | **1.04 Mbps** | 3.7 MB | 0.9635 |

**A 1080p rendition capped at 4 Mbps is 9.5x less bandwidth for SSIM 0.9857.**
It takes 10.6 s for a 30 s clip (~0.35x realtime), so a 2 h movie is roughly
40 min of background encoding — acceptable because v0.1.32 pre-warms on
download completion.

For comparison, the 720p tiers buy little: 1.36 Mbps at SSIM 0.965, and pushing
to 1 Mbps costs almost nothing more in quality (0.9635) because the ceiling is
already biting. 720p is the right fallback for genuinely poor links, but 1080p
at 4 Mbps is the better default.

## 4. CRF alone is not a bitrate cap

The existing `-crf 24` output for the HEVC source landed at 5.92 Mbps — but
that is *incidental*, not guaranteed. A grainy or high-motion movie at crf 24
can exceed 20 Mbps with the same settings. Anything that must respect a slow
link needs an explicit `-maxrate`/`-bufsize`, not just CRF.

---

## Recommendation

Changing the codec (WebM/VP9) does **not** address this — see
[`transcode-preset-benchmark.md`](transcode-preset-benchmark.md) for why that is
also the wrong direction. The work that would actually help, in value order:

1. **Cap the bitrate on the transcode path.** Add `-maxrate 4M -bufsize 8M` to
   the existing x264 args. Small, contained change in
   `mkv_playback.go:347`; measured to give 4.24 Mbps at SSIM 0.9857.
2. **Route high-bitrate MP4s through the same transcoder.** Currently an MP4 is
   always served raw, so a 40 Mbps MP4 is unplayable on a slow link even though
   the transcoder could produce a 4 Mbps version. This is a larger change: the
   MP4 path needs the same "prepare then serve the cache" flow the MKV path
   already has.
3. **Let the user pick a quality tier** (Auto / 1080p / 720p) so a poor link can
   be dropped a tier without a re-download.

Not measured here, and worth stating plainly: **actual playback behaviour on a
throttled link** (buffering stalls, time-to-first-frame, whether the browser
adapts its range requests). The numbers above are encode-side only. Confirming
the improvement would need a throttled-network playback test against these
renditions, which is the next step before claiming a fix.

---

## Re-running

```bash
wsl -d Debian -- bash /mnt/d/cloudflix/tmp/bench-ladder.sh    # 1080p/720p/480p ladder
wsl -d Debian -- bash /mnt/d/cloudflix/tmp/bench-mp4-gap.sh   # capped MP4 renditions
```

Scripts extract the shipped ffmpeg to `tmp/bench-tools` and write to
`tmp/bench-ladder` (gitignored). `/tmp` does not survive between WSL
invocations on this machine, hence the workspace paths.
