package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/zinc-sig/ghost/cmd/helpers"
	"github.com/zinc-sig/ghost/internal/runner"
)

var (
	execInputFile  string
	execOutputFile string
	execStderrFile string

	execTimeoutStr     string
	execLandlock       bool
	execWorkdir        string
	execIsolateNetwork bool
	execMaxPids        uint64
)

var execCmd = &cobra.Command{
	Use:   "exec [flags] -- <command> [args...]",
	Short: "Replace the current process with a command under optional isolation",
	Long: `Execute a command by replacing the ghost process via syscall.Exec after
redirecting stdin/stdout/stderr. No JSON output, webhooks, or uploads — the
process is replaced and the command's exit status is ghost's.

Landlock filesystem restrictions (--landlock) and per-exec network isolation
(--isolate-network) are independent: pass each only as needed.

The '--' separator is required to distinguish ghost flags from the target command.`,
	Example: `  ghost exec -i input.txt -o output.txt -e error.log -- ./my-command arg1
  ghost exec --landlock --isolate-network -i /dev/null -o out -e err -- ./prog`,
	RunE: execCommand,
}

func execCommand(cmd *cobra.Command, args []string) error {
	if err := helpers.ValidateCommandSeparator(cmd, args); err != nil {
		return err
	}

	ioFlags := helpers.IOFlags{
		Input:  execInputFile,
		Output: execOutputFile,
		Stderr: execStderrFile,
	}
	if err := helpers.ValidateIOFlags(ioFlags, false); err != nil {
		return err
	}

	timeout, err := helpers.ParseTimeout(execTimeoutStr)
	if err != nil {
		return err
	}

	config := &runner.Config{
		Command:        args[0],
		Args:           args[1:],
		InputFile:      execInputFile,
		OutputFile:     execOutputFile,
		StderrFile:     execStderrFile,
		Timeout:        timeout,
		Exec:           true,
		Landlock:       execLandlock,
		IsolateNetwork: execIsolateNetwork,
		SandboxWorkDir: execWorkdir,
		MaxPids:        execMaxPids,
	}

	return runner.ExecuteExec(config)
}

func init() {
	execCmd.Flags().StringVarP(&execInputFile, "input", "i", "", "Input file to redirect to command's stdin (required)")
	execCmd.Flags().StringVarP(&execOutputFile, "output", "o", "", "Output file to capture command's stdout (required)")
	execCmd.Flags().StringVarP(&execStderrFile, "stderr", "e", "", "Error file to capture command's stderr (required)")
	_ = execCmd.MarkFlagRequired("input")
	_ = execCmd.MarkFlagRequired("output")
	_ = execCmd.MarkFlagRequired("stderr")

	execCmd.Flags().StringVarP(&execTimeoutStr, "timeout", "t", "", "Timeout duration (e.g., 30s, 2m, 500ms)")
	execCmd.Flags().BoolVar(&execLandlock, "landlock", false, "Apply Landlock filesystem restrictions before execution")
	execCmd.Flags().StringVar(&execWorkdir, "workdir", "", "Working directory for Landlock read-write rules (defaults to current directory)")
	execCmd.Flags().BoolVar(&execIsolateNetwork, "isolate-network", false, "Unshare a network namespace before exec (loopback-only); requires CAP_SYS_ADMIN — silently no-ops in a capability-dropped container, leaving the container's network in effect")
	execCmd.Flags().Uint64Var(&execMaxPids, "max-pids", 0, "Maximum number of processes for the current user (includes ghost itself; 0 = no limit)")

	execCmd.PreRunE = func(cmd *cobra.Command, args []string) error {
		if execLandlock && execWorkdir == "" {
			wd, err := os.Getwd()
			if err != nil {
				return fmt.Errorf("failed to get current working directory: %w", err)
			}
			execWorkdir = wd
		}
		return nil
	}

	rootCmd.AddCommand(execCmd)
}
