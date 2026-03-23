//go:build linux

package sandbox

import (
	"fmt"
	"syscall"
)

// IsolateNetwork creates a new network namespace, isolating the process from
// the host network stack.
func IsolateNetwork() error {
	if err := syscall.Unshare(syscall.CLONE_NEWNET); err != nil {
		return fmt.Errorf("sandbox: unshare CLONE_NEWNET: %w", err)
	}
	return nil
}
