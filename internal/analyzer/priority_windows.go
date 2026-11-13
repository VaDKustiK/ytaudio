package analyzer

import "os/exec"

func setPriority(cmd *exec.Cmd, mode string) {
	if mode == "low" && cmd.SysProcAttr != nil {
		cmd.SysProcAttr.CreationFlags |= 0x00004000
	}
}

func applyPriority(cmd *exec.Cmd, mode string) {}
