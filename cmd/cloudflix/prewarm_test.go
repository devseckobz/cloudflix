package main

import (
 "testing"

 "github.com/anacrolix/torrent"
 "github.com/anacrolix/torrent/bencode"
 "github.com/anacrolix/torrent/metainfo"
)

// prewarmFixture builds a completed MKV torrent so the prewarm selection has
// real metadata to judge, and records what it asks the transcoder to prepare.
func prewarmFixture(t *testing.T, mkv bool) (*engine, *[]string, string) {
 t.Helper()
 client := perfClient(t)
 const pieceLength int64 = 1 << 20
 ext := ".mkv"
 if !mkv {
  ext = ".mp4"
 }
 info := metainfo.Info{
  Name:        "Movie2024",
  PieceLength: pieceLength,
  Files:       []metainfo.FileInfo{{Path: []string{"Movie2024" + ext}, Length: pieceLength}},
  Pieces:      make([]byte, 20),
 }
 raw, err := bencode.Marshal(info)
 if err != nil {
  t.Fatal(err)
 }
 tt, _, err := client.AddTorrentSpec(&torrent.TorrentSpec{AddTorrentOpts: torrent.AddTorrentOpts{
  InfoHash: metainfo.HashBytes(raw), InfoBytes: raw,
  Storage: allCompleteStorage{}, DisableInitialPieceCheck: true,
 }})
 if err != nil {
  t.Fatal(err)
 }
 hash := tt.InfoHash().HexString()
 e := &engine{
  client:    client,
  downloads: t.TempDir(),
  entries:   map[string]*entry{hash: {hash: hash, torrent: tt, validated: true}},
  diskSpace: func(string) (uint64, uint64, error) { return 1 << 40, 1 << 39, nil },
 }
 calls := make([]string, 0)
 e.prewarm = func(h, path string) { calls = append(calls, h+"|"+path) }
 return e, &calls, hash
}

func TestPrewarmPreparesACompletedMKV(t *testing.T){
 e, calls, _ := prewarmFixture(t, true)
 e.prewarmCompleted(t.Context())
 if len(*calls) != 1 {
  t.Fatalf("expected exactly one prewarm call, got %v", *calls)
 }
 if !hasSuffix((*calls)[0], ".mkv") {
  t.Fatalf("prewarmed the wrong file: %q", (*calls)[0])
 }
}

func TestPrewarmIgnoresMP4(t *testing.T){
 // An MP4 plays natively, so converting it would waste hours for nothing.
 e, calls, _ := prewarmFixture(t, false)
 e.prewarmCompleted(t.Context())
 if len(*calls) != 0 {
  t.Fatalf("prewarm queued a native MP4: %v", *calls)
 }
}

func TestPrewarmDoesNotRepeatForTheSameFile(t *testing.T){
 e, calls, _ := prewarmFixture(t, true)
 e.prewarmCompleted(t.Context())
 e.prewarmCompleted(t.Context())
 e.prewarmCompleted(t.Context())
 if len(*calls) != 1 {
  t.Fatalf("same file queued %d times, want 1: %v", len(*calls), *calls)
 }
}

func TestPrewarmSkipsRejectedAndPausedTorrents(t *testing.T){
 e, calls, hash := prewarmFixture(t, true)
 e.entries[hash].rejectionReason = "Rejected: no MP4"
 e.prewarmCompleted(t.Context())
 if len(*calls) != 0 {
  t.Fatalf("prewarm queued a rejected torrent: %v", *calls)
 }
}

func TestPrewarmIsInertWithoutATranscoder(t *testing.T){
 e, _, _ := prewarmFixture(t, true)
 e.prewarm = nil
 // Must not panic when no media tools are configured (tests, CLI use).
 e.prewarmCompleted(t.Context())
}

func hasSuffix(s, suffix string) bool {
 return len(s) >= len(suffix) && s[len(s)-len(suffix):] == suffix
}
