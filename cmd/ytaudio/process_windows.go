//go:build windows

package main

import (
	"os/exec"
	"syscall"
)

func startProcess(command string, args []string) error {
	cmd := exec.Command(command, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { _ = cmd.Wait() }()
	return nil
}
