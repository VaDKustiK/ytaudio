package update

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"

	"golang.org/x/sys/unix"
)

func installLocation(executable string) (string, string) {
	bundle := filepath.Dir(filepath.Dir(filepath.Dir(executable)))
	if strings.HasSuffix(bundle, ".app") && executable == filepath.Join(bundle, "Contents", "MacOS", "ytaudio") {
		return bundle, ".zip"
	}
	return executable, ""
}

func verifyBundle(path string) error {
	return exec.Command("/usr/bin/codesign", "--verify", "--deep", "--strict", path).Run()
}

func replaceApplication(incoming, destination string) error {
	// Swap both bundle paths atomically so the installed path always exists.
	return unix.RenameatxNp(unix.AT_FDCWD, incoming, unix.AT_FDCWD, destination, unix.RENAME_SWAP)
}

func installPackage(plan, string) error {
	return fmt.Errorf("unsupported update package on macOS")
}
