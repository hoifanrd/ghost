//go:build linux

package cmd

import (
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
)

// denyLinkProfileJSON is a minimal Docker-format seccomp profile (default
// allow) that denies the four link-creation syscalls via SCMP_ACT_ERRNO. It is
// the smallest profile that proves the oracle-rigging fix end-to-end without
// risking the Go runtime under a default-deny filter. The inline-JSON delivery
// matches how core single-sources the profile to ghost.
const denyLinkProfileJSON = `{
  "defaultAction": "SCMP_ACT_ALLOW",
  "architectures": ["SCMP_ARCH_AARCH64", "SCMP_ARCH_X86_64", "SCMP_ARCH_X86"],
  "syscalls": [
    {"names": ["symlink", "symlinkat", "link", "linkat"], "action": "SCMP_ACT_ERRNO"}
  ]
}`

// ghostBinPath holds the path to a freshly built ghost binary, built once per
// process via buildGhostBin. End-to-end seccomp tests need the real CLI surface
// (flag parsing → Config → ApplySeccompFromJSON → filter → child inherits),
// which a unit test cannot reach.
var (
	ghostBinOnce sync.Once
	ghostBinPath string
	ghostBinErr  error
)

// buildGhostBin builds the ghost binary into a temp file once per test process
// and returns its path. Subsequent calls return the cached path. The binary is
// built with CGO enabled (libseccomp-golang needs it).
func buildGhostBin(t *testing.T) string {
	t.Helper()
	ghostBinOnce.Do(func() {
		f, err := os.CreateTemp("", "ghost-seccomp-e2e-*")
		if err != nil {
			ghostBinErr = err
			return
		}
		ghostBinPath = f.Name()
		_ = f.Close()
		// Build the ghost binary from the repo root. The test runs in the cmd
		// package, so resolve the module root via the working directory of the
		// `go build` invocation (go test runs with cwd = package dir; use `go
		// build -o <out> .` from the module root instead).
		cmd := exec.Command("go", "build", "-o", ghostBinPath, ".")
		cmd.Dir = repoRoot(t)
		out, err := cmd.CombinedOutput()
		if err != nil {
			ghostBinErr = err
			t.Logf("go build output:\n%s", out)
		}
	})
	if ghostBinErr != nil {
		t.Fatalf("build ghost binary: %v", ghostBinErr)
	}
	return ghostBinPath
}

// repoRoot returns the module root directory. The cmd package sits at
// <root>/cmd, so the module root is one parent up from the test working dir
// (go test runs with cwd = the package directory).
func repoRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	return filepath.Dir(wd)
}

// runGhostSupervise invokes `ghost supervise --seccomp-profile-json=<profile>
// -i <in> -o <out> -e <err> --result-file=<res> -- <cmd...>` and returns the
// combined stderr+stdout plus the wait error. The seccomp profile is applied to
// ghost (the parent) before fork; the child inherits it across fork/execve.
func runGhostSupervise(t *testing.T, bin, profileJSON string, args ...string) ([]byte, error) {
	t.Helper()
	dir := t.TempDir()
	out := filepath.Join(dir, "stdout")
	errf := filepath.Join(dir, "stderr")
	res := filepath.Join(dir, ".result")
	cmd := exec.Command(bin,
		append([]string{
			"supervise",
			"--seccomp-profile-json=" + profileJSON,
			"-i", "/dev/null",
			"-o", out,
			"-e", errf,
			"--result-file=" + res,
			"--",
		}, args...)...)
	return cmd.CombinedOutput()
}

// TestSeccompEndToEnd_DeniesLinkCreation proves the full path engages seccomp:
// flag → Config.SeccompProfileJSON → ApplySeccompFromJSON → libseccomp filter
// → SetNoNewPrivsBit + Load → child inherits across fork → the symlink syscall
// is denied. The `ln -sf` inside the supervised child must fail, and the
// symlink target must NOT exist.
func TestSeccompEndToEnd_DeniesLinkCreation(t *testing.T) {
	bin := buildGhostBin(t)

	// Target the supervised child will try to create. Lives in a fresh tempdir
	// per run; nothing else touches it.
	dir := t.TempDir()
	linkTarget := filepath.Join(dir, "should-fail")

	// Run a normal command that exits 0 even though ln fails: the seccomp
	// denial returns EPERM, `ln` prints an error, but sh -c '...' exits 0 by
	// default unless we explicitly propagate. To make the assertion robust
	// against shell quirks, we check the FILE existence rather than the exit
	// code — the symlink file must not be created if the syscall was denied.
	out, err := runGhostSupervise(t, bin, denyLinkProfileJSON,
		"sh", "-c", "ln -sf /etc/hostname "+linkTarget+" 2>/dev/null; true")
	if err != nil {
		t.Fatalf("ghost supervise failed unexpectedly\noutput:\n%s\nerr: %v", out, err)
	}

	if _, statErr := os.Lstat(linkTarget); statErr == nil {
		t.Fatalf("symlink was CREATED despite the seccomp deny profile — seccomp did not engage\nghost output:\n%s", out)
	} else if !os.IsNotExist(statErr) {
		t.Fatalf("unexpected stat error on %s: %v", linkTarget, statErr)
	}
	// The file must not exist: the symlink syscall was denied before the link
	// could be created. This is the end-to-end oracle-rigging proof.
}

// TestSeccompEndToEnd_AllowsNormalCommand proves the deny-link profile does not
// break legitimate commands: a plain `echo ok > <file>` inside the supervised
// child must succeed, the file must contain the expected output, and ghost
// supervise itself must exit 0. This guards against an over-broad filter that
// would deny everything (e.g. a default-deny misconfiguration) — such a filter
// would also "deny links" but would be useless for grading.
func TestSeccompEndToEnd_AllowsNormalCommand(t *testing.T) {
	bin := buildGhostBin(t)

	dir := t.TempDir()
	target := filepath.Join(dir, "allowed.txt")

	out, err := runGhostSupervise(t, bin, denyLinkProfileJSON,
		"sh", "-c", "echo ok > "+target)
	if err != nil {
		t.Fatalf("ghost supervise failed on a legitimate command under the seccomp profile\noutput:\n%s\nerr: %v", out, err)
	}

	data, readErr := os.ReadFile(target)
	if readErr != nil {
		t.Fatalf("expected output file %s to exist, got: %v\nghost output:\n%s", target, readErr, out)
	}
	if string(data) != "ok\n" {
		t.Errorf("output file content = %q, want %q\nghost output:\n%s", string(data), "ok\n", out)
	}
}

// TestSeccompEndToEnd_NoProfileIsNoOp confirms that when --seccomp-profile-json
// is NOT passed, ghost supervise runs normally and link creation is allowed.
// This guards the gating (empty field → ApplySeccompFromJSON not called) and
// proves the e2e deny test above only fails because of the profile, not because
// of the host or harness denying links for some other reason.
func TestSeccompEndToEnd_NoProfileIsNoOp(t *testing.T) {
	bin := buildGhostBin(t)

	dir := t.TempDir()
	linkTarget := filepath.Join(dir, "should-succeed")

	out, err := runGhostSupervise(t, bin, "", // no profile
		"sh", "-c", "ln -sf /etc/hostname "+linkTarget)
	if err != nil {
		t.Fatalf("ghost supervise failed without a seccomp profile\noutput:\n%s\nerr: %v", out, err)
	}

	if _, statErr := os.Lstat(linkTarget); statErr != nil {
		t.Fatalf("expected symlink to be created without a seccomp profile, got: %v\nghost output:\n%s", statErr, out)
	}
}
