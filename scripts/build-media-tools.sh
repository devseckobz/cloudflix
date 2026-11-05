#!/usr/bin/env bash
# Native build: run once on each release architecture. No distro media libraries.
set -Eeuo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
OUTPUT="${1:-$ROOT/internal/mediatools/payload}"
CACHE="${CLOUDFLIX_MEDIA_SOURCE_CACHE:-$ROOT/tmp/media-sources}"
FFMPEG=9.0.2
X264=b35605ace3ddf7c1a5d67a2eb553f034aef41d55
mkdir -p "$OUTPUT" "$CACHE" "$ROOT/tmp"
OUTPUT="$(cd "$OUTPUT" && pwd)"
work="$(mktemp -d)"
trap 'code=$?; if [[ "$code" != 0 && -f "$work/ffmpeg-$FFMPEG/ffbuild/config.log" ]];then cp "$work/ffmpeg-$FFMPEG/ffbuild/config.log" "$ROOT/tmp/media-config.log";fi; rm -rf "$work"' EXIT
# A release build must not fail because one upstream mirror briefly refuses a
# connection. ffmpeg.org intermittently resets TLS (curl exit 35) and can exceed
# a short connect timeout, which aborted v0.1.24's arm64 job mid-download. Retry
# every error class, fall back to the www host, and only then give up. The
# checksum is still verified afterwards, so a mirror swap cannot smuggle in
# different bytes.
fetch_source(){
 local name="$1" sha="$2"
 local -a urls=("${@:3}")
 local tmp="$CACHE/$name.part" url
 [[ -f "$CACHE/$name" ]] && { printf '%s  %s\n' "$sha" "$CACHE/$name" | sha256sum -c -; return; }
 for url in "${urls[@]}";do
  rm -f "$tmp"
  if curl -fL --retry 8 --retry-delay 5 --retry-all-errors --connect-timeout 30 \
      --max-time 900 "$url" -o "$tmp";then
   mv -f "$tmp" "$CACHE/$name"
   break
  fi
  printf 'warning: download failed from %s; trying next source\n' "$url" >&2
 done
 [[ -f "$CACHE/$name" ]] || { printf 'ERROR: could not download %s from any source\n' "$name" >&2; return 1; }
 printf '%s  %s\n' "$sha" "$CACHE/$name" | sha256sum -c -
}
fetch_source "ffmpeg-$FFMPEG.tar.xz" 8c3850283eb25fa026482078a04051e0be17347b09ef81a0849bec15a96e002e \
 "https://ffmpeg.org/releases/ffmpeg-$FFMPEG.tar.xz" \
 "https://www.ffmpeg.org/releases/ffmpeg-$FFMPEG.tar.xz"
fetch_source "x264-$X264.tar.bz2" 6eeb82934e69fd51e043bd8c5b0d152839638d1ce7aa4eea65a3fedcf83ff224 \
 "https://code.videolan.org/videolan/x264/-/archive/$X264/x264-$X264.tar.bz2"
tar -xf "$CACHE/x264-$X264.tar.bz2" -C "$work"
tar -xf "$CACHE/ffmpeg-$FFMPEG.tar.xz" -C "$work"
jobs="${CLOUDFLIX_MEDIA_JOBS:-$(getconf _NPROCESSORS_ONLN)}"
(
 cd "$work/x264-$X264"
 CC=musl-gcc ./configure --prefix="$work/prefix" --enable-static --disable-cli --disable-opencl --enable-pic
 make -j "$jobs"
 make install
)
(
 cd "$work/ffmpeg-$FFMPEG"
 PKG_CONFIG_PATH="$work/prefix/lib/pkgconfig" ./configure \
  --prefix="$work/prefix" --cc=musl-gcc --disable-autodetect --disable-doc --disable-debug \
  --disable-network --disable-shared --enable-static --enable-gpl --enable-libx264 \
  --pkg-config-flags=--static --extra-cflags="-I$work/prefix/include" \
  --extra-ldflags="-L$work/prefix/lib -static" --disable-everything \
  --enable-ffmpeg --enable-ffprobe --disable-ffplay --enable-protocol=file,pipe \
  --enable-demuxer=matroska,mov,srt --enable-muxer=mp4,webvtt,matroska \
  --enable-decoder=h264,hevc,mpeg4,mpeg2video,vp8,vp9,av1,aac,ac3,eac3,dca,flac,mp3,opus,vorbis,pcm_s16le,pcm_s32le,pcm_f32le,pcm_f64le,subrip,ass,webvtt,movtext,wrapped_avframe \
  --enable-parser=h264,hevc,mpeg4video,mpegvideo,vp8,vp9,av1,aac,ac3,dca,flac,mpegaudio,opus,vorbis \
  --enable-encoder=libx264,aac,webvtt --enable-bsf=aac_adtstoasc,extract_extradata \
  --enable-filter=buffer,buffersink,abuffer,abuffersink,format,scale,aresample,aformat,null,anull,color,sine \
  --enable-indev=lavfi
 make -j "$jobs" ffmpeg ffprobe
 # Reject accidental dynamic linkage: the VPS needs no FFmpeg package or libs.
 for tool in ffmpeg ffprobe;do
  if readelf -l "$tool" | grep -q INTERP;then echo "$tool is not static" >&2;exit 1;fi
  strip "$tool"
  gzip -n -9 -c "$tool" > "$OUTPUT/$tool.gz"
  sha256sum "$tool" | cut -d' ' -f1 > "$OUTPUT/$tool.sha256"
 done
 # Exercise the codecs and containers used by Cloudflix, using only our tools.
 ./ffmpeg -nostdin -v error -f lavfi -i color=size=32x32:rate=1 -f lavfi -i sine=frequency=440 -t 1 -c:v libx264 -pix_fmt yuv420p -c:a aac "$work/smoke.mkv"
 ./ffprobe -v error -show_entries stream=codec_name -of json "$work/smoke.mkv"
 ./ffmpeg -nostdin -v error -i "$work/smoke.mkv" -c copy -movflags +faststart "$work/smoke.mp4"
)
cp "$work/ffmpeg-$FFMPEG/COPYING.GPLv2" "$OUTPUT/FFmpeg-COPYING.txt"
cp "$work/x264-$X264/COPYING" "$OUTPUT/x264-COPYING.txt"
cp /usr/share/doc/musl/copyright "$OUTPUT/musl-COPYRIGHT.txt"
printf 'FFmpeg %s; x264 %s\nBuilt by scripts/build-media-tools.sh with static libraries.\n' "$FFMPEG" "$X264" > "$OUTPUT/VERSIONS.txt"
# Corresponding sources and the exact build recipe accompany every release.
cp "$ROOT/scripts/build-media-tools.sh" "$CACHE/build-media-tools.sh"
tar -czf "$ROOT/tmp/cloudflix-media-sources.tar.gz" -C "$CACHE" "ffmpeg-$FFMPEG.tar.xz" "x264-$X264.tar.bz2" build-media-tools.sh
