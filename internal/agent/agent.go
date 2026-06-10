package agent

import (
	"fmt"
	"os"

	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/worker"

	"github.com/zinc-sig/ghost/internal/agent/contract"
)

// Run connects to Temporal, joins the per-run task queue and serves the
// two contract activities until interrupted (SIGTERM/SIGINT drain the
// worker gracefully).
func Run(cfg *Config) error {
	if err := os.MkdirAll(cfg.Workdir, 0o755); err != nil {
		return fmt.Errorf("agent: failed to create workspace %s: %w", cfg.Workdir, err)
	}

	store, err := newObjectStore(cfg)
	if err != nil {
		return err
	}

	opts := client.Options{
		HostPort:  cfg.TemporalAddress,
		Namespace: cfg.TemporalNamespace,
	}
	if cfg.AuthToken != "" {
		// Phase 8 seam (RFD 0015 Decision 8): once core issues per-run
		// queue tokens, wire cfg.AuthToken into opts.HeadersProvider
		// ("authorization: Bearer <token>") and enable TLS via
		// opts.ConnectionOptions. The trusted-network interim ignores it.
		_ = cfg.AuthToken
	}

	c, err := client.Dial(opts)
	if err != nil {
		return fmt.Errorf("agent: failed to connect to temporal at %s: %w", cfg.TemporalAddress, err)
	}
	defer c.Close()

	w := worker.New(c, cfg.TaskQueue, worker.Options{})
	acts := NewActivities(cfg, store)
	w.RegisterActivityWithOptions(acts.FetchSubmission, activity.RegisterOptions{Name: contract.FetchSubmissionActivity})
	w.RegisterActivityWithOptions(acts.RunExec, activity.RegisterOptions{Name: contract.RunExecActivity})

	if err := w.Run(worker.InterruptCh()); err != nil {
		return fmt.Errorf("agent: worker stopped: %w", err)
	}
	return nil
}
