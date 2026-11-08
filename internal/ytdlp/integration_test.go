package ytdlp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestRunnerHelper(t *testing.T) {
	if os.Getenv("YTAUDIO_RUNNER_HELPER") == "" {
		return
	}
	mode := os.Args[len(os.Args)-1]
	switch mode {
	case "progress":
		fmt.Println("download: 42.5%|1.2MiB/s|00:10")
		fmt.Println("postprocess:processing")
		fmt.Println(`filepath:"/tmp/音楽/song.mp3"`)
	case "failure":
		fmt.Fprint(os.Stderr, strings.Repeat("x", 128*1024)+" HTTP Error 403: Forbidden")
		os.Exit(1)
	case "oversized":
		fmt.Println(strings.Repeat("x", 128*1024))
		time.Sleep(time.Minute)
	case "wait":
		time.Sleep(time.Minute)
	}
	os.Exit(0)
}
func helperRunner(t *testing.T) Runner {
	t.Helper()
	p, e := os.Executable()
	if e != nil {
		t.Fatal(e)
	}
	return Runner{Executable: p, Env: append(os.Environ(), "YTAUDIO_RUNNER_HELPER=1")}
}
func TestRunnerConsumesRealStdoutAndReportsFinalFile(t *testing.T) {
	var events []Progress
	err := helperRunner(t).Run(context.Background(), []string{"-test.run=TestRunnerHelper", "--", "progress"}, func(p Progress) { events = append(events, p) }, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 4 || events[1].Percent != 42.5 || events[3].File != "/tmp/音楽/song.mp3" {
		t.Fatalf("events=%#v", events)
	}
}
func TestRunnerBoundsErrorsAndClassifiesStderr(t *testing.T) {
	err := helperRunner(t).Run(context.Background(), []string{"-test.run=TestRunnerHelper", "--", "failure"}, nil, io.Discard)
	if err == nil || len(err.Error()) > 66000 || Classify(err.Error()).Kind != Forbidden403 {
		t.Fatalf("unexpected bounded error: %v", err)
	}
}
func TestRunnerCancelsPromptly(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	start := time.Now()
	err := helperRunner(t).Run(ctx, []string{"-test.run=TestRunnerHelper", "--", "wait"}, nil, io.Discard)
	if err != context.DeadlineExceeded || time.Since(start) > 5*time.Second {
		t.Fatalf("cancel: %v after %v", err, time.Since(start))
	}
}
func TestAllRequestedFormats(t *testing.T) {
	for _, format := range []Format{MP3, WAV, FLAC, MP4, OGG} {
		t.Run(string(format), func(t *testing.T) {
			args, err := BuildArgs("https://youtu.be/abc", t.TempDir(), format)
			if err != nil {
				t.Fatal(err)
			}
			joined := strings.Join(args, " ")
			if !strings.Contains(joined, "--ignore-config") || !strings.Contains(joined, "--no-plugin-dirs") || !strings.Contains(joined, "bestaudio/best") {
				t.Fatal(joined)
			}
			if format == MP4 && (!strings.Contains(joined, "--remux-video mp4") || strings.Contains(joined, "--audio-format")) {
				t.Fatal("MP4 must remux without re-encoding")
			}
		})
	}
}
func TestOutputFolderRejectsFilesAndRelativePaths(t *testing.T) {
	if OutputFolder("relative") == nil {
		t.Fatal("relative accepted")
	}
	path := filepath.Join(t.TempDir(), "file")
	os.WriteFile(path, []byte("x"), 0600)
	if OutputFolder(path) == nil {
		t.Fatal("file accepted")
	}
}
func TestBuildArgsRejectsMaliciousURL(t *testing.T) {
	for _, url := range []string{"--exec=bad", "https://youtube.com.evil/a", "https://user@youtube.com/a"} {
		if _, err := BuildArgs(url, t.TempDir(), MP3); err == nil {
			t.Fatal(url)
		}
	}
}

// Opt-in because this exercises actual FFmpeg and yt-dlp executables. The media
// is a generated sine wave and every input is local: no YouTube/network access.
func TestLocalAudioConversion(t *testing.T) {
	if os.Getenv("YTAUDIO_MEDIA_TESTS") != "1" {
		t.Skip("set YTAUDIO_MEDIA_TESTS=1 to test real media tools")
	}
	if runtime.GOOS != "linux" {
		t.Skip("local tool paths are Linux-specific")
	}
	root, err := filepath.Abs("../../internal/deps/assets/linux-amd64")
	if err != nil {
		t.Fatal(err)
	}
	ffmpeg := filepath.Join(root, "ffmpeg")
	probe := filepath.Join(root, "ffprobe")
	yt := filepath.Join(root, "yt-dlp")
	input := filepath.Join(t.TempDir(), "input.webm")
	if output, err := exec.Command(ffmpeg, "-hide_banner", "-loglevel", "error", "-f", "lavfi", "-i", "sine=frequency=440:duration=1", "-c:a", "libopus", input).CombinedOutput(); err != nil {
		t.Fatalf("generate audio: %v %s", err, output)
	}
	for _, format := range []Format{MP3, WAV, FLAC, MP4, OGG} {
		t.Run(string(format), func(t *testing.T) {
			args, err := BuildArgs("https://youtu.be/abc", t.TempDir(), format)
			if err != nil {
				t.Fatal(err)
			}
			filtered := []string{"--enable-file-urls", "--no-cache-dir"}
			for i := 0; i < len(args); i++ {
				if args[i] == "--sleep-requests" || args[i] == "--sleep-interval" || args[i] == "--max-sleep-interval" {
					i++
					continue
				}
				filtered = append(filtered, args[i])
			}
			filtered[len(filtered)-1] = "file://" + input
			var final string
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			err = (Runner{Executable: yt, Env: append(os.Environ(), "PATH="+root+string(os.PathListSeparator)+os.Getenv("PATH"))}).Run(ctx, filtered, func(p Progress) {
				if p.File != "" {
					final = p.File
				}
			}, io.Discard)
			if err != nil {
				t.Fatal(err)
			}
			if final == "" {
				t.Fatal("no final file event")
			}
			out, err := exec.Command(probe, "-v", "error", "-show_entries", "stream=codec_name,codec_type", "-of", "json", final).Output()
			if err != nil {
				t.Fatal(err)
			}
			var info struct {
				Streams []struct {
					Codec string `json:"codec_name"`
					Type  string `json:"codec_type"`
				} `json:"streams"`
			}
			if err = json.Unmarshal(out, &info); err != nil {
				t.Fatal(err)
			}
			expected := map[Format]string{MP3: "mp3", WAV: "pcm_s16le", FLAC: "flac", MP4: "opus", OGG: "vorbis"}[format]
			if len(info.Streams) != 1 || info.Streams[0].Type != "audio" || info.Streams[0].Codec != expected {
				t.Fatalf("unexpected audio output %s: %s", format, out)
			}
		})
	}
}

func TestOversizedStdoutTerminatesProcess(t *testing.T) {
	start := time.Now()
	err := helperRunner(t).Run(context.Background(), []string{"-test.run=TestRunnerHelper", "--", "oversized"}, nil, io.Discard)
	if err == nil || time.Since(start) > 5*time.Second {
		t.Fatalf("oversized output hung: %v", err)
	}
}
func TestOutputTemplateEscapesPercentInFolder(t *testing.T) {
	folder := filepath.Join(t.TempDir(), "100% music")
	args, err := BuildArgs("https://youtu.be/abc", folder, MP3)
	if err != nil {
		t.Fatal(err)
	}
	for i, a := range args {
		if a == "-o" {
			if !strings.Contains(args[i+1], "100%% music") {
				t.Fatal(args[i+1])
			}
			return
		}
	}
	t.Fatal("missing output template")
}
