//go:build linux

package sandbox

import (
	"fmt"

	"github.com/landlock-lsm/go-landlock/landlock"
	"golang.org/x/sys/unix"
)

// ApplySandbox applies Landlock filesystem restrictions.
// Read-only: /usr, /bin, /lib, /lib64, /etc (ignored if missing).
// Read-write: /output, /tmp, and the given workDir.
func ApplySandbox(workDir string) error {
	if workDir == "" {
		return fmt.Errorf("sandbox: workDir must not be empty")
	}

	err := landlock.V5.BestEffort().RestrictPaths(
		landlock.RODirs("/usr", "/bin", "/lib", "/lib64", "/etc").IgnoreIfMissing(),
		landlock.RWDirs("/output", "/tmp", workDir),
	)
	if err != nil {
		return fmt.Errorf("sandbox: landlock restrict paths: %w", err)
	}
	return nil
}

// EnforceMaxPids sets RLIMIT_NPROC to limit the total number of processes for
// the current user (UID). The limit counts ALL processes for the UID, including
// the ghost process itself. For example, with maxPids=33, ghost uses 1 slot and
// the student command can create up to 32 processes (including itself).
func EnforceMaxPids(maxPids uint64) error {
	return unix.Setrlimit(unix.RLIMIT_NPROC, &unix.Rlimit{Cur: maxPids, Max: maxPids})
}
