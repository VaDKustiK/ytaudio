package deps

import (
	"embed"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
)

// Release builds populate this directory with verified platform assets before
// compilation. Development builds fall back to the normal setup flow.
//
//go:embed assets
var bundledAssets embed.FS

func installBundled(dir string) error {
	platform := runtime.GOOS + "-" + runtime.GOARCH
	entries, err := fs.ReadDir(bundledAssets, filepath.Join("assets", platform))
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() || entry.Name() == "placeholder.txt" {
			continue
		}
		source := filepath.Join("assets", platform, entry.Name())
		data, err := bundledAssets.ReadFile(source)
		if err != nil {
			return err
		}
		target := filepath.Join(dir, entry.Name())
		if existing, err := os.ReadFile(target); err == nil && string(existing) == string(data) {
			continue
		}
		if err := os.WriteFile(target, data, 0o700); err != nil {
			return fmt.Errorf("install bundled %s: %w", entry.Name(), err)
		}
	}
	return nil
}
