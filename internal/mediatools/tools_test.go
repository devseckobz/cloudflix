package mediatools

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
)

func fixture(t *testing.T) (*manager, []byte) {
	t.Helper()
	raw := []byte("private executable fixture")
	var compressed bytes.Buffer
	w := gzip.NewWriter(&compressed)
	if _, err := w.Write(raw); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256(raw)
	return &manager{files: fstest.MapFS{
		"payload/ffmpeg.gz":     {Data: compressed.Bytes()},
		"payload/ffmpeg.sha256": {Data: []byte(hex.EncodeToString(hash[:]))},
	}}, raw
}

func TestPrivateToolsExtractAndRepairAcrossRestarts(t *testing.T) {
	m, raw := fixture(t)
	dir := t.TempDir()
	path, err := m.resolve(dir, "ffmpeg")
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(data, raw) {
		t.Fatal("extracted wrong tool", err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0700 {
		t.Fatal("tool permissions must be private", err)
	}
	if err := os.WriteFile(path, []byte("corrupted"), 0600); err != nil {
		t.Fatal(err)
	}
	restarted := manager{files: m.files}
	repaired, err := restarted.resolve(dir, "ffmpeg")
	if err != nil || repaired != path {
		t.Fatal("repair failed", err)
	}
	data, _ = os.ReadFile(path)
	if !bytes.Equal(data, raw) {
		t.Fatal("tool integrity not restored")
	}
}

func TestPrivateToolsRejectInvalidPayloadAndUnsafePaths(t *testing.T) {
	m, _ := fixture(t)
	if _, err := m.resolve(t.TempDir(), "../ffmpeg"); err == nil {
		t.Fatal("accepted unsafe tool name")
	}
	m.files.(fstest.MapFS)["payload/ffmpeg.sha256"] = &fstest.MapFile{Data: []byte(strings.Repeat("0", 64))}
	if _, err := m.resolve(t.TempDir(), "ffmpeg"); err == nil {
		t.Fatal("accepted checksum mismatch")
	}
	m, _ = fixture(t)
	dir := t.TempDir()
	if err := os.Symlink(t.TempDir(), filepath.Join(dir, "runtime-tools")); err != nil {
		t.Fatal(err)
	}
	if _, err := m.resolve(dir, "ffmpeg"); err == nil {
		t.Fatal("extracted through escaping symlink")
	}
}

func TestPrivateToolsConcurrentPreparation(t *testing.T) {
	m, _ := fixture(t)
	dir := t.TempDir()
	var wg sync.WaitGroup
	for range 6 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := m.resolve(dir, "ffmpeg"); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
}

// Release packaging runs this against the actual embedded native payload.
// PATH is deliberately empty: no host ffmpeg or ffprobe may satisfy this test.
func TestBundledMKVPlaybackWithoutSystemFFmpeg(t *testing.T) {
	if _, err := fs.Stat(payload, "payload/ffmpeg.gz"); err != nil {
		t.Skip("source-only build; release CI builds and tests native payloads")
	}
	t.Setenv("PATH", t.TempDir())
	dir := t.TempDir()
	ffmpeg, err := Resolve(dir, "ffmpeg")
	if err != nil {
		t.Fatal(err)
	}
	ffprobe, err := Resolve(dir, "ffprobe")
	if err != nil {
		t.Fatal(err)
	}
	run := func(tool string, args ...string) []byte {
		t.Helper()
		out, err := exec.Command(tool, args...).CombinedOutput()
		if err != nil {
			t.Fatalf("%s: %v\n%s", filepath.Base(tool), err, out)
		}
		return out
	}
	mkv, mp4 := filepath.Join(dir, "movie.mkv"), filepath.Join(dir, "movie.mp4")
	run(ffmpeg, "-nostdin", "-v", "error", "-f", "lavfi", "-i", "color=size=32x32:rate=1", "-f", "lavfi", "-i", "sine=frequency=440", "-t", "1", "-c:v", "libx264", "-pix_fmt", "yuv420p", "-c:a", "aac", mkv)
	run(ffmpeg, "-nostdin", "-v", "error", "-i", mkv, "-c:v", "libx264", "-c:a", "aac", "-pix_fmt", "yuv420p", "-movflags", "+faststart", mp4)
	probe := run(ffprobe, "-v", "error", "-show_entries", "stream=codec_name", "-of", "json", mp4)
	if !bytes.Contains(probe, []byte("h264")) || !bytes.Contains(probe, []byte("aac")) {
		t.Fatalf("incompatible output: %s", probe)
	}
	// Text subtitle extraction is part of the existing Cloudflix CC contract.
	srt, subbed, vtt := filepath.Join(dir, "captions.srt"), filepath.Join(dir, "subbed.mkv"), filepath.Join(dir, "captions.vtt")
	if err := os.WriteFile(srt, []byte("1\n00:00:00,000 --> 00:00:00,900\nBuilt-in captions\n"), 0600); err != nil {
		t.Fatal(err)
	}
	// subrip is decoded for CC; the muxing fixture copies the existing packets.
	run(ffmpeg, "-nostdin", "-v", "error", "-i", mkv, "-i", srt, "-map", "0", "-map", "1", "-c", "copy", subbed)
	run(ffmpeg, "-nostdin", "-v", "error", "-i", subbed, "-map", "0:s:0", "-c:s", "webvtt", vtt)
	text, err := os.ReadFile(vtt)
	if err != nil || !bytes.Contains(text, []byte("Built-in captions")) {
		t.Fatal("embedded caption extraction failed", err)
	}
}
