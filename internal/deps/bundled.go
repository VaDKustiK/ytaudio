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
//go:embed assets/*
var bundledAssets embed.FS

func installBundled(dir string) error {
	platform := runtime.GOOS + "-" + runtime.GOARCH
	entries, err := fs.ReadDir(bundledAssets, "assets")
	if err != nil {
		return err
	}
	found := false
	for _, entry := range entries {
		if entry.IsDir() || entry.Name() == ".keep" {
			continue
		}
		if len(entry.Name()) < len(platform) || entry.Name()[:len(platform)] != platform {
			continue
		}
		found = true
		data, err := bundledAssets.ReadFile(filepath.Join("assets", entry.Name()))
		if err != nil {
			return err
		}
		name := entry.Name()[len(platform)+1:]
		target := filepath.Join(dir, name)
		if err := os.WriteFile(target, data, 0o700); err != nil {
			return fmt.Errorf("install bundled %s: %w", name, err)
		}
	}
	if !found {
		return nil
	}
	return nil
}
