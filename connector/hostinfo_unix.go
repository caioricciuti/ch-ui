//go:build !windows

package connector

import "syscall"

// getDiskInfo returns total and free disk space for the root filesystem
func getDiskInfo() (total, free uint64) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs("/", &stat); err != nil {
		return 0, 0
	}

	total = stat.Blocks * uint64(stat.Bsize)
	free = stat.Bfree * uint64(stat.Bsize)
	return
}
