//go:build linux

package sandbox

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// denyLinkProfile is a minimal profile (default allow) that denies the four
// link-creation syscalls — the same shape core's full allowlist enforces by
// omission. Used to test the apply mechanism without risking the Go runtime
// under a default-deny filter.
const denyLinkProfile = `{
  "defaultAction": "SCMP_ACT_ALLOW",
  "architectures": ["SCMP_ARCH_AARCH64", "SCMP_ARCH_X86_64", "SCMP_ARCH_X86"],
  "syscalls": [
    {"names": ["symlink", "symlinkat", "link", "linkat"], "action": "SCMP_ACT_ERRNO"}
  ]
}`

const (
	seccompChildEnv = "GHOST_SECCOMP_TEST_CHILD"
	seccompJSONEnv  = "GHOST_SECCOMP_TEST_JSON"
)

// TestApplySeccompFromJSON_DeniesLink passes the profile inline as JSON (the
// single-source delivery path core uses), applies it in a forked child so the
// filter never touches the parent test runtime, attempts a symlink, and asserts
// it is denied.
func TestApplySeccompFromJSON_DeniesLink(t *testing.T) {
	if os.Getenv(seccompChildEnv) == "1" {
		// CHILD: apply the inline profile, attempt a symlink, report via exit.
		if err := ApplySeccompFromJSON([]byte(os.Getenv(seccompJSONEnv))); err != nil {
			t.Fatalf("ApplySeccompFromJSON: %v", err)
		}
		target := filepath.Join(t.TempDir(), "symlink-target")
		if err := os.Symlink("/etc/hostname", target); err != nil {
			os.Exit(0) // denied — expected.
		}
		os.Exit(1) // not denied — the filter failed.
	}

	// PARENT: pass the JSON inline via env (no file), fork the child.
	cmd := exec.Command(os.Args[0], "-test.run=^TestApplySeccompFromJSON_DeniesLink$")
	cmd.Env = append(os.Environ(),
		seccompChildEnv+"=1",
		seccompJSONEnv+"="+denyLinkProfile,
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("link was NOT denied after applying the seccomp profile (child did not exit 0)\nchild output:\n%s\nerr: %v", out, err)
	}
}

// TestBuildSeccompFilter_DefaultDenyWithConditional validates the parser
// handles core's real profile shape: default-deny (ERRNO + defaultErrnoRet),
// an allow list, and a conditional entry (the clone CLONE_NEW* mask). It only
// builds the filter (no Load), so it is safe to run in-process.
func TestBuildSeccompFilter_DefaultDenyWithConditional(t *testing.T) {
	errno := uint(38) // ENOSYS, matching core's defaultErrnoRet.
	profile := &seccompProfile{
		DefaultAction:   "SCMP_ACT_ERRNO",
		DefaultErrnoRet: &errno,
		Architectures:   []string{"SCMP_ARCH_AARCH64", "SCMP_ARCH_X86_64", "SCMP_ARCH_X86"},
		Syscalls: []seccompSyscall{
			{Names: []string{"read", "write", "exit", "exit_group", "rt_sigreturn"}, Action: "SCMP_ACT_ALLOW"},
			{Names: []string{"clone"}, Action: "SCMP_ACT_ALLOW", Args: []seccompArg{
				{Index: 0, Value: 0x7E020000, ValueTwo: 0, Op: "SCMP_CMP_MASKED_EQ"},
			}},
		},
	}
	filter, err := buildSeccompFilter(profile)
	if err != nil {
		t.Fatalf("buildSeccompFilter failed on a core-shaped profile: %v", err)
	}
	filter.Release()
}

// TestApplySeccompFromJSON_EmptyIsNoOp confirms the gating: empty input means
// no filter (backward-compatible when the flag isn't passed).
func TestApplySeccompFromJSON_EmptyIsNoOp(t *testing.T) {
	if err := ApplySeccompFromJSON(nil); err != nil {
		t.Fatalf("empty input should be a no-op, got: %v", err)
	}
}
