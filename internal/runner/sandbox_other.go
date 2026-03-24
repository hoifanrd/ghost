//go:build !linux

package runner

import "os/exec"

// applySandboxToCmd is a no-op on non-Linux platforms.
func applySandboxToCmd(cmd *exec.Cmd, workDir string) error {
	return nil
}
