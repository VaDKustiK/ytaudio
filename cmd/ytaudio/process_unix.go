//go:build !windows

package main

import "os/exec"

func startProcess(command string, args []string) error {
	cmd := exec.Command(command, args...)
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { _ = cmd.Wait() }()
	return nil
}
