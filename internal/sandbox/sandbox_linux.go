//go:build linux

package sandbox

import (
	"fmt"

	"github.com/landlock-lsm/go-landlock/landlock"
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
