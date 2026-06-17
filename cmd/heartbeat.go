package cmd

import (
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"github.com/zinc-sig/ghost/internal/reaper"
)

var (
	heartbeatInterval time.Duration
	heartbeatFile     string
)

var heartbeatCmd = &cobra.Command{
	Use:   "heartbeat",
	Short: "PID 1 keepalive that writes timestamps to a file",
	Long: `Run a keepalive loop that periodically writes Unix timestamps to a file.
Intended for use as a container ENTRYPOINT to signal liveness.

The process handles SIGTERM and SIGINT for graceful shutdown.`,
	Example: `  ghost heartbeat
  ghost heartbeat --interval 5s --file /output/.heartbeat`,
	RunE: heartbeatCommand,
}

func init() {
	heartbeatCmd.Flags().DurationVar(&heartbeatInterval, "interval", 10*time.Second, "interval between heartbeat writes")
	heartbeatCmd.Flags().StringVar(&heartbeatFile, "file", "/output/.heartbeat", "file to write heartbeat timestamps to")
}

func heartbeatCommand(cmd *cobra.Command, args []string) error {
	// Reap zombie children so fork bomb corpses free their PID slots promptly.
	reaper.Start()

	ctx, stop := signal.NotifyContext(cmd.Context(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	ticker := time.NewTicker(heartbeatInterval)
	defer ticker.Stop()

	// Write an initial heartbeat immediately
	if err := writeHeartbeat(heartbeatFile); err != nil {
		fmt.Fprintf(os.Stderr, "heartbeat: initial write failed: %v\n", err)
	}

	consecutiveErrors := 0
	const maxConsecutiveErrors = 5

	for {
		select {
		case <-ctx.Done():
			fmt.Fprintln(os.Stderr, "heartbeat: shutting down")
			return nil
		case <-ticker.C:
			if err := writeHeartbeat(heartbeatFile); err != nil {
				consecutiveErrors++
				fmt.Fprintf(os.Stderr, "heartbeat: write failed (%d/%d): %v\n", consecutiveErrors, maxConsecutiveErrors, err)
				if consecutiveErrors >= maxConsecutiveErrors {
					return fmt.Errorf("heartbeat: too many consecutive write errors (%d)", consecutiveErrors)
				}
			} else {
				consecutiveErrors = 0
			}
		}
	}
}

func writeHeartbeat(path string) error {
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	return os.WriteFile(path, []byte(ts), 0644)
}
