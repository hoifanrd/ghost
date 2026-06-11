package sandbox

import "errors"

// ErrNetworkIsolationUnsupported is returned by IsolateNetwork when the
// process cannot create a network namespace because it lacks
// CAP_SYS_ADMIN (EPERM) — the expected case in a capability-dropped
// grading container. It is non-fatal: the spawned command runs in the
// container's network namespace, and restricting its egress is the
// container/cluster's responsibility (a deny-egress network policy),
// not ghost's. Callers must surface it, never treat it as isolation.
var ErrNetworkIsolationUnsupported = errors.New("network namespace isolation unsupported (no CAP_SYS_ADMIN); container-level egress policy required")
