//go:build !windows

package update

import (
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"
)

func samePath(a, b string) bool                          { return filepath.Clean(a) == filepath.Clean(b) }
func childCommand(name string, args ...string) *exec.Cmd { return exec.Command(name, args...) }

func waitForExit(pid int) error {
	if pid <= 1 {
		return fmt.Errorf("invalid update parent")
	}
	deadline := time.Now().Add(45 * time.Second)
	for time.Now().Before(deadline) {
		if err := syscall.Kill(pid, 0); errors.Is(err, syscall.ESRCH) {
			return nil
		} else if err != nil {
			return err
		}
		time.Sleep(100 * time.Millisecond)
	}
	return fmt.Errorf("ytaudio did not exit in time")
}
