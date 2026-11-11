package main

import (
	"fmt"
	"os"
	"path/filepath"
)

func revealFolder(folder string) error {
	if !filepath.IsAbs(folder) {
		return fmt.Errorf("output folder must be an absolute path")
	}
	folder = filepath.Clean(folder)
	info, err := os.Stat(folder)
	if err != nil {
		return fmt.Errorf("open output folder: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("output path is not a folder")
	}
	return openFolder(folder)
}
