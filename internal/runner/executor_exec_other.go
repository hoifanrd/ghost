//go:build !linux

package runner

import "fmt"

// ExecuteExec is not supported on non-Linux platforms.
func ExecuteExec(config *Config) error {
	return fmt.Errorf("exec mode is only supported on Linux")
}
