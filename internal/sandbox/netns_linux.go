//go:build linux

package sandbox

import (
	"errors"
	"syscall"
)

// IsolateNetwork creates a new network namespace, isolating the process from
// the host network stack. If the process lacks CAP_SYS_ADMIN (EPERM), the
// error is silently ignored — this typically means the container is already
// network-isolated (e.g. Docker NetworkMode:"none").
func IsolateNetwork() error {
	if err := syscall.Unshare(syscall.CLONE_NEWNET); err != nil {
		if errors.Is(err, syscall.EPERM) {
			return nil
		}
		return err
	}
	return nil
}
