package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRevealFolderRejectsInvalidPaths(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "audio.mp3")
	if err := os.WriteFile(file, nil, 0600); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"", "relative-folder", filepath.Join(dir, "missing"), file} {
		if err := revealFolder(path); err == nil {
			t.Errorf("revealFolder(%q) should fail", path)
		}
	}
}
