//go:build linux

package runner

import (
	"os/exec"

	"github.com/zinc-sig/ghost/internal/sandbox"
)

// applySandboxToCmd applies Landlock filesystem restrictions to the child
// process. Network isolation is the container/cluster's responsibility (egress
// NetworkPolicy), not ghost's.
func applySandboxToCmd(cmd *exec.Cmd, workDir string) error {
	return sandbox.ApplySandbox(workDir)
}
