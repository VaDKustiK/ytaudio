//go:build windows

package main

import (
	"os/exec"
	"syscall"
)

func startProcess(command string, args []string) error {
	cmd := exec.Command(command, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	return cmd.Start()
}
