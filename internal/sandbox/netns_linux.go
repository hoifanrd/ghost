//go:build linux

package sandbox

import (
	"errors"
	"syscall"
)

// IsolateNetwork creates a new network namespace, isolating the spawned
// command from the network. unshare(CLONE_NEWNET) requires CAP_SYS_ADMIN;
// a container that has dropped all capabilities (the grading posture)
// will get EPERM. We do NOT treat that as success-by-isolation: it means
// the per-exec netns isolation is NOT in effect and the command shares
// the container's network namespace. Whether that is safe depends on the
// container's own network configuration — a NetworkMode:"none" container
// is isolated regardless, but a container with egress (so the agent can
// reach Temporal/object storage) leaves the command with that egress
// unless a container/cluster-level egress policy restricts it
// (RFD 0015 Decision 9/10; Phase 8). EPERM is returned as a typed,
// non-fatal signal so callers can surface the degraded posture rather
// than silently relying on it.
func IsolateNetwork() error {
	if err := syscall.Unshare(syscall.CLONE_NEWNET); err != nil {
		if errors.Is(err, syscall.EPERM) {
			return ErrNetworkIsolationUnsupported
		}
		return err
	}
	return nil
}
