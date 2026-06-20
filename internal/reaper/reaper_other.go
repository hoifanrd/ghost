//go:build !linux

package reaper

import "syscall"

// Start is a no-op on non-Linux platforms.
func Start() {}

// WaitChild is inert off Linux (no PID-1 reaper competes with os/exec.Wait).
func WaitChild(pid int) (ws syscall.WaitStatus, ok bool) { return 0, false }
