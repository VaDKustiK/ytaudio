package ytdlp

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

type Format string

const (
	MP3  Format = "mp3"
	WAV  Format = "wav"
	FLAC Format = "flac"
	MP4  Format = "mp4"
)

type Progress struct {
	Percent float64 `json:"percent"`
	Speed   string  `json:"speed,omitempty"`
	ETA     string  `json:"eta,omitempty"`
	Stage   string  `json:"stage"`
}

type Runner struct {
	Executable string
}

type ExecutionError struct {
	Err    error
	Stderr string
}

func (e *ExecutionError) Error() string {
	if e.Stderr == "" {
		return e.Err.Error()
	}
	return e.Err.Error() + ": " + e.Stderr
}

func (e *ExecutionError) Unwrap() error { return e.Err }

func BuildArgs(rawURL, outputFolder string, format Format) ([]string, error) {
	return BuildArgsWithPlaylist(rawURL, outputFolder, format, false)
}

func BuildArgsWithPlaylist(rawURL, outputFolder string, format Format, playlist bool) ([]string, error) {
	if outputFolder == "" || filepath.IsAbs(outputFolder) == false {
		return nil, fmt.Errorf("output folder must be an absolute path")
	}
	tempFolder := filepath.Join(outputFolder, ".ytaudio-temp")
	args := []string{"--newline", "--progress", "--progress-template", "download:%(progress._percent_str)s|%(progress._speed_str)s|%(progress._eta)s", "--progress-delta", "0.5", "--no-overwrites", "--no-keep-video", "--paths", "temp:" + tempFolder, "--js-runtimes", "deno", "--embed-metadata", "--sleep-requests", "1", "--sleep-interval", "3", "--max-sleep-interval", "8"}
	if playlist {
		args = append(args, "--playlist-end", "100")
	} else {
		args = append(args, "--no-playlist")
	}
	switch format {
	case MP3:
		args = append(args, "-x", "--audio-format", "mp3", "--audio-quality", "0")
	case WAV, FLAC:
		args = append(args, "-x", "--audio-format", string(format), "--audio-quality", "0")
	case MP4:
		args = append(args, "-f", "bv*+ba/b", "--merge-output-format", "mp4")
	default:
		return nil, fmt.Errorf("unsupported format %q", format)
	}
	args = append(args, "-o", filepath.Join(outputFolder, "%(title)s [%(id)s].%(ext)s"), "--", rawURL)
	return args, nil
}

var progressPattern = regexp.MustCompile(`^download:\s*(\d+(?:\.\d+)?)%?\s*\|\s*([^|]*)\s*\|\s*(.*)$`)
var ansiPattern = regexp.MustCompile(`\x1b\[[0-?]*[ -/]*[@-~]`)

func ParseProgress(line string) (Progress, bool) {
	line = strings.TrimSpace(ansiPattern.ReplaceAllString(line, ""))
	matches := progressPattern.FindStringSubmatch(line)
	if matches == nil {
		return Progress{}, false
	}

	percent, err := strconv.ParseFloat(strings.TrimSuffix(matches[1], "%"), 64)
	if err != nil {
		return Progress{}, false
	}
	return Progress{Percent: percent, Speed: strings.TrimSpace(matches[2]), ETA: strings.TrimSpace(matches[3]), Stage: "downloading"}, true
}

func (r Runner) Run(ctx context.Context, args []string, progress func(Progress), output ioWriter) error {
	cmd := exec.CommandContext(ctx, r.Executable, args...)
	cmd.Stdout = output
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	var log bytes.Buffer
	scannerErr := consumeProgress(io.TeeReader(stderr, &log), progress)
	waitErr := cmd.Wait()
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if scannerErr != nil {
		return &ExecutionError{Err: scannerErr, Stderr: strings.TrimSpace(log.String())}
	}
	if waitErr != nil {
		return &ExecutionError{Err: waitErr, Stderr: strings.TrimSpace(log.String())}
	}
	return nil
}

func consumeProgress(reader io.Reader, progress func(Progress)) error {
	scanner := bufio.NewScanner(reader)
	for scanner.Scan() {
		if item, ok := ParseProgress(scanner.Text()); ok && progress != nil {
			progress(item)
		}
	}
	return scanner.Err()
}

type ioWriter interface {
	Write([]byte) (int, error)
}

func OutputFolder(path string) error {
	info, err := os.Stat(path)
	if err != nil || !info.IsDir() {
		return fmt.Errorf("output folder must exist")
	}
	return nil
}
