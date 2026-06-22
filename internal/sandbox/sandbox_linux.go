//go:build linux

package sandbox

import (
	"fmt"

	"github.com/landlock-lsm/go-landlock/landlock"
	llsyscall "github.com/landlock-lsm/go-landlock/landlock/syscall"
	"golang.org/x/sys/unix"
)

// SandboxOpts enables Landlock grants beyond the base ruleset. They are opt-in
// because they widen the sandbox: the base posture (exec mode) needs neither,
// while supervise needs one or both depending on how it measures and isolates.
type SandboxOpts struct {
	// AllowCgroupRead grants RO access to /sys/fs/cgroup so supervise's
	// in-process sampler and baseline/final reads of memory.current/peak/events
	// succeed after Landlock is applied. exec mode does not read cgroup files,
	// so it leaves this off and keeps /sys/fs/cgroup unreadable to the child.
	AllowCgroupRead bool

	// AllowUsernsSetup grants the narrow /proc file access the parent needs to
	// write a forked child's setgroups/uid_map/gid_map when isolating the
	// network via a new user namespace AFTER Landlock is in force. Go's runtime
	// performs those writes from the (now Landlocked) parent; without this grant
	// the kernel denies them and the userns child fails to start. The grant is
	// RWFiles (read+write existing files, no dir create/delete) and is
	// DAC-bounded, so the inheriting child gains no meaningful new capability.
	AllowUsernsSetup bool
}

// ApplySandbox applies the base Landlock filesystem restrictions.
// Read-only: /usr, /bin, /lib, /lib64, /etc (ignored if missing), /proc/self/fd.
// Read-write: /output, /tmp, /dev, and the given work directory.
func ApplySandbox(workDir string) error {
	return ApplySandboxWith(workDir, SandboxOpts{})
}

// ApplySandboxWith applies the base Landlock restrictions (see ApplySandbox)
// plus any grants enabled in opts. Supervise uses it to additionally read
// cgroup memory files and, when isolating the network, to set up the child's
// user namespace after Landlock has been applied to the supervising parent.
func ApplySandboxWith(workDir string, opts SandboxOpts) error {
	if workDir == "" {
		return fmt.Errorf("sandbox: workDir must not be empty")
	}

	rules := []landlock.Rule{
		landlock.RODirs("/usr", "/bin", "/lib", "/lib64", "/etc").IgnoreIfMissing(),
		landlock.RODirs("/proc/self/fd"),
		landlock.RWDirs("/output", "/tmp", "/dev", workDir),
	}
	if opts.AllowCgroupRead {
		// RO + IgnoreIfMissing for non-cgroup-v2 hosts.
		rules = append(rules, landlock.RODirs("/sys/fs/cgroup").IgnoreIfMissing())
	}
	if opts.AllowUsernsSetup {
		// The parent writes /proc/<child>/{setgroups,uid_map,gid_map}; Go opens
		// these existing files read+write, so WRITE_FILE alone is insufficient.
		rules = append(rules, landlock.RWFiles("/proc"))
	}

	if err := landlock.V5.BestEffort().RestrictPaths(rules...); err != nil {
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
