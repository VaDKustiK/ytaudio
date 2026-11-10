//go:build darwin && desktop

package main

// Wails uses UTType on macOS but its desktop package does not link the
// UniformTypeIdentifiers framework when built directly with go build.

// #cgo LDFLAGS: -framework UniformTypeIdentifiers
import "C"
