package main

import (
	"context"
	"github.com/VaDKustiK/ytaudio/internal/process"
	"runtime"
	"time"
)

func revealFolder(folder string) error {
	name := "xdg-open"
	if runtime.GOOS == "windows" {
		return startProcess("explorer.exe", []string{folder})
	}
	if runtime.GOOS == "darwin" {
		name = "open"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return process.Command(ctx, name, folder).Run()
}
