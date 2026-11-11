//go:build !windows

package main

import "runtime"

func openFolder(folder string) error {
	name := "xdg-open"
	if runtime.GOOS == "darwin" {
		name = "open"
	}
	// The file manager may outlive the request; do not tie it to a timeout.
	return startProcess(name, []string{folder})
}
