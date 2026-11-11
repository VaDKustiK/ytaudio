//go:build !windows

package main

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestRevealFolderPassesPathAsSingleArgument(t *testing.T) {
	dir := t.TempDir()
	folder := filepath.Join(dir, "Music & звук")
	if err := os.Mkdir(folder, 0700); err != nil {
		t.Fatal(err)
	}
	name := "xdg-open"
	if runtime.GOOS == "darwin" {
		name = "open"
	}
	opener := filepath.Join(dir, name)
	if err := os.WriteFile(opener, []byte("#!/bin/sh\nprintf '%s\\n%s' \"$#\" \"$1\" > \"$YTAUDIO_TEST_FOLDER_ARGS\"\n"), 0700); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(dir, "arguments")
	t.Setenv("PATH", dir)
	t.Setenv("YTAUDIO_TEST_FOLDER_ARGS", output)
	if err := revealFolder(folder); err != nil {
		t.Fatal(err)
	}
	want := "1\n" + folder
	deadline := time.Now().Add(5 * time.Second)
	for {
		data, _ := os.ReadFile(output)
		if string(data) == want {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("opener arguments = %q, want %q", data, want)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err := os.Remove(opener); err != nil {
		t.Fatal(err)
	}
	if err := revealFolder(folder); err == nil {
		t.Fatal("missing folder opener should report an error")
	}
}
