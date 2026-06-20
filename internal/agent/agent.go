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

	if cfg.Sandbox {
		// Operational posture (RFD 0015 Decision 9/10): per-exec network
		// isolation via unshare(CLONE_NEWNET) needs CAP_SYS_ADMIN, which
		// a hardened grading container drops. When that is the case the
		// student command shares this container's network namespace —
		// including the egress the agent needs for Temporal/object
		// storage — so egress MUST be restricted by a container/cluster
		// network policy (deny-egress except the required endpoints).
		// The per-exec sandbox does not, and cannot, provide this.
		fmt.Fprintln(os.Stderr, "ghost agent: per-exec network isolation requires CAP_SYS_ADMIN; if the container drops it, restrict egress with a container/cluster network policy (RFD 0015 Decision 9/10, Phase 8)")
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

	// Bound activity concurrency per container. Core dispatches all of a
	// stage's scenarios in parallel; without a cap a wide stage spawns N
	// sandboxed children at once and the concurrent process count can
	// exceed RLIMIT_NPROC (cfg.MaxPids), making fork/spawn fail
	// intermittently ("could not spawn" -> error scenario state).
	w := worker.New(c, cfg.TaskQueue, worker.Options{
		MaxConcurrentActivityExecutionSize: cfg.MaxConcurrentExecs,
	})
	acts := NewActivities(cfg, store)
	w.RegisterActivityWithOptions(acts.FetchSubmission, activity.RegisterOptions{Name: contract.FetchSubmissionActivity})
	w.RegisterActivityWithOptions(acts.RunExec, activity.RegisterOptions{Name: contract.RunExecActivity})

	if err := w.Run(worker.InterruptCh()); err != nil {
		return fmt.Errorf("agent: worker stopped: %w", err)
	}
	return nil
}
