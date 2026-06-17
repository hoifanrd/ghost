//go:build !linux

package reaper

// Start is a no-op on non-Linux platforms.
func Start() {}
