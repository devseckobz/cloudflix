package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/devseckobz/cloudflix/internal/mediatools"
)

func TestBundledMKVConversionThroughPrivateFileDescriptor(t *testing.T) {
	dir := t.TempDir()
	ffmpeg, err := mediatools.Resolve(dir, "ffmpeg")
	if err != nil || !strings.Contains(ffmpeg, "runtime-tools") {
		t.Skip("release payload not generated")
	}
	t.Setenv("PATH", t.TempDir())
	mkv := filepath.Join(dir, "movie.mkv")
	args := []string{"-nostdin", "-v", "error", "-f", "lavfi", "-i", "color=size=32x32:rate=1", "-f", "lavfi", "-i", "sine=frequency=440", "-t", "1", "-c:v", "libx264", "-pix_fmt", "yuv420p", "-c:a", "aac", mkv}
	if out, err := exec.Command(ffmpeg, args...).CombinedOutput(); err != nil {
		t.Fatalf("fixture: %v: %s", err, out)
	}
	source, err := os.Open(mkv)
	if err != nil {
		t.Fatal(err)
	}
	a := &application{dataDir: dir, playback: newMKVManager()}
	job := &mkvJob{state: "queued"}
	hash := strings.Repeat("a", 40)
	key := mkvKey(hash, "movie.mkv")
	a.playback.jobs[key] = job
	a.convertMKV(source, hash, "movie.mkv", key, job)
	if job.state != "ready" {
		t.Fatalf("built-in conversion failed: %s: %s", job.state, job.message)
	}
	cache, err := a.mkvCacheDir()
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := cachedMKV(cache, key); !ok {
		t.Fatal("converted movie cache not ready")
	}
}
