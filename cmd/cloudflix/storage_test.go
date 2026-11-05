package main

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/bencode"
	"github.com/anacrolix/torrent/metainfo"
)

func TestStorageCapacity(t *testing.T) {
	for _, tc := range []struct {
		name                string
		known               bool
		available, required uint64
		wantError           bool
	}{
		{"fits exactly", true, 10, 10, false},
		{"one byte short", true, 10, 11, true},
		{"completed file", true, 0, 0, false},
		{"unknown fails closed", false, 0, 0, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := checkStorageCapacity(storageStatus{Known: tc.known, Available: tc.available}, tc.required)
			if (err != nil) != tc.wantError {
				t.Fatalf("unexpected capacity result: %v", err)
			}
		})
	}
	if remainingBytes(100, 40) != 60 || remainingBytes(100, 150) != 0 || remainingBytes(100, -2) != 100 {
		t.Fatal("invalid partial file accounting")
	}
}

func TestStorageReadsDownloadFilesystemAndFailsClosed(t *testing.T) {
	e := &engine{downloads: "download-mount", diskSpace: func(path string) (uint64, uint64, error) {
		if path != "download-mount" {
			t.Fatal("checked wrong mount")
		}
		return 1000, storageHeadroom + 250, nil
	}}
	s := e.storage()
	if !s.Known || s.Available != 250 || s.Total != 1000 {
		t.Fatalf("unexpected storage: %+v", s)
	}
	e.diskSpace = func(string) (uint64, uint64, error) { return 0, 0, errors.New("mount unavailable") }
	if s = e.storage(); s.Known || s.Error == "" || checkStorageCapacity(s, 1) == nil {
		t.Fatal("unreadable storage must block downloads")
	}
	if _, free, err := filesystemSpace(t.TempDir()); err != nil || free == 0 {
		t.Fatalf("real filesystem probe failed: %d %v", free, err)
	}
}

func storageFixture(t *testing.T, count int, free uint64) (*engine, []*entry) {
	t.Helper()
	cfg := torrent.NewDefaultClientConfig()
	cfg.DataDir = t.TempDir()
	cfg.DisableTCP = true
	cfg.DisableUTP = true
	cfg.NoDHT = true
	cfg.DisableTrackers = true
	cfg.NoDefaultPortForwarding = true
	cfg.NoUpload = true
	client, err := torrent.NewClient(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { client.Close() })
	db := testPINDB(t)
	if _, err = db.Exec("CREATE TABLE torrents (hash TEXT PRIMARY KEY, rejection_reason TEXT NOT NULL DEFAULT '', paused INTEGER DEFAULT 0)"); err != nil {
		t.Fatal(err)
	}
	e := &engine{client: client, db: db, downloads: cfg.DataDir, entries: map[string]*entry{}, diskSpace: func(string) (uint64, uint64, error) { return free * 2, free, nil }}
	var items []*entry
	for i := 0; i < count; i++ {
		info := metainfo.Info{Name: strings.Repeat("m", i+1), PieceLength: 1 << 20, Files: []metainfo.FileInfo{
			{Path: []string{"movie.mp4"}, Length: 2 << 20},
			{Path: []string{"movie.srt"}, Length: 1024},
			{Path: []string{"ignored.bin"}, Length: 8 << 20},
		}, Pieces: make([]byte, 11*20)}
		raw, err := bencode.Marshal(info)
		if err != nil {
			t.Fatal(err)
		}
		tt, _, err := client.AddTorrentSpec(&torrent.TorrentSpec{AddTorrentOpts: torrent.AddTorrentOpts{InfoHash: metainfo.HashBytes(raw), InfoBytes: raw, DisallowDataDownload: true, DisallowDataUpload: true, DisableInitialPieceCheck: true}})
		if err != nil {
			t.Fatal(err)
		}
		item := &entry{hash: tt.InfoHash().HexString(), title: info.Name, torrent: tt}
		e.entries[item.hash] = item
		if _, err = db.Exec("INSERT INTO torrents(hash) VALUES(?)", item.hash); err != nil {
			t.Fatal(err)
		}
		items = append(items, item)
	}
	return e, items
}

func awaitStorageDecision(t *testing.T, e *engine, items []*entry) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		e.mu.Lock()
		pending := false
		for _, item := range items {
			if !item.validated && item.rejectionReason == "" {
				pending = true
			}
		}
		e.mu.Unlock()
		if !pending {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("metadata validation did not finish")
}

func TestConcurrentMetadataCannotOvercommitDisk(t *testing.T) {
	e, items := storageFixture(t, 2, storageHeadroom+(3<<20))
	for _, item := range items {
		e.startWhenReady(item)
	}
	awaitStorageDecision(t, e, items)
	e.mu.Lock()
	defer e.mu.Unlock()
	accepted, rejected := 0, 0
	for _, item := range items {
		if item.validated {
			accepted++
			item.paused = true // Paused downloads must keep their reservation.
			if got := e.storageLocked(nil).Reserved; got != (2<<20)+1024 {
				t.Fatalf("incorrect selected-file reservation: %d", got)
			}
		} else {
			rejected++
			if !strings.Contains(item.rejectionReason, "Not enough disk space") || item.torrent != nil {
				t.Fatalf("unsafe rejection: %+v", item)
			}
			var reason string
			if err := e.db.QueryRow("SELECT rejection_reason FROM torrents WHERE hash=?", item.hash).Scan(&reason); err != nil || reason != item.rejectionReason {
				t.Fatal("rejection was not persisted")
			}
			if item.fileManifest == nil {
				t.Fatal("rejected files must remain inspectable")
			}
			for _, file := range item.fileManifest.Files {
				if file.Selected {
					t.Fatal("storage-rejected torrent must not advertise selected files")
				}
			}
		}
	}
	if accepted != 1 || rejected != 1 {
		t.Fatalf("accepted=%d rejected=%d", accepted, rejected)
	}
}

func TestResumeRechecksStorageWithoutUnpausing(t *testing.T) {
	e, items := storageFixture(t, 1, storageHeadroom+(3<<20))
	e.startWhenReady(items[0])
	awaitStorageDecision(t, e, items)
	if err := e.action(items[0].hash, "pause"); err != nil {
		t.Fatal(err)
	}
	e.diskSpace = func(string) (uint64, uint64, error) { return storageHeadroom, storageHeadroom, nil }
	if err := e.action(items[0].hash, "resume"); err == nil {
		t.Fatal("resume overcommitted disk")
	}
	if !items[0].paused {
		t.Fatal("failed resume must remain paused")
	}
}
