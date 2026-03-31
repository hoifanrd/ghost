//go:build linux

package reaper

import (
	"os"
	"os/signal"
	"syscall"
)

// Start begins reaping zombie child processes in a background goroutine.
// Must be called from PID 1 to inherit orphaned children. The reaper
// drains all zombies per SIGCHLD delivery since multiple children can
// exit between signal deliveries.
func Start() {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGCHLD)
	go func() {
		for range ch {
			for {
				wpid, _ := syscall.Wait4(-1, nil, syscall.WNOHANG, nil)
				if wpid <= 0 {
					break
				}
			}
		}
	}()
}
