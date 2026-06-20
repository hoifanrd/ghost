package agent

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/testsuite"

	"github.com/zinc-sig/ghost/internal/agent/contract"
)

func TestProtocolMismatch(t *testing.T) {
	cfg := newTestConfig(t)
	env := newActivityEnv(t, cfg, newFakeStore())

	cases := []struct {
		name     string
		activity string
		input    any
	}{
		{"FetchSubmission", contract.FetchSubmissionActivity, contract.FetchSubmissionInput{ProtocolVersion: contract.ProtocolVersion + 1}},
		{"RunExec", contract.RunExecActivity, contract.RunExecInput{ProtocolVersion: contract.ProtocolVersion + 1}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := env.ExecuteActivity(tc.activity, tc.input)
			if err == nil {
				t.Fatal("expected protocol mismatch error, got nil")
			}
			var appErr *temporal.ApplicationError
			if !errors.As(err, &appErr) {
				t.Fatalf("expected *temporal.ApplicationError, got %T: %v", err, err)
			}
			if appErr.Type() != contract.ProtocolMismatchErrorType {
				t.Errorf("error type = %q, want %q", appErr.Type(), contract.ProtocolMismatchErrorType)
			}
			if !appErr.NonRetryable() {
				t.Error("protocol mismatch error must be non-retryable")
			}
		})
	}
}

func TestFetchSubmission(t *testing.T) {
	cfg := newTestConfig(t)
	store := newFakeStore()
	store.objects["course-1"] = map[string][]byte{
		"submissions/42/main.py":     []byte("print(1)\n"),
		"submissions/42/lib/util.py": []byte("util\n"),
		"assets/expected/q1.out":     []byte("ok\n"),
	}
	env := newActivityEnv(t, cfg, store)

	input := contract.FetchSubmissionInput{
		ProtocolVersion: contract.ProtocolVersion,
		Downloads: []contract.DownloadSpec{
			{Bucket: "course-1", Prefix: "submissions/42/", TargetDir: "."},
			{Bucket: "course-1", Prefix: "assets/", TargetDir: "expected"},
		},
	}
	val, err := env.ExecuteActivity(contract.FetchSubmissionActivity, input)
	if err != nil {
		t.Fatalf("FetchSubmission failed: %v", err)
	}
	var res contract.FetchSubmissionResult
	if err := val.Get(&res); err != nil {
		t.Fatalf("failed to decode result: %v", err)
	}

	want := contract.FetchSubmissionResult{
		AgentProtocolVersion: contract.ProtocolVersion,
		AgentVersion:         "test",
		Files:                3,
		Bytes:                int64(len("print(1)\n") + len("util\n") + len("ok\n")),
	}
	if !reflect.DeepEqual(res, want) {
		t.Errorf("result = %+v, want %+v", res, want)
	}

	checks := map[string]string{
		"main.py":                  "print(1)\n",
		"lib/util.py":              "util\n",
		"expected/expected/q1.out": "ok\n",
	}
	for rel, content := range checks {
		data, err := os.ReadFile(filepath.Join(cfg.Workdir, rel))
		if err != nil {
			t.Errorf("expected file %s: %v", rel, err)
			continue
		}
		if string(data) != content {
			t.Errorf("file %s = %q, want %q", rel, data, content)
		}
	}
}

func TestFetchSubmission_TraversalKeyRejected(t *testing.T) {
	cfg := newTestConfig(t)
	store := newFakeStore()
	store.objects["b"] = map[string][]byte{
		"sub/../../escape": []byte("evil"),
	}
	env := newActivityEnv(t, cfg, store)

	input := contract.FetchSubmissionInput{
		ProtocolVersion: contract.ProtocolVersion,
		Downloads:       []contract.DownloadSpec{{Bucket: "b", Prefix: "sub/", TargetDir: "."}},
	}
	if _, err := env.ExecuteActivity(contract.FetchSubmissionActivity, input); err == nil {
		t.Fatal("expected traversal key to be rejected")
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(cfg.Workdir), "escape")); err == nil {
		t.Fatal("traversal file escaped the workspace")
	}
}

func TestFetchSubmission_TargetDirTraversalRejected(t *testing.T) {
	cfg := newTestConfig(t)
	env := newActivityEnv(t, cfg, newFakeStore())

	for _, target := range []string{"../escape", "/abs/escape"} {
		input := contract.FetchSubmissionInput{
			ProtocolVersion: contract.ProtocolVersion,
			Downloads:       []contract.DownloadSpec{{Bucket: "b", Prefix: "p/", TargetDir: target}},
		}
		if _, err := env.ExecuteActivity(contract.FetchSubmissionActivity, input); err == nil {
			t.Errorf("expected target dir %q to be rejected", target)
		}
	}
}

func execRun(t *testing.T, env *testsuite.TestActivityEnvironment, in contract.RunExecInput) contract.ExecResult {
	t.Helper()
	val, err := env.ExecuteActivity(contract.RunExecActivity, in)
	if err != nil {
		t.Fatalf("RunExec failed: %v", err)
	}
	var res contract.ExecResult
	if err := val.Get(&res); err != nil {
		t.Fatalf("failed to decode result: %v", err)
	}
	return res
}

func TestRunExec_HappyPath(t *testing.T) {
	cfg := newTestConfig(t)
	store := newFakeStore()
	env := newActivityEnv(t, cfg, store)

	input := contract.RunExecInput{
		ProtocolVersion: contract.ProtocolVersion,
		Stage:           "test",
		ScenarioCode:    "q1",
		Spec: contract.ExecSpec{
			Command:    "/bin/echo",
			Args:       []string{"hello"},
			StdoutPath: strPtr("out/echo.txt"),
			Workdir:    ".",
		},
		StdioUpload: contract.StdioUploadSpec{Bucket: "runs", KeyPrefix: "55/test/q1"},
	}
	res := execRun(t, env, input)

	if res.Command != "/bin/echo" || !reflect.DeepEqual(res.Args, []string{"hello"}) {
		t.Errorf("command echo = %q %v", res.Command, res.Args)
	}
	if res.ExitCode == nil || *res.ExitCode != 0 {
		t.Fatalf("ExitCode = %v, want 0 (error: %q)", res.ExitCode, res.Error)
	}
	if res.TimedOut {
		t.Error("TimedOut = true, want false")
	}
	if res.Error != "" {
		t.Errorf("Error = %q, want empty", res.Error)
	}

	if data, ok := store.upload("runs", "55/test/q1/stdout"); !ok || string(data) != "hello\n" {
		t.Errorf("uploaded stdout = %q (present=%v), want %q", data, ok, "hello\n")
	}
	if data, ok := store.upload("runs", "55/test/q1/stderr"); !ok || len(data) != 0 {
		t.Errorf("uploaded stderr = %q (present=%v), want empty object", data, ok)
	}
	if want := contract.URIFor("runs", "55/test/q1/stdout"); res.StdoutURI != want {
		t.Errorf("StdoutURI = %q, want %q", res.StdoutURI, want)
	}
	if want := contract.URIFor("runs", "55/test/q1/stderr"); res.StderrURI != want {
		t.Errorf("StderrURI = %q, want %q", res.StderrURI, want)
	}
	if res.StdinURI != "" {
		t.Errorf("StdinURI = %q, want empty (no stdin provided)", res.StdinURI)
	}

	// StdoutPath copy lands relative to the effective workdir.
	data, err := os.ReadFile(filepath.Join(cfg.Workdir, "out/echo.txt"))
	if err != nil {
		t.Fatalf("stdout_path copy missing: %v", err)
	}
	if string(data) != "hello\n" {
		t.Errorf("stdout_path copy = %q, want %q", data, "hello\n")
	}

	if res.StartedAt.IsZero() || res.EndedAt.IsZero() || res.EndedAt.Before(res.StartedAt) {
		t.Errorf("timestamps invalid: started=%v ended=%v", res.StartedAt, res.EndedAt)
	}
	if res.DurationMs < 0 {
		t.Errorf("DurationMs = %d, want >= 0", res.DurationMs)
	}
}

func TestRunExec_NonZeroExit(t *testing.T) {
	cfg := newTestConfig(t)
	store := newFakeStore()
	env := newActivityEnv(t, cfg, store)

	input := contract.RunExecInput{
		ProtocolVersion: contract.ProtocolVersion,
		Spec:            contract.ExecSpec{Command: "/bin/false", Workdir: "."},
		StdioUpload:     contract.StdioUploadSpec{Bucket: "runs", KeyPrefix: "55/test/q2"},
	}
	res := execRun(t, env, input)

	if res.ExitCode == nil || *res.ExitCode != 1 {
		t.Fatalf("ExitCode = %v, want 1", res.ExitCode)
	}
	if res.Error != "" {
		t.Errorf("Error = %q, want empty (non-zero exit is not an error)", res.Error)
	}
	if res.TimedOut {
		t.Error("TimedOut = true, want false")
	}
	if res.StdoutURI == "" || res.StderrURI == "" {
		t.Error("stdout/stderr URIs must be populated even on failure")
	}
}

func TestRunExec_Timeout(t *testing.T) {
	cfg := newTestConfig(t)
	store := newFakeStore()
	env := newActivityEnv(t, cfg, store)

	input := contract.RunExecInput{
		ProtocolVersion: contract.ProtocolVersion,
		Spec: contract.ExecSpec{
			Command:   "/bin/sleep",
			Args:      []string{"5"},
			TimeoutMs: 200,
			Workdir:   ".",
		},
		StdioUpload: contract.StdioUploadSpec{Bucket: "runs", KeyPrefix: "55/test/q3"},
	}
	start := time.Now()
	res := execRun(t, env, input)
	elapsed := time.Since(start)

	if !res.TimedOut {
		t.Fatal("TimedOut = false, want true")
	}
	if elapsed > 4*time.Second {
		t.Errorf("activity took %v; the process group was not killed at the deadline", elapsed)
	}
	if res.ExitCode == nil {
		t.Error("ExitCode = nil, want the kill status from ProcessState")
	}
	if res.StdoutURI == "" || res.StderrURI == "" {
		t.Error("stdout/stderr URIs must be populated on timeout")
	}
}

func TestRunExec_EnvScrub(t *testing.T) {
	// The agent's own credentials must never leak into a student
	// process; Spec.Env must be visible.
	t.Setenv("GHOST_AGENT_TEMPORAL_ADDRESS", "temporal.internal:7233")
	t.Setenv("GHOST_AGENT_STORAGE_SECRET_KEY", "super-secret")

	cfg := newTestConfig(t)
	store := newFakeStore()
	env := newActivityEnv(t, cfg, store)

	input := contract.RunExecInput{
		ProtocolVersion: contract.ProtocolVersion,
		Spec: contract.ExecSpec{
			Command: "/usr/bin/env",
			Env:     map[string]string{"SPEC_VAR": "spec-value"},
			Workdir: ".",
		},
		StdioUpload: contract.StdioUploadSpec{Bucket: "runs", KeyPrefix: "55/test/q4"},
	}
	res := execRun(t, env, input)
	if res.ExitCode == nil || *res.ExitCode != 0 {
		t.Fatalf("ExitCode = %v, want 0 (error: %q)", res.ExitCode, res.Error)
	}

	data, ok := store.upload("runs", "55/test/q4/stdout")
	if !ok {
		t.Fatal("stdout was not uploaded")
	}
	out := string(data)
	if strings.Contains(out, "GHOST_AGENT_") {
		t.Errorf("child environment leaked GHOST_AGENT_* variables:\n%s", out)
	}
	if !strings.Contains(out, "SPEC_VAR=spec-value") {
		t.Errorf("child environment missing Spec.Env entry SPEC_VAR:\n%s", out)
	}
}

func TestRunExec_StdinContent(t *testing.T) {
	cfg := newTestConfig(t)
	store := newFakeStore()
	env := newActivityEnv(t, cfg, store)

	input := contract.RunExecInput{
		ProtocolVersion: contract.ProtocolVersion,
		Spec: contract.ExecSpec{
			Command:      "/bin/cat",
			StdinContent: strPtr("hello agent"),
			Workdir:      ".",
		},
		StdioUpload: contract.StdioUploadSpec{Bucket: "runs", KeyPrefix: "55/test/q5"},
	}
	res := execRun(t, env, input)

	if res.ExitCode == nil || *res.ExitCode != 0 {
		t.Fatalf("ExitCode = %v, want 0 (error: %q)", res.ExitCode, res.Error)
	}
	if data, ok := store.upload("runs", "55/test/q5/stdout"); !ok || string(data) != "hello agent" {
		t.Errorf("uploaded stdout = %q (present=%v), want %q", data, ok, "hello agent")
	}
	if data, ok := store.upload("runs", "55/test/q5/stdin"); !ok || string(data) != "hello agent" {
		t.Errorf("uploaded stdin = %q (present=%v), want %q", data, ok, "hello agent")
	}
	if want := contract.URIFor("runs", "55/test/q5/stdin"); res.StdinURI != want {
		t.Errorf("StdinURI = %q, want %q", res.StdinURI, want)
	}
}

func TestRunExec_StdinPath(t *testing.T) {
	cfg := newTestConfig(t)
	if err := os.WriteFile(filepath.Join(cfg.Workdir, "q.in"), []byte("42\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	store := newFakeStore()
	env := newActivityEnv(t, cfg, store)

	input := contract.RunExecInput{
		ProtocolVersion: contract.ProtocolVersion,
		Spec: contract.ExecSpec{
			Command:   "/bin/cat",
			StdinPath: strPtr("q.in"),
			Workdir:   ".",
		},
		StdioUpload: contract.StdioUploadSpec{Bucket: "runs", KeyPrefix: "55/test/q6"},
	}
	res := execRun(t, env, input)

	if res.ExitCode == nil || *res.ExitCode != 0 {
		t.Fatalf("ExitCode = %v, want 0 (error: %q)", res.ExitCode, res.Error)
	}
	if data, ok := store.upload("runs", "55/test/q6/stdout"); !ok || string(data) != "42\n" {
		t.Errorf("uploaded stdout = %q (present=%v), want %q", data, ok, "42\n")
	}
	if data, ok := store.upload("runs", "55/test/q6/stdin"); !ok || string(data) != "42\n" {
		t.Errorf("uploaded stdin = %q (present=%v), want %q", data, ok, "42\n")
	}
	if want := contract.URIFor("runs", "55/test/q6/stdin"); res.StdinURI != want {
		t.Errorf("StdinURI = %q, want %q", res.StdinURI, want)
	}
}

func TestRunExec_SpawnFailure(t *testing.T) {
	cfg := newTestConfig(t)
	cfg.GhostPath = "/nonexistent/ghost-binary"
	store := newFakeStore()
	env := newActivityEnv(t, cfg, store)

	input := contract.RunExecInput{
		ProtocolVersion: contract.ProtocolVersion,
		Spec:            contract.ExecSpec{Command: "/bin/echo", Args: []string{"hi"}, Workdir: "."},
		StdioUpload:     contract.StdioUploadSpec{Bucket: "runs", KeyPrefix: "55/test/q7"},
	}
	res := execRun(t, env, input)

	if res.ExitCode != nil {
		t.Errorf("ExitCode = %v, want nil for a spawn failure", *res.ExitCode)
	}
	if res.Error == "" {
		t.Error("Error must explain the spawn failure")
	}
	if res.StdoutURI != "" || res.StderrURI != "" || res.StdinURI != "" {
		t.Error("no stdio URIs should be set when the command never spawned")
	}
}

func TestRunExec_WorkdirEscapeRejected(t *testing.T) {
	cfg := newTestConfig(t)
	store := newFakeStore()
	env := newActivityEnv(t, cfg, store)

	input := contract.RunExecInput{
		ProtocolVersion: contract.ProtocolVersion,
		Spec:            contract.ExecSpec{Command: "/bin/echo", Workdir: "../outside"},
		StdioUpload:     contract.StdioUploadSpec{Bucket: "runs", KeyPrefix: "55/test/q8"},
	}
	res := execRun(t, env, input)

	if res.ExitCode != nil {
		t.Errorf("ExitCode = %v, want nil", *res.ExitCode)
	}
	if !strings.Contains(res.Error, "escapes the workspace") {
		t.Errorf("Error = %q, want a workspace-escape rejection", res.Error)
	}
}

func TestRunExec_UploadFailureSurfacesOnError(t *testing.T) {
	cfg := newTestConfig(t)
	store := newFakeStore()
	store.uploadErr = errors.New("storage unreachable")
	env := newActivityEnv(t, cfg, store)

	input := contract.RunExecInput{
		ProtocolVersion: contract.ProtocolVersion,
		Spec:            contract.ExecSpec{Command: "/bin/echo", Args: []string{"hi"}, Workdir: "."},
		StdioUpload:     contract.StdioUploadSpec{Bucket: "runs", KeyPrefix: "55/test/q9"},
	}
	res := execRun(t, env, input)

	if res.ExitCode == nil || *res.ExitCode != 0 {
		t.Fatalf("ExitCode = %v, want 0", res.ExitCode)
	}
	if !strings.Contains(res.Error, "storage unreachable") {
		t.Errorf("Error = %q, want the upload failure", res.Error)
	}
	if res.StdoutURI != "" || res.StderrURI != "" {
		t.Error("failed uploads must not yield URIs")
	}
}

// TestRunExec_Sandboxed exercises the full sandboxed child path
// (Landlock + netns + RLIMIT_NPROC). It needs a kernel with Landlock
// and unprivileged user namespaces, so it only runs when
// GHOST_TEST_SANDBOX=1 is set.
func TestRunExec_Sandboxed(t *testing.T) {
	if os.Getenv("GHOST_TEST_SANDBOX") != "1" {
		t.Skip("set GHOST_TEST_SANDBOX=1 to run the sandboxed child test")
	}

	cfg := newTestConfig(t)
	cfg.Sandbox = true
	cfg.MaxPids = 64
	store := newFakeStore()
	env := newActivityEnv(t, cfg, store)

	input := contract.RunExecInput{
		ProtocolVersion: contract.ProtocolVersion,
		Spec:            contract.ExecSpec{Command: "/bin/echo", Args: []string{"sandboxed"}, Workdir: "."},
		StdioUpload:     contract.StdioUploadSpec{Bucket: "runs", KeyPrefix: "55/test/sandbox"},
	}
	res := execRun(t, env, input)

	if res.ExitCode == nil || *res.ExitCode != 0 {
		stderr, _ := store.upload("runs", "55/test/sandbox/stderr")
		t.Fatalf("ExitCode = %s, want 0 (error: %q, child stderr: %q)", fmtExitCode(res.ExitCode), res.Error, stderr)
	}
	if data, ok := store.upload("runs", "55/test/sandbox/stdout"); !ok || string(data) != "sandboxed\n" {
		t.Errorf("uploaded stdout = %q (present=%v), want %q", data, ok, "sandboxed\n")
	}
}

func fmtExitCode(code *int) string {
	if code == nil {
		return "<nil>"
	}
	return strconv.Itoa(*code)
}
