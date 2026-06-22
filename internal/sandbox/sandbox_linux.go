//go:build linux

package sandbox

import (
	"fmt"

	"github.com/landlock-lsm/go-landlock/landlock"
	llsyscall "github.com/landlock-lsm/go-landlock/landlock/syscall"
	"golang.org/x/sys/unix"
)

// ApplySandbox applies Landlock filesystem restrictions.
// Read-only: /usr, /bin, /lib, /lib64, /etc (ignored if missing), /proc/self/fd,
// /sys/fs/cgroup (ignored if missing — supervise's peak/OOM sampler reads it).
// Read-write: /output, /tmp, /dev, and the given work directory.
func ApplySandbox(workDir string) error {
	if workDir == "" {
		return fmt.Errorf("sandbox: workDir must not be empty")
	}

	err := landlock.V5.BestEffort().RestrictPaths(
		landlock.RODirs("/usr", "/bin", "/lib", "/lib64", "/etc").IgnoreIfMissing(),
		landlock.RODirs("/proc/self/fd"),
		// /sys/fs/cgroup must stay readable so supervise's in-process sampler and
		// baseline/final reads of memory.current/peak/events succeed after
		// Landlock is applied. RO + IgnoreIfMissing for non-cgroup-v2 hosts.
		landlock.RODirs("/sys/fs/cgroup").IgnoreIfMissing(),
		landlock.RWDirs("/output", "/tmp", "/dev", workDir),
	)
	if err != nil {
		return fmt.Errorf("sandbox: landlock restrict paths: %w", err)
	}
	return nil
}

// LandlockAvailable reports whether the kernel supports Landlock (ABI >= 1).
// Used by tests to decide whether the sandbox actually enforces filesystem
// restrictions on this host; BestEffort no-ops when this is false.
func LandlockAvailable() bool {
	v, err := llsyscall.LandlockGetABIVersion()
	return err == nil && v >= 1
}

// EnforceMaxPids sets RLIMIT_NPROC to limit the total number of processes for
// the current user (UID). The limit counts ALL processes for the UID, including
// the ghost process itself. For example, with maxPids=33, ghost uses 1 slot and
// the student command can create up to 32 processes (including itself).
func EnforceMaxPids(maxPids uint64) error {
	return unix.Setrlimit(unix.RLIMIT_NPROC, &unix.Rlimit{Cur: maxPids, Max: maxPids})
}
