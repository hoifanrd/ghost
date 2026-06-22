package runner

import (
	"bytes"
	"testing"
)

func TestCappedWriterUnderCap(t *testing.T) {
	var buf bytes.Buffer
	budget := newOutputBudget(100)
	w := newCappedWriter(&buf, budget)

	n, err := w.Write([]byte("hello"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n != 5 {
		t.Errorf("n = %d, want 5", n)
	}
	if got := buf.String(); got != "hello" {
		t.Errorf("buf = %q, want %q", got, "hello")
	}
	if budget.Truncated() {
		t.Error("truncated set while under cap")
	}
}

func TestCappedWriterCrossingCapTruncates(t *testing.T) {
	var buf bytes.Buffer
	budget := newOutputBudget(4)
	w := newCappedWriter(&buf, budget)

	n, err := w.Write([]byte("abcdefgh"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Reports full length so the child never short-writes.
	if n != 8 {
		t.Errorf("n = %d, want 8 (full reported length)", n)
	}
	if got := buf.String(); got != "abcd" {
		t.Errorf("buf = %q, want %q (capped at remaining)", got, "abcd")
	}
	if !budget.Truncated() {
		t.Error("truncated not set after crossing cap")
	}
}

func TestCappedWriterPostCapDropsButReportsSuccess(t *testing.T) {
	var buf bytes.Buffer
	budget := newOutputBudget(2)
	w := newCappedWriter(&buf, budget)

	_, _ = w.Write([]byte("xy")) // exactly fills the budget
	n, err := w.Write([]byte("more"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n != 4 {
		t.Errorf("n = %d, want 4 (reports success on drop)", n)
	}
	if got := buf.String(); got != "xy" {
		t.Errorf("buf = %q, want %q (nothing past cap)", got, "xy")
	}
	if !budget.Truncated() {
		t.Error("truncated not set after post-cap write")
	}
}

func TestCappedWriterSharedBudgetCapsTotal(t *testing.T) {
	var out, errb bytes.Buffer
	budget := newOutputBudget(10)
	stdout := newCappedWriter(&out, budget)
	stderr := newCappedWriter(&errb, budget)

	// stdout consumes 6 of 10.
	if _, err := stdout.Write([]byte("123456")); err != nil {
		t.Fatal(err)
	}
	// stderr writes 8; only 4 remain in the shared budget.
	n, err := stderr.Write([]byte("ABCDEFGH"))
	if err != nil {
		t.Fatal(err)
	}
	if n != 8 {
		t.Errorf("n = %d, want 8", n)
	}
	if got := errb.String(); got != "ABCD" {
		t.Errorf("stderr buf = %q, want %q", got, "ABCD")
	}
	if total := out.Len() + errb.Len(); total != 10 {
		t.Errorf("total written = %d, want 10 (shared cap)", total)
	}
	if !budget.Truncated() {
		t.Error("truncated not set after total cap exceeded")
	}
}
