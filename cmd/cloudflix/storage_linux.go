package main

import "golang.org/x/sys/unix"

// Bavail is the space available to the unprivileged Cloudflix service, rather
// than root's reserved filesystem blocks. Inspect the actual download mount.
func filesystemSpace(path string) (total, free uint64, err error) {
	var stat unix.Statfs_t
	if err = unix.Statfs(path, &stat); err != nil {
		return
	}
	total = stat.Blocks * uint64(stat.Bsize)
	free = stat.Bavail * uint64(stat.Bsize)
	return
}
