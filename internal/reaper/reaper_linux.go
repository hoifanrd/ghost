//go:build linux

package reaper

import (
	"os"
	"os/signal"
	"sync"
	"syscall"
)

var once sync.Once

// Start begins reaping zombie child processes in a background goroutine.
// Only activates when the current process is PID 1 (container init).
// Safe to call multiple times; the reaper is started at most once.
func Start() {
	if os.Getpid() != 1 {
		return
	}
	once.Do(func() {
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
	})
}
