//go:build !linux

package sandbox

// ApplySandbox is a no-op on non-Linux platforms.
func ApplySandbox(workDir string) error {
	return nil
}

// EnforceMaxPids is a no-op on non-Linux platforms.
func EnforceMaxPids(maxPids uint64) error {
	return nil
}
