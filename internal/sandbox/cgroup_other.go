//go:build !linux

package sandbox

// CgroupV2Available is a no-op on non-Linux platforms.
func CgroupV2Available() bool { return false }

// ReadMemoryCurrent is a no-op on non-Linux platforms.
func ReadMemoryCurrent() (int64, bool) { return 0, false }

// ReadMemoryPeak is a no-op on non-Linux platforms.
func ReadMemoryPeak() (int64, bool) { return 0, false }

// ReadOOMKillCount is a no-op on non-Linux platforms.
func ReadOOMKillCount() int64 { return 0 }
