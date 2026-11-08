package ytdlp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"

	"github.com/VaDKustiK/ytaudio/internal/process"
	"github.com/VaDKustiK/ytaudio/internal/urlcheck"
)

type Format string

const (
	MP3  Format = "mp3"
	WAV  Format = "wav"
	FLAC Format = "flac"
	OGG  Format = "ogg"
	M4A  Format = "m4a"
)

type Progress struct {
	Percent float64 `json:"percent"`
	Speed   string  `json:"speed,omitempty"`
	ETA     string  `json:"eta,omitempty"`
	Stage   string  `json:"stage"`
	File    string  `json:"file,omitempty"`
}

type Runner struct {
	Executable string
	Env        []string
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
	var err error
	rawURL, err = urlcheck.Normalize(rawURL)
	if err != nil {
		return nil, err
	}
	if outputFolder == "" || !filepath.IsAbs(outputFolder) {
		return nil, fmt.Errorf("output folder must be an absolute path")
	}
	tempFolder := filepath.Join(outputFolder, ".ytaudio-temp")
	args := []string{"--match-filter", "!is_live & !is_upcoming", "--ignore-config", "--no-plugin-dirs", "--no-colors", "--windows-filenames", "--trim-filenames", "180", "--newline", "--progress", "--progress-template", "download:download:%(progress._percent_str)s|%(progress._speed_str)s|%(progress._eta_str)s", "--progress-template", "postprocess:postprocess:%(progress.status)s", "--print", "after_move:filepath:%(filepath)j", "--no-simulate", "--progress-delta", "0.5", "--no-overwrites", "--no-keep-video", "--paths", "home:" + outputFolder, "--paths", "temp:" + tempFolder, "--js-runtimes", "deno", "--embed-metadata", "--no-write-thumbnail", "--no-embed-thumbnail", "--socket-timeout", "30", "--retries", "3", "--sleep-requests", "1", "--sleep-interval", "3", "--max-sleep-interval", "8"}
	if playlist {
		args = append(args, "--playlist-end", "100")
	} else {
		args = append(args, "--no-playlist")
	}
	args = append(args, "-f", "bestaudio/best")
	switch format {
	case MP3:
		args = append(args, "-x", "--audio-format", "mp3", "--audio-quality", "0")
	case WAV, FLAC, M4A:
		args = append(args, "-x", "--audio-format", string(format), "--audio-quality", "0")
	case OGG:
		args = append(args, "-x", "--audio-format", "vorbis", "--audio-quality", "0")
	default:
		return nil, fmt.Errorf("unsupported format %q", format)
	}
	args = append(args, "-o", "%(title)s [%(id)s].%(ext)s", "--", rawURL)
	return args, nil
}

var progressPattern = regexp.MustCompile(`^download:\s*(\d+(?:\.\d+)?)%?\s*\|\s*([^|]*)\s*\|\s*(.*)$`)
var ansiPattern = regexp.MustCompile(`\x1b\[[0-?]*[ -/]*[@-~]`)

func ParseProgress(line string) (Progress, bool) {
	line = strings.TrimSpace(ansiPattern.ReplaceAllString(line, ""))
	if strings.HasPrefix(line, "postprocess:") {
		return Progress{Percent: 100, Stage: "converting"}, true
	}
	if strings.HasPrefix(line, "filepath:") {
		var name string
		if json.Unmarshal([]byte(strings.TrimPrefix(line, "filepath:")), &name) == nil {
			return Progress{Percent: 100, Stage: "saved", File: name}, true
		}
		return Progress{}, false
	}
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

// boundedWriter retains a diagnostic tail without allowing unbounded logs.
type boundedWriter struct {
	mu   sync.Mutex
	data []byte
}

func (b *boundedWriter) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.data = append(b.data, p...)
	if len(b.data) > 64*1024 {
		b.data = append([]byte(nil), b.data[len(b.data)-64*1024:]...)
	}
	return len(p), nil
}
func (b *boundedWriter) String() string { b.mu.Lock(); defer b.mu.Unlock(); return string(b.data) }

func (r Runner) Run(ctx context.Context, args []string, progress func(Progress), output io.Writer) error {
	if output == nil {
		output = io.Discard
	}
	cmd := process.Command(ctx, r.Executable, args...)
	if r.Env != nil {
		cmd.Env = r.Env
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	var log boundedWriter
	cmd.Stderr = &log
	cleanup, err := process.Start(cmd)
	if err != nil {
		return err
	}
	defer cleanup()
	if progress != nil {
		progress(Progress{Stage: "preparing"})
	}
	scannerErr := consumeProgress(io.TeeReader(stdout, output), progress)
	if scannerErr != nil {
		_ = cmd.Cancel()
	}
	waitErr := cmd.Wait()
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if scannerErr != nil {
		return &ExecutionError{Err: scannerErr, Stderr: log.String()}
	}
	if waitErr != nil {
		return &ExecutionError{Err: waitErr, Stderr: log.String()}
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

func OutputFolder(path string) error {
	if !filepath.IsAbs(path) {
		return fmt.Errorf("choose an absolute output folder")
	}
	info, err := os.Stat(path)
	if err != nil || !info.IsDir() {
		return fmt.Errorf("output folder must exist")
	}
	f, err := os.CreateTemp(path, ".ytaudio-write-*")
	if err != nil {
		return fmt.Errorf("output folder is not writable")
	}
	name := f.Name()
	f.Close()
	os.Remove(name)
	return nil
}
