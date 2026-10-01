//go:build darwin || dragonfly || freebsd || linux || netbsd || openbsd || solaris

// Package filesystemprobe contains the narrowly scoped host filesystem probe
// for already-selected volumes.
package filesystemprobe

import "golang.org/x/sys/unix"

func DirectoryUsageBytes(dir string) (int64, bool) {
	capacity, ok := VolumeCapacityBytes(dir)
	if !ok {
		return 0, false
	}
	return capacity.UsedBytes(), true
}

func VolumeCapacityBytes(dir string) (VolumeCapacity, bool) {
	var stat unix.Statfs_t
	if err := unix.Statfs(dir, &stat); err != nil {
		return VolumeCapacity{}, false
	}
	blockSize := uint64(stat.Bsize)
	return VolumeCapacity{
		CapacityBytes:  uint64(stat.Blocks) * blockSize,
		AvailableBytes: uint64(stat.Bavail) * blockSize,
	}, true
}
