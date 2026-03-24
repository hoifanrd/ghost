//go:build linux

package runner

import (
	"os/exec"
	"syscall"

	"github.com/zinc-sig/ghost/internal/sandbox"
)

// applySandboxToCmd applies Landlock restrictions and sets CLONE_NEWNET on the
// child process so it runs in an isolated network namespace.
func applySandboxToCmd(cmd *exec.Cmd, workDir string) error {
	if err := sandbox.ApplySandbox(workDir); err != nil {
		return err
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{Cloneflags: syscall.CLONE_NEWNET}
	return nil
}
