//go:build !linux

package sandbox

// IsolateNetwork is a no-op on non-Linux platforms.
func IsolateNetwork() error {
	return nil
}
