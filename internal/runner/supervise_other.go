//go:build !linux

package runner

import "fmt"

// Supervise is not supported on non-Linux platforms.
func Supervise(config *Config) error {
	return fmt.Errorf("supervise mode is only supported on Linux")
}
