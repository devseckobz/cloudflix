package main

import (
	"errors"
	"fmt"
	"math"
	"path/filepath"

	"github.com/anacrolix/torrent"
)

// Leave room for SQLite, piece boundaries and normal service operation.
const storageHeadroom uint64 = 64 << 20

type storageStatus struct {
	Known     bool   `json:"known"`
	Total     uint64 `json:"total"`
	Free      uint64 `json:"free"`
	Reserved  uint64 `json:"reserved"`
	Available uint64 `json:"available"`
	Headroom  uint64 `json:"headroom"`
	Error     string `json:"error,omitempty"`
}

func saturatingAdd(a, b uint64) uint64 {
	if b > math.MaxUint64-a {
		return math.MaxUint64
	}
	return a + b
}

func remainingBytes(size, completed int64) uint64 {
	if size <= 0 {
		return 0
	}
	if completed < 0 {
		completed = 0
	}
	if completed >= size {
		return 0
	}
	return uint64(size - completed)
}

func torrentRemainingBytes(tt *torrent.Torrent) uint64 {
	if tt == nil || tt.Info() == nil {
		return 0
	}
	if tt.Complete().Bool() {
		return 0
	}
	var remaining uint64
	for _, file := range tt.Files() {
		if allowedMediaPath(filepath.ToSlash(file.Path())) {
			remaining = saturatingAdd(remaining, remainingBytes(file.Length(), file.BytesCompleted()))
		}
	}
	return remaining
}

// Caller holds e.mu. Paused jobs retain their reservation, so resuming them
// cannot overcommit a second torrent admitted while they were paused.
func (e *engine) reservedBytesLocked(exclude *entry) uint64 {
	var reserved uint64
	for _, item := range e.entries {
		if item == exclude || !item.validated || item.rejectionReason != "" || item.torrent == nil {
			continue
		}
		reserved = saturatingAdd(reserved, torrentRemainingBytes(item.torrent))
	}
	return reserved
}

func (e *engine) storageLocked(exclude *entry) storageStatus {
	status := storageStatus{Headroom: storageHeadroom, Reserved: e.reservedBytesLocked(exclude)}
	read := e.diskSpace
	if read == nil {
		read = filesystemSpace
	}
	total, free, err := read(e.downloads)
	if err != nil {
		status.Error = "Disk space is unavailable. Downloads cannot start until storage can be checked."
		return status
	}
	status.Known, status.Total, status.Free = true, total, free
	committed := saturatingAdd(status.Reserved, status.Headroom)
	if free > committed {
		status.Available = free - committed
	}
	return status
}

func (e *engine) storage() storageStatus {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.storageLocked(nil)
}

func checkStorageCapacity(status storageStatus, required uint64) error {
	if !status.Known {
		return errors.New("Cannot check available disk space. No additional movie content was requested. Try again after fixing server storage.")
	}
	if required > status.Available {
		return fmt.Errorf("Not enough disk space: this download needs %s more, but only %s is available after queued downloads and the 64 MB safety reserve. No additional movie content was requested.", storageBytes(required), storageBytes(status.Available))
	}
	return nil
}

func storageBytes(n uint64) string {
	if n < 1024 {
		return fmt.Sprintf("%d B", n)
	}
	size, unit := float64(n), "B"
	for _, next := range []string{"KB", "MB", "GB", "TB", "PB", "EB"} {
		size /= 1024
		unit = next
		if size < 1024 {
			break
		}
	}
	return fmt.Sprintf("%.1f %s", size, unit)
}

// Must run under e.mu before selecting files or allowing any payload transfer.
func (e *engine) checkTorrentStorageLocked(item *entry) error {
	return checkStorageCapacity(e.storageLocked(item), torrentRemainingBytes(item.torrent))
}
