//go:build linux

package runner

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/zinc-sig/ghost/internal/output"
	"github.com/zinc-sig/ghost/internal/sandbox"
)

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
