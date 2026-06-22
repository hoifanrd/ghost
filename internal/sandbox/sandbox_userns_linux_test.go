//go:build linux

package sandbox

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"testing"
)

// TestApplySandboxWithUsernsSetup verifies that the AllowUsernsSetup grant lets
// a Landlocked parent fork a child into a new user namespace — i.e. that Go's
// write of /proc/<child>/{setgroups,uid_map,gid_map} from the (now Landlocked)
// parent is permitted. Without the grant this fork fails with EPERM.
//
// Landlock is per-thread and irreversible, so the actual restriction is applied
// in a re-exec'd child process; the parent inspects its RESULT line. The child
// SKIPs (rather than fails) when the sandbox cannot be applied on this host
// (e.g. /output absent on a dev box) so the test is a no-op off-cluster.
func TestApplySandboxWithUsernsSetup(t *testing.T) {
	if os.Getenv("GHOST_TEST_SANDBOX_USERNS_CHILD") == "1" {
		sandboxUsernsChild()
		return
	}
	if !LandlockAvailable() {
		t.Skip("Landlock not available (ABI < 1): BestEffort no-ops, grant is not exercised")
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestApplySandboxWithUsernsSetup$", "-test.v")
	cmd.Env = append(os.Environ(), "GHOST_TEST_SANDBOX_USERNS_CHILD=1")
	out, err := cmd.CombinedOutput()
	s := string(out)
	switch {
	case strings.Contains(s, "RESULT:OK"):
		// Fix confirmed: Landlocked parent created the userns child.
	case strings.Contains(s, "RESULT:SKIP"):
		t.Skipf("child could not exercise the grant: %s", resultLine(s))
	default:
		t.Fatalf("userns child did not start under Landlock (err=%v):\n%s", err, s)
	}
}

func resultLine(out string) string {
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "RESULT:") {
			return line
		}
	}
	return "(no RESULT line)"
}

// sandboxUsernsChild runs in the re-exec'd subprocess: it applies the sandbox
// with the userns grant, then forks a CLONE_NEWUSER|CLONE_NEWNET child and
// prints a single RESULT line for the parent to parse.
func sandboxUsernsChild() {
	truePath, err := exec.LookPath("true")
	if err != nil {
		fmt.Println("RESULT:SKIP lookpath true:", err)
		return
	}
	dir, err := os.MkdirTemp("", "sbx-userns-")
	if err != nil {
		fmt.Println("RESULT:SKIP mktemp:", err)
		return
	}
	defer func() { _ = os.RemoveAll(dir) }()

	if err := ApplySandboxWith(dir, SandboxOpts{AllowUsernsSetup: true}); err != nil {
		// e.g. /output missing on a dev host, or kernel too old. Not the bug.
		fmt.Println("RESULT:SKIP applysandbox:", err)
		return
	}

	cmd := exec.Command(truePath)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Cloneflags:  syscall.CLONE_NEWUSER | syscall.CLONE_NEWNET,
		UidMappings: []syscall.SysProcIDMap{{ContainerID: 0, HostID: os.Getuid(), Size: 1}},
		GidMappings: []syscall.SysProcIDMap{{ContainerID: 0, HostID: os.Getgid(), Size: 1}},
	}
	if err := cmd.Start(); err != nil {
		fmt.Println("RESULT:FAIL start:", err)
		return
	}
	if err := cmd.Wait(); err != nil {
		fmt.Println("RESULT:FAIL wait:", err)
		return
	}
	fmt.Println("RESULT:OK")
}
