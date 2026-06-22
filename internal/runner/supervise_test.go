//go:build linux

package runner

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/zinc-sig/ghost/internal/output"
	"github.com/zinc-sig/ghost/internal/sandbox"
)

// requireUnprivilegedUserns skips a test when this host cannot create an
// unprivileged user namespace (the mechanism --isolate-network relies on in
// supervise mode).
func requireUnprivilegedUserns(t *testing.T) {
	t.Helper()
	probe := exec.Command("true")
	probe.SysProcAttr = &syscall.SysProcAttr{
		Cloneflags:  syscall.CLONE_NEWUSER,
		UidMappings: []syscall.SysProcIDMap{{ContainerID: 0, HostID: os.Getuid(), Size: 1}},
		GidMappings: []syscall.SysProcIDMap{{ContainerID: 0, HostID: os.Getgid(), Size: 1}},
	}
	if err := probe.Start(); err != nil {
		t.Skipf("unprivileged user namespaces unavailable: %v", err)
	}
	_ = probe.Wait()
}

// TestSuperviseNetworkIsolationUserns exercises the supervise userns netns path
// end to end (no Landlock): the child must start, be waited on correctly, and
// produce its output. Guards superviseSysProcAttr / the CLONE_NEWUSER fork.
func TestSuperviseNetworkIsolationUserns(t *testing.T) {
	requireUnprivilegedUserns(t)
	dir := t.TempDir()
	cfg := superviseConfig(dir, "sh", "-c", "echo ok")
	cfg.IsolateNetwork = true
	if err := Supervise(cfg); err != nil {
		t.Fatalf("Supervise with IsolateNetwork: %v", err)
	}
	tr := decodeResultFile(t, cfg.ResultFile)
	if tr.ExitCode != 0 {
		t.Fatalf("exit_code = %d, want 0 (userns child must start and exit cleanly)", tr.ExitCode)
	}
	assertFileContains(t, cfg.OutputFile, "ok\n")
}

// TestSuperviseSandboxedNetworkIsolation is the regression guard for the
// Landlock+userns interaction: Landlock is applied to the supervising parent
// before it forks a child into a NEW user namespace, and Go writes the child's
// /proc/<pid>/{setgroups,uid_map,gid_map} from that (now Landlocked) parent.
// Without the AllowUsernsSetup grant those writes are denied and cmd.Start
// fails — so combining --landlock with --isolate-network would break entirely.
// Gated to SKIP unless Landlock enforces, unprivileged userns works, and
// /output exists (the base sandbox RWDirs requires it).
func TestSuperviseSandboxedNetworkIsolation(t *testing.T) {
	if !sandbox.LandlockAvailable() {
		t.Skip("Landlock not available (ABI < 1): BestEffort no-ops, so the Landlock+userns interaction is not exercised")
	}
	requireUnprivilegedUserns(t)
	if _, err := os.Stat("/output"); err != nil {
		t.Skip("/output not present: base sandbox RWDirs(/output) cannot be applied on this host")
	}
	dir := t.TempDir()
	cfg := superviseConfig(dir, "sh", "-c", "echo ok")
	cfg.Landlock = true
	cfg.IsolateNetwork = true
	cfg.SandboxWorkDir = dir
	if err := Supervise(cfg); err != nil {
		t.Fatalf("Supervise with Landlock+IsolateNetwork: %v", err)
	}
	tr := decodeResultFile(t, cfg.ResultFile)
	if tr.ExitCode != 0 {
		t.Fatalf("exit_code = %d, want 0 (child must start under Landlock+userns)", tr.ExitCode)
	}
	assertFileContains(t, cfg.OutputFile, "ok\n")
}

// decodeResultFile reads and JSON-decodes the supervise result file (the
// primary Docker transport — plain JSON, no frame sentinels).
func decodeResultFile(t *testing.T, path string) output.Trailer {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read result file: %v", err)
	}
	var tr output.Trailer
	if err := json.Unmarshal(data, &tr); err != nil {
		t.Fatalf("decode trailer: %v", err)
	}
	return tr
}

func superviseConfig(dir string, cmd string, args ...string) *Config {
	return &Config{
		Command:        cmd,
		Args:           args,
		InputFile:      "/dev/null",
		OutputFile:     filepath.Join(dir, "stdout"),
		StderrFile:     filepath.Join(dir, "stderr"),
		Supervise:      true,
		MaxOutputBytes: 1 << 20,
		ResultFile:     filepath.Join(dir, ".result"),
	}
}

func TestSuperviseHappyPath(t *testing.T) {
	dir := t.TempDir()
	cfg := superviseConfig(dir, "sh", "-c", "echo hello; sleep 0.1")
	if err := Supervise(cfg); err != nil {
		t.Fatalf("Supervise: %v", err)
	}

	tr := decodeResultFile(t, cfg.ResultFile)
	if tr.Schema != output.TrailerSchema {
		t.Errorf("schema = %d, want %d", tr.Schema, output.TrailerSchema)
	}
	if tr.ExitCode != 0 {
		t.Errorf("exit_code = %d, want 0", tr.ExitCode)
	}
	if tr.OOMKilled {
		t.Error("oom_killed = true on happy path")
	}
	if tr.Truncated {
		t.Error("truncated = true on small output")
	}
	if tr.DurationMs < 90 {
		t.Errorf("duration_ms = %d, want >= ~100 (child sleeps 0.1s)", tr.DurationMs)
	}
	if sandbox.CgroupV2Available() && tr.PeakMemoryB <= 0 {
		t.Errorf("peak_memory_bytes = %d, want > 0 on a cgroup-v2 host", tr.PeakMemoryB)
	}
	assertFileContains(t, cfg.OutputFile, "hello\n")
}

func TestSuperviseExitCode(t *testing.T) {
	dir := t.TempDir()
	cfg := superviseConfig(dir, "sh", "-c", "exit 7")
	if err := Supervise(cfg); err != nil {
		t.Fatalf("Supervise: %v", err)
	}
	tr := decodeResultFile(t, cfg.ResultFile)
	if tr.ExitCode != 7 {
		t.Errorf("exit_code = %d, want 7", tr.ExitCode)
	}
}

func TestSuperviseTruncatesOutput(t *testing.T) {
	dir := t.TempDir()
	cfg := superviseConfig(dir, "sh", "-c", "head -c 100 /dev/zero | tr '\\0' 'a'")
	cfg.MaxOutputBytes = 10
	if err := Supervise(cfg); err != nil {
		t.Fatalf("Supervise: %v", err)
	}
	tr := decodeResultFile(t, cfg.ResultFile)
	if !tr.Truncated {
		t.Error("truncated = false, want true (output exceeded cap)")
	}
	data, err := os.ReadFile(cfg.OutputFile)
	if err != nil {
		t.Fatal(err)
	}
	if int64(len(data)) > cfg.MaxOutputBytes {
		t.Errorf("output file %d bytes, want <= cap %d", len(data), cfg.MaxOutputBytes)
	}
}

func TestResolvePeak(t *testing.T) {
	tests := []struct {
		name      string
		baseline  int64
		watermark int64
		sampled   int64
		want      int64
	}{
		{"watermark above baseline wins", 100, 150, 120, 150},
		{"watermark equal to baseline falls back to sampled", 100, 100, 120, 120},
		{"watermark below baseline falls back to sampled", 100, 90, 130, 130},
		{"zero watermark falls back to sampled", 0, 0, 64, 64},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := resolvePeak(tt.baseline, tt.watermark, tt.sampled); got != tt.want {
				t.Errorf("resolvePeak(%d, %d, %d) = %d, want %d",
					tt.baseline, tt.watermark, tt.sampled, got, tt.want)
			}
		})
	}
}

// runForExitErr runs a small command and returns the wait error so the test can
// exercise exitCodeFor against a real *exec.ExitError / syscall.WaitStatus.
func runForExitErr(t *testing.T, name string, args ...string) error {
	t.Helper()
	cmd := exec.Command(name, args...)
	return cmd.Run()
}

func TestExitCodeFor(t *testing.T) {
	// timedOut short-circuits to -1 regardless of waitErr.
	if got := exitCodeFor(nil, true); got != -1 {
		t.Errorf("exitCodeFor(nil, timedOut=true) = %d, want -1", got)
	}
	if got := exitCodeFor(errors.New("anything"), true); got != -1 {
		t.Errorf("exitCodeFor(err, timedOut=true) = %d, want -1", got)
	}

	// Normal nil error => 0.
	if got := exitCodeFor(nil, false); got != 0 {
		t.Errorf("exitCodeFor(nil, false) = %d, want 0", got)
	}

	// Normal non-zero exit => the wait-status exit code is preserved.
	err := runForExitErr(t, "sh", "-c", "exit 7")
	if got := exitCodeFor(err, false); got != 7 {
		t.Errorf("exitCodeFor(exit-7, false) = %d, want 7", got)
	}

	// Signalled-but-not-timeout (SIGSEGV) => -1.
	err = runForExitErr(t, "sh", "-c", "kill -SEGV $$")
	if got := exitCodeFor(err, false); got != -1 {
		t.Errorf("exitCodeFor(SIGSEGV, false) = %d, want -1", got)
	}

	// Non-*exec.ExitError wait failure => -1 (abnormal).
	if got := exitCodeFor(fmt.Errorf("wait failed: %w", os.ErrClosed), false); got != -1 {
		t.Errorf("exitCodeFor(non-ExitError, false) = %d, want -1", got)
	}
}

func TestSuperviseTimeout(t *testing.T) {
	dir := t.TempDir()
	cfg := superviseConfig(dir, "sleep", "5")
	cfg.Timeout = 200 * time.Millisecond
	if err := Supervise(cfg); err != nil {
		t.Fatalf("Supervise: %v", err)
	}
	tr := decodeResultFile(t, cfg.ResultFile)
	if tr.ExitCode != -1 {
		t.Errorf("exit_code = %d, want -1 on timeout", tr.ExitCode)
	}
}
