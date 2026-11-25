package update

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

func samePath(a, b string) bool { return strings.EqualFold(filepath.Clean(a), filepath.Clean(b)) }

func childCommand(name string, args ...string) *exec.Cmd {
	cmd := exec.Command(name, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	return cmd
}

func waitForExit(pid int) error {
	if pid <= 1 {
		return fmt.Errorf("invalid update parent")
	}
	handle, err := windows.OpenProcess(windows.SYNCHRONIZE, false, uint32(pid))
	if errors.Is(err, windows.ERROR_INVALID_PARAMETER) {
		return nil
	}
	if err != nil {
		return err
	}
	defer windows.CloseHandle(handle)
	result, err := windows.WaitForSingleObject(handle, 45000)
	if err != nil {
		return err
	}
	if result != windows.WAIT_OBJECT_0 {
		return fmt.Errorf("ytaudio did not exit in time")
	}
	return nil
}

const uninstallKey = `Software\Microsoft\Windows\CurrentVersion\Uninstall\ytaudio`

func installLocation(executable string) (string, string) {
	// NSIS is a 32-bit installer, including for the ARM64 app.
	if key, err := registry.OpenKey(registry.CURRENT_USER, uninstallKey, registry.QUERY_VALUE|registry.WOW64_32KEY); err == nil {
		defer key.Close()
		value, _, err := key.GetStringValue("UninstallString")
		if err == nil && strings.EqualFold(filepath.Base(executable), "ytaudio.exe") &&
			samePath(strings.Trim(value, `"`), filepath.Join(filepath.Dir(executable), "Uninstall.exe")) {
			return executable, "-setup.exe"
		}
	}
	return executable, ".zip"
}

func verifyBundle(string) error { return fmt.Errorf("unsupported app bundle on Windows") }

func replaceApplication(incoming, destination string) error { return os.Rename(incoming, destination) }

func installPackage(p plan, archive string) error {
	stage, err := os.MkdirTemp(filepath.Dir(p.Executable), ".ytaudio-backup-")
	if err != nil {
		return err
	}
	defer func() {
		if stage != "" {
			os.RemoveAll(stage)
		}
	}()
	backup := filepath.Join(stage, "ytaudio.exe")
	if err = copyExecutable(p.Executable, backup); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, archive)
	// NSIS parses /D itself. It must be last and unquoted, even with spaces.
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CmdLine: windows.EscapeArg(archive) + " /S /D=" + filepath.Dir(p.Executable)}
	if err = cmd.Run(); err == nil {
		if got, probeErr := versionOf(p.Executable); probeErr == nil && got == p.Version {
			return nil
		}
		err = fmt.Errorf("the installer did not install %s", p.Version)
	}
	if restoreErr := os.Rename(backup, p.Executable); restoreErr != nil {
		stage = ""
		return fmt.Errorf("could not restore the previous executable: %w", restoreErr)
	}
	return err
}
