//go:build linux

package runner

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/zinc-sig/ghost/internal/output"
	"github.com/zinc-sig/ghost/internal/sandbox"
)

// peakSampleInterval matches core's monitor sampling cadence. In-process here,
// it has less timing jitter than host-side polling.
const peakSampleInterval = 25 * time.Millisecond

// gracefulShutdownDelay is the grace period between SIGTERM and SIGKILL on
// supervise timeout (ported from zinc's executor).
const gracefulShutdownDelay = 5 * time.Second

// Supervise forks the target command, measures it from a live parent (peak
// memory, OOM attribution, output-size cap), and emits a result trailer to the
// result file and as a stream frame on ghost's own stdout. Unlike ExecuteExec,
// ghost is not replaced — it survives the child to measure and report.
func Supervise(config *Config) error {
	inputFile, err := os.Open(config.InputFile)
	if err != nil {
		return fmt.Errorf("supervise: failed to open input file %s: %w", config.InputFile, err)
	}
	defer func() { _ = inputFile.Close() }()

	outFile, err := createFileWithDir(config.OutputFile)
	if err != nil {
		return fmt.Errorf("supervise: failed to create output file: %w", err)
	}
	defer func() { _ = outFile.Close() }()

	errFile, err := createFileWithDir(config.StderrFile)
	if err != nil {
		return fmt.Errorf("supervise: failed to create stderr file: %w", err)
	}
	defer func() { _ = errFile.Close() }()

	maxOutput := config.MaxOutputBytes
	if maxOutput <= 0 {
		maxOutput = 1 << 20 // 1 MiB default, matches core's maxOutputDirBytes
	}
	budget := newOutputBudget(maxOutput)

	// fork+wait via os/exec (NOT dup3/execve): ghost must survive the child.
	var ctx context.Context
	var cancel context.CancelFunc
	var cmd *exec.Cmd
	if config.Timeout > 0 {
		ctx, cancel = context.WithTimeout(context.Background(), config.Timeout)
		defer cancel()
		cmd = exec.CommandContext(ctx, config.Command, config.Args...)
	} else {
		cmd = exec.Command(config.Command, config.Args...)
	}
	cmd.Stdin = inputFile
	cmd.Stdout = newCappedWriter(outFile, budget)
	cmd.Stderr = newCappedWriter(errFile, budget)

	// Child isolation: Landlock and the user+network namespace are now
	// orthogonal (§7). Setpgid lets the timeout escalation signal the whole
	// process group.
	if config.Landlock {
		// Apply Landlock to the parent before fork; it is inherited by the
		// child. /output stays RW (RWDirs includes it), so the trailer write
		// below is permitted.
		if err := sandbox.ApplySandbox(config.SandboxWorkDir); err != nil {
			return fmt.Errorf("supervise: %w", err)
		}
	}
	if config.IsolateNetwork {
		// userns netns: CLONE_NEWUSER|CLONE_NEWNET. Only created when network
		// isolation is requested — without it ghost never calls
		// clone(CLONE_NEWUSER), which the sandbox seccomp profile blocks.
		cmd.SysProcAttr = superviseSysProcAttr()
	} else {
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	}

	// §12.7: supervise enforces its own timeout as a SIGTERM→delay→SIGKILL
	// escalation on the child's process group; core remains the backstop.
	// cmd.Cancel may only be set when the command was created with a context
	// (i.e. a timeout was configured).
	var timedOut atomic.Bool
	if ctx != nil {
		cmd.Cancel = func() error {
			timedOut.Store(true)
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
			go func() {
				time.Sleep(gracefulShutdownDelay)
				_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
			}()
			return nil
		}
		cmd.WaitDelay = gracefulShutdownDelay + time.Second
	}

	// §12.3 / §7: set RLIMIT_NPROC in the parent before fork; it is inherited
	// by the child. Goroutines are threads of one process and consume no PID
	// slots, so ghost = 1 slot and core's +1 reserve holds.
	if config.MaxPids > 0 {
		if err := sandbox.EnforceMaxPids(config.MaxPids); err != nil {
			return fmt.Errorf("supervise: failed to enforce max pids: %w", err)
		}
	}

	// OOM + peak baseline snapshot immediately before launch.
	cgroupOK := sandbox.CgroupV2Available()
	if !cgroupOK {
		// §12 resolved: cgroup v1/non-v2 is a dev-host degradation only (RFD
		// scopes cluster backends to cgroup-v2). Emit peak 0, do not fail.
		fmt.Fprintln(os.Stderr, "ghost supervise: peak memory sampling unavailable: cgroup v2 not detected")
	}
	oomBefore := sandbox.ReadOOMKillCount()
	baselinePeak, _ := sandbox.ReadMemoryPeak()

	sampler := newPeakSampler()
	if cgroupOK {
		sampler.start()
	}

	startTime := time.Now()
	if err := cmd.Start(); err != nil {
		sampler.stop()
		// §12.4 resolved: hard-fail with a clear error if userns creation
		// EPERMs — no silent fall back to bare CLONE_NEWNET (network leak).
		if config.IsolateNetwork && errors.Is(err, syscall.EPERM) {
			return fmt.Errorf("supervise: failed to create user namespace for network isolation "+
				"(unprivileged user namespaces may be disabled on this host): %w", err)
		}
		return fmt.Errorf("supervise: failed to start command: %w", err)
	}

	waitErr := cmd.Wait()
	duration := time.Since(startTime).Milliseconds()

	sampled := sampler.stop()
	watermark, _ := sandbox.ReadMemoryPeak()
	oomAfter := sandbox.ReadOOMKillCount()

	var peak int64
	if cgroupOK {
		peak = resolvePeak(baselinePeak, watermark, sampled)
	}

	trailer := output.Trailer{
		Schema:      output.TrailerSchema,
		ExitCode:    exitCodeFor(waitErr, timedOut.Load()),
		PeakMemoryB: peak,
		OOMKilled:   oomAfter > oomBefore,
		Truncated:   budget.Truncated(),
		DurationMs:  duration,
	}

	return writeTrailer(config.ResultFile, trailer)
}

// exitCodeFor maps the wait result to the trailer exit code: the wait-status
// exit code on a normal exit; -1 for a timeout or any signalled exit (including
// an OOM SIGKILL — oom_killed is the authoritative OOM signal, emitted
// independently). Matches core's convention.
func exitCodeFor(waitErr error, timedOut bool) int {
	if timedOut {
		return -1
	}
	if waitErr == nil {
		return 0
	}
	var exitErr *exec.ExitError
	if errors.As(waitErr, &exitErr) {
		if status, ok := exitErr.Sys().(syscall.WaitStatus); ok {
			if status.Signaled() {
				return -1
			}
			return status.ExitStatus()
		}
		return 1
	}
	// Failed to wait / non-ExitError: treat as signalled/abnormal.
	return -1
}

// writeTrailer writes the trailer to the result file (primary Docker transport,
// read directly off the bind mount) and as a single stream frame on ghost's own
// stdout (for cluster backends that read the exec stream). The frame is emitted
// unconditionally so it exists the moment a driver reads it; Docker ignores
// ghost's stdout and reads the result file.
func writeTrailer(resultFile string, t output.Trailer) error {
	data, err := json.Marshal(t)
	if err != nil {
		return fmt.Errorf("supervise: marshal trailer: %w", err)
	}
	if resultFile != "" {
		if err := os.WriteFile(resultFile, data, 0644); err != nil {
			return fmt.Errorf("supervise: write result file %s: %w", resultFile, err)
		}
	}
	frame, err := output.EncodeFrame(t)
	if err != nil {
		return fmt.Errorf("supervise: encode result frame: %w", err)
	}
	if _, err := os.Stdout.Write(frame); err != nil {
		return fmt.Errorf("supervise: write result frame: %w", err)
	}
	return nil
}

// superviseSysProcAttr returns SysProcAttr creating a new user + network
// namespace for the child. CLONE_NEWUSER lets an unprivileged process create
// the netns (the new userns grants CAP_SYS_ADMIN within itself), giving the
// child loopback-only networking even without container CAP_SYS_ADMIN.
// UID/GID 0→current mapping preserves file ownership at the executor's UID.
// Adopted from zinc's executor (sandboxSysProcAttr).
func superviseSysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{
		Setpgid:    true,
		Cloneflags: syscall.CLONE_NEWUSER | syscall.CLONE_NEWNET,
		UidMappings: []syscall.SysProcIDMap{
			{ContainerID: 0, HostID: os.Getuid(), Size: 1},
		},
		GidMappings: []syscall.SysProcIDMap{
			{ContainerID: 0, HostID: os.Getgid(), Size: 1},
		},
	}
}

// resolvePeak picks the per-execution peak in sampling mode (ported verbatim
// from core's monitor). /sys/fs/cgroup is read-only inside the container so
// ghost can never reset memory.peak: a watermark risen above the baseline was
// set during this exec and is exact; otherwise the sampled maximum is the best
// per-exec estimate (never an over-report).
func resolvePeak(baseline, watermark, sampled int64) int64 {
	if watermark > baseline {
		return watermark
	}
	return sampled
}

// peakSampler tracks the maximum observed memory.current while the child runs.
type peakSampler struct {
	mu      sync.Mutex
	stopCh  chan struct{}
	doneCh  chan struct{}
	max     atomic.Int64
	started bool
}

func newPeakSampler() *peakSampler {
	return &peakSampler{
		stopCh: make(chan struct{}),
		doneCh: make(chan struct{}),
	}
}

func (s *peakSampler) observe() {
	v, ok := sandbox.ReadMemoryCurrent()
	if !ok {
		return
	}
	for {
		cur := s.max.Load()
		if v <= cur || s.max.CompareAndSwap(cur, v) {
			return
		}
	}
}

func (s *peakSampler) start() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.started {
		return
	}
	s.started = true
	go func() {
		defer close(s.doneCh)
		ticker := time.NewTicker(peakSampleInterval)
		defer ticker.Stop()
		s.observe()
		for {
			select {
			case <-s.stopCh:
				s.observe()
				return
			case <-ticker.C:
				s.observe()
			}
		}
	}()
}

func (s *peakSampler) stop() int64 {
	s.mu.Lock()
	started := s.started
	s.started = false
	s.mu.Unlock()
	if !started {
		return s.max.Load()
	}
	close(s.stopCh)
	<-s.doneCh
	return s.max.Load()
}
