package filesystemprobe

// VolumeCapacity is a privacy-safe aggregate for one already-selected native
// volume. The native path used to query it remains inside the observer.
type VolumeCapacity struct {
	CapacityBytes  uint64
	AvailableBytes uint64
}

func (value VolumeCapacity) UsedBytes() int64 {
	if value.AvailableBytes > value.CapacityBytes {
		return 0
	}
	used := value.CapacityBytes - value.AvailableBytes
	if used > uint64(^uint64(0)>>1) {
		return int64(^uint64(0) >> 1)
	}
	return int64(used)
}
