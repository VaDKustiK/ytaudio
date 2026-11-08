package ytdlp

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildArgsUsesSafeArgumentBoundary(t *testing.T) {
	folder := t.TempDir()
	args, err := BuildArgs("https://www.youtube.com/watch?v=abc", folder, MP3)
	if err != nil {
		t.Fatal(err)
	}
	if args[len(args)-2] != "--" || args[len(args)-1] != "https://www.youtube.com/watch?v=abc" {
		t.Fatalf("URL boundary missing: %#v", args)
	}
	for _, arg := range args {
		if arg == "--cookies-from-browser" || arg == "--exec" {
			t.Fatal("unexpected user configuration or shell hook")
		}
	}
	foundTempPath := false
	for index, arg := range args {
		if arg == "--paths" && index+1 < len(args) && args[index+1] == "temp:"+filepath.Join(folder, ".ytaudio-temp") {
			foundTempPath = true
		}
	}
	if !foundTempPath {
		t.Fatalf("temporary path missing: %#v", args)
	}
	for _, arg := range args {
		if strings.Contains(arg, "--audio-format mp3") {
			t.Fatal("flags were concatenated")
		}
		foundRuntime := false
		for index, arg := range args {
			if arg == "--js-runtimes" && index+1 < len(args) && args[index+1] == "deno" {
				foundRuntime = true
			}
		}
		if !foundRuntime {
			t.Fatalf("Deno runtime flag missing: %#v", args)
		}
	}
}

func TestParseProgress(t *testing.T) {
	got, ok := ParseProgress("download:42.5%|1.2MiB/s|00:10")
	if !ok {
		t.Fatal("expected progress line")
	}
	if got.Percent != 42.5 || got.Speed != "1.2MiB/s" || got.ETA != "00:10" {
		t.Fatalf("progress = %#v", got)
	}
}

func TestParseProgressAcceptsYTDLPSpacing(t *testing.T) {
	got, ok := ParseProgress("download:  42.5%| 1.2MiB/s | 00:10 ")
	if !ok {
		t.Fatal("expected progress line")
	}
	if got.Percent != 42.5 || got.Speed != "1.2MiB/s" || got.ETA != "00:10" {
		t.Fatalf("progress = %#v", got)
	}
}

func TestConsumeProgressReturnsScannerError(t *testing.T) {
	err := consumeProgress(strings.NewReader(strings.Repeat("x", 70*1024)), nil)
	if err == nil {
		t.Fatal("expected scanner error for oversized line")
	}
}
