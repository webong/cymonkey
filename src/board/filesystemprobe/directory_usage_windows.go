//go:build windows

package filesystemprobe

import "golang.org/x/sys/windows"

func DirectoryUsageBytes(dir string) (int64, bool) {
	capacity, ok := VolumeCapacityBytes(dir)
	if !ok {
		return 0, false
	}
	return capacity.UsedBytes(), true
}

func VolumeCapacityBytes(dir string) (VolumeCapacity, bool) {
	var available, total, free uint64
	if err := windows.GetDiskFreeSpaceEx(windows.StringToUTF16Ptr(dir), &available, &total, &free); err != nil {
		return VolumeCapacity{}, false
	}
	if available > total {
		return VolumeCapacity{}, false
	}
	return VolumeCapacity{CapacityBytes: total, AvailableBytes: available}, true
}
