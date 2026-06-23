package output

import (
	"encoding/json"
	"testing"
)

func TestTrailerJSONKeys(t *testing.T) {
	tr := Trailer{
		Schema:      TrailerSchema,
		ExitCode:    0,
		PeakMemoryB: 12345,
		OOMKilled:   false,
		Truncated:   true,
		DurationMs:  250,
	}
	data, err := json.Marshal(tr)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("unmarshal to map: %v", err)
	}
	for _, key := range []string{"schema", "exit_code", "peak_memory_bytes", "oom_killed", "truncated", "duration_ms"} {
		if _, ok := m[key]; !ok {
			t.Errorf("missing frozen key %q in %s", key, data)
		}
	}
	if len(m) != 6 {
		t.Errorf("unexpected key count %d (want 6): %s", len(m), data)
	}
}

func TestTrailerRoundTrip(t *testing.T) {
	want := Trailer{Schema: 1, ExitCode: 42, PeakMemoryB: 999, OOMKilled: true, Truncated: false, DurationMs: 77}
	data, err := json.Marshal(want)
	if err != nil {
		t.Fatal(err)
	}
	var got Trailer
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Errorf("round-trip mismatch\ngot:  %+v\nwant: %+v", got, want)
	}
}

func TestEncodeDecodeFrame(t *testing.T) {
	want := Trailer{Schema: 1, ExitCode: 0, PeakMemoryB: 5000, OOMKilled: false, Truncated: true, DurationMs: 100}
	frame, err := EncodeFrame(want)
	if err != nil {
		t.Fatal(err)
	}
	if frame[len(frame)-1] != '\n' {
		t.Error("frame must end with newline")
	}
	got, err := DecodeFrame(frame)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got != want {
		t.Errorf("frame round-trip mismatch\ngot:  %+v\nwant: %+v", got, want)
	}
}

func TestDecodeFrameAmidOutput(t *testing.T) {
	want := Trailer{Schema: 1, ExitCode: 1, PeakMemoryB: 1, OOMKilled: false, Truncated: false, DurationMs: 1}
	frame, err := EncodeFrame(want)
	if err != nil {
		t.Fatal(err)
	}
	// Surround with child-like output, including a lone RS byte that must not
	// false-match the double-RS + token sentinel.
	noise := append([]byte("regular output\x1emore output\n"), frame...)
	noise = append(noise, []byte("trailing line\n")...)
	got, err := DecodeFrame(noise)
	if err != nil {
		t.Fatalf("decode amid output: %v", err)
	}
	if got != want {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestDecodeFrameNoFrame(t *testing.T) {
	if _, err := DecodeFrame([]byte("just some output\x1ewith a lone RS")); err != ErrNoFrame {
		t.Errorf("err = %v, want ErrNoFrame", err)
	}
}
