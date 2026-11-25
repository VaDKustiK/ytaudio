package update

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

func installLocation(executable string) (string, string) {
	if executable != "/usr/bin/ytaudio" && executable != "/bin/ytaudio" {
		return executable, ".tar.gz"
	}
	if _, err := os.Stat("/usr/bin/pkcon"); err != nil {
		return executable, ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if output, err := exec.CommandContext(ctx, "/usr/bin/dpkg-query", "-W", "-f=${Status}", "ytaudio").Output(); err == nil && strings.TrimSpace(string(output)) == "install ok installed" {
		return executable, ".deb"
	}
	if exec.CommandContext(ctx, "/usr/bin/rpm", "-q", "ytaudio").Run() == nil {
		return executable, ".rpm"
	}
	if exec.CommandContext(ctx, "/usr/bin/pacman", "-Q", "ytaudio").Run() == nil {
		return executable, ".pkg.tar.zst"
	}
	return executable, ""
}

func verifyBundle(string) error { return fmt.Errorf("unsupported app bundle on Linux") }

func replaceApplication(incoming, destination string) error { return os.Rename(incoming, destination) }

func installPackage(p plan, archive string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	// PackageKit controls authorization. The release file has already passed
	// ytaudio's digest check, but the packages lack a distribution signature.
	cmd := exec.CommandContext(ctx, "/usr/bin/pkcon", "--noninteractive", "--allow-untrusted", "install-local", archive)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("the system package manager could not install the update: %w", err)
	}
	if version, err := versionOf(p.Executable); err != nil || version != p.Version {
		return fmt.Errorf("the system package manager did not install %s", p.Version)
	}
	return nil
}
