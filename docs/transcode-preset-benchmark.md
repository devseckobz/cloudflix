# MKV transcode preset benchmark

Answers two questions: whether re-encoding everything to WebM/VP9 would make
playback faster, and whether a faster x264 preset is worth switching to.

Measured on this machine (Windows host + WSL Debian, 16 vCPU, 8 threads as
`transcodeThreads()` computes it) using the **shipped** static `ffmpeg` 9.0.2
from `internal/mediatools/payload/ffmpeg.gz`, with the exact production
argument shape from `cmd/cloudflix/mkv_playback.go`.

Source: `tmp/perf/browser/media/clip-hevc-1080p60.mkv` — 15.023 s, 1920x1080,
**60 fps, HEVC, yuv420p, AAC**, 47.8 MB / 25.4 Mbps. This is the slow path: an
MKV with non-H.264 video, so it must actually be re-encoded rather than
remuxed.

---

## 1. Should we move to WebM / VP9?

**No.** Two independent reasons.

### Playback is already at the ceiling

From [`verification-60fps-and-ui.md`](verification-60fps-and-ui.md):

| Scenario | fps | dropped frames | long tasks |
|---|---|---|---|
| theater 1440x900 | 59.99 | 0 / 849 | 0 |
| theater 390x844 | 59.99 | 0 / 854 | 0 |

60 fps is the display refresh rate, and zero frames were dropped. There is no
headroom a better codec could recover — the player is already doing the maximum
the display allows. The margin is in hardware decode, which H.264 has
universally and VP9 does not.

### The build cannot even produce VP9

`scripts/build-media-tools.sh:61` enables only:

```
--enable-encoder=libx264,aac,webvtt
```

libvpx is not compiled in. Shipping WebM means adding it to both architecture
payloads: a larger binary and a longer `media-tools` CI job on every release,
for a codec that would then need software decode on many devices.

Subtitles are unaffected either way: they are served as sidecar WebVTT
(`mkv_playback.go:152-156`), independent of the container.

---

## 2. Is `ultrafast` a worthwhile preset switch?

**No.** CRF is not constant quality across presets, and at matched output size
`ultrafast` is both larger and worse.

Raw results, `-crf 24` for every preset (what production uses today):

| preset | time | output | realtime factor |
|---|---|---|---|
| **veryfast (current)** | **4.87 s** | **8.7 MB** | 3.1x |
| ultrafast | 2.48 s | **42.6 MB** | 6.0x |
| superfast | 3.78 s | 22.4 MB | 4.0x |
| medium | 8.87 s | 10.5 MB | 1.7x |

`ultrafast` at the same CRF is 2x faster but produces **4.9x the bytes** — a
47.8 MB source became 42.6 MB instead of 8.7 MB. That is a disaster for a
self-hosted box with finite disk.

Size-matched comparison (each preset tuned toward the 8.7 MB baseline):

| encode | time | output | SSIM vs source |
|---|---|---|---|
| veryfast crf 24 (current) | 4.87 s | 8.7 MB | **0.950661** |
| ultrafast crf 32 | 2.17 s | 19.9 MB | 0.947827 |
| ultrafast crf 35 | 2.04 s | 13.2 MB | 0.946566 |
| superfast crf 26 | 3.73 s | 17.6 MB | 0.950378 |

Even pushing `ultrafast` to crf 35 — well past the point of visible damage — it
still lands at 13.2 MB, **1.5x larger** than the 8.7 MB `veryfast` output, and
measures worse (SSIM 0.9466 vs 0.9507). There is no crf where `ultrafast` both
matches the size and matches the quality.

**Conclusion: keep `veryfast` / crf 24.** It is the best quality-per-byte in
this set, and the 2x speed gain available from `ultrafast` costs 1.5–4.9x the
disk for strictly worse output.

The `medium` row is the interesting one for a different reason: 1.7x the time
for only 20% more bytes and no measurable SSIM gain. Not worth it either — the
current setting sits in a genuine sweet spot.

---

## 3. Where transcode time actually goes

Worth recording so the next person does not re-derive it:

- **Already-H.264 MKVs never re-encode.** `mkv_playback.go:337` sets
  `copyVideo` when the codec is `h264` with `yuv420p`, so those are remuxed
  (`-c:v copy`) in seconds. The measured 4.87 s above is the worst case
  (HEVC), not the typical case.
- **Pre-warming** (v0.1.32) means this happens at download completion, not on
  the Watch click, so the user never waits for it.
- A real 2 h movie at this rate extrapolates to roughly 35 min of encoding. The
  earlier measured 23–26 min figure for a 2 h HEVC movie is consistent.

If transcode time ever becomes the bottleneck, the levers in order of value are
hardware encoding (VAAPI/NVENC — only if the VPS has a GPU), then accepting
`medium`, and last `ultrafast`. Not codec choice.

---

## Re-running

```bash
wsl -d Debian -- bash /mnt/d/cloudflix/tmp/bench-transcode.sh   # raw presets
wsl -d Debian -- bash /mnt/d/cloudflix/tmp/bench-matched.sh     # size-matched
```

Both extract the shipped ffmpeg to `tmp/bench-tools` and write encoded files to
`tmp/bench-out` (gitignored). Note `/tmp` does not survive between WSL
invocations on this machine, which is why the scripts use the workspace.
