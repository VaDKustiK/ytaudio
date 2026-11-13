//go:build !windows

package analyzer

import (
	"os/exec"
	"syscall"
)

// Thread limits and disabled inference spinning bound impact on Unix. Avoid
// pre-exec hooks that would interfere with cancellation/process-group setup.
func setPriority(cmd *exec.Cmd, mode string) {}
func applyPriority(cmd *exec.Cmd, mode string) {
	if mode == "low" {
		_ = syscall.Setpriority(syscall.PRIO_PROCESS, cmd.Process.Pid, 10)
	}
}
