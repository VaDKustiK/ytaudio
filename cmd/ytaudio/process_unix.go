//go:build !windows

package main

import "os/exec"

func startProcess(command string, args []string) error {
	return exec.Command(command, args...).Start()
}
