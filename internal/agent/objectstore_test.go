package agent

import (
	"path/filepath"
	"testing"
)

func TestObjectDestination(t *testing.T) {
	target := t.TempDir()

	ok := []struct {
		name   string
		prefix string
		key    string
		want   string // relative to target
	}{
		{"plain file", "sub/", "sub/main.py", "main.py"},
		{"nested file", "sub/", "sub/lib/util.py", "lib/util.py"},
		{"prefix without trailing slash", "sub", "sub/main.py", "main.py"},
		{"redundant separators collapse", "sub/", "sub/lib//util.py", "lib/util.py"},
		{"internal dotdot that stays inside", "sub/", "sub/lib/../main.py", "main.py"},
	}
	for _, tc := range ok {
		t.Run("ok/"+tc.name, func(t *testing.T) {
			got, err := objectDestination(target, tc.prefix, tc.key)
			if err != nil {
				t.Fatalf("objectDestination(%q, %q) error: %v", tc.prefix, tc.key, err)
			}
			if want := filepath.Join(target, tc.want); got != want {
				t.Errorf("objectDestination(%q, %q) = %q, want %q", tc.prefix, tc.key, got, want)
			}
		})
	}

	bad := []struct {
		name   string
		prefix string
		key    string
	}{
		{"parent escape", "sub/", "sub/../escape"},
		{"deep parent escape", "sub/", "sub/../../etc/passwd"},
		{"absolute after prefix strip", "", "//etc/passwd"},
		{"key equals prefix", "sub/", "sub/"},
		{"empty relative path", "sub", "sub"},
		{"dot only", "sub/", "sub/."},
	}
	for _, tc := range bad {
		t.Run("reject/"+tc.name, func(t *testing.T) {
			if got, err := objectDestination(target, tc.prefix, tc.key); err == nil {
				t.Errorf("objectDestination(%q, %q) = %q, want rejection", tc.prefix, tc.key, got)
			}
		})
	}
}

func TestSecurePathUnder(t *testing.T) {
	root := t.TempDir()
	sub := filepath.Join(root, "stage")

	if got, err := securePathUnder(root, root, "."); err != nil || got != root {
		t.Errorf("securePathUnder(root, root, %q) = %q, %v; want root", ".", got, err)
	}
	if got, err := securePathUnder(root, root, ""); err != nil || got != root {
		t.Errorf("securePathUnder(root, root, %q) = %q, %v; want root", "", got, err)
	}
	if got, err := securePathUnder(root, root, "a/b"); err != nil || got != filepath.Join(root, "a/b") {
		t.Errorf("securePathUnder(root, root, %q) = %q, %v", "a/b", got, err)
	}
	// Relative to a base inside root: ".." back to root is allowed, but
	// not beyond it.
	if got, err := securePathUnder(root, sub, "../shared/out.txt"); err != nil || got != filepath.Join(root, "shared/out.txt") {
		t.Errorf("securePathUnder(root, sub, %q) = %q, %v", "../shared/out.txt", got, err)
	}
	for _, rel := range []string{"..", "../../escape", "/abs/path"} {
		if got, err := securePathUnder(root, root, rel); err == nil {
			t.Errorf("securePathUnder(root, root, %q) = %q, want rejection", rel, got)
		}
	}
	if got, err := securePathUnder(root, sub, "../../escape"); err == nil {
		t.Errorf("securePathUnder(root, sub, %q) = %q, want rejection", "../../escape", got)
	}
}
