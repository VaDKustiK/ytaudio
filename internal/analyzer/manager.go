package analyzer

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/VaDKustiK/ytaudio/internal/process"
)

type selectedFile struct {
	File
	path      string
	temporary bool
}
type Manager struct {
	mu                          sync.Mutex
	ctx                         context.Context
	cancel                      context.CancelFunc
	wg                          sync.WaitGroup
	root, release, proxy, local string
	files                       map[string]selectedFile
	state                       State
	jobCancel                   context.CancelFunc
	slot                        chan struct{}
	closed                      bool
	lastLog                     string
}

func New(parent context.Context, release, proxy string) (*Manager, error) {
	root := os.Getenv("YTAUDIO_CACHE_DIR")
	if root == "" {
		var err error
		root, err = os.UserCacheDir()
		if err != nil {
			return nil, err
		}
	}
	root = filepath.Join(root, "ytaudio", "analyzer")
	if err := os.MkdirAll(root, 0700); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(parent)
	local := os.Getenv("YTAUDIO_ANALYZER_DIR")
	if local == "" && release == "dev" {
		if executable, e := os.Executable(); e == nil {
			candidate := filepath.Join(filepath.Dir(executable), "build", "analyzer", "install")
			if _, e = os.Stat(filepath.Join(candidate, "manifest.json")); e == nil {
				local = candidate
			}
		}
	}
	m := &Manager{ctx: ctx, cancel: cancel, root: root, release: release, proxy: proxy, local: local, files: map[string]selectedFile{}, slot: make(chan struct{}, 1), state: State{Status: "idle"}}
	m.state.Ready = m.available()
	return m, nil
}
func token() string { var b [16]byte; _, _ = rand.Read(b[:]); return hex.EncodeToString(b[:]) }
func (m *Manager) Register(path string, temporary bool) (File, error) {
	if !filepath.IsAbs(path) {
		return File{}, fmt.Errorf("select an audio file using Choose or drag and drop")
	}
	path = filepath.Clean(path)
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		return File{}, fmt.Errorf("audio file is missing or is not a regular file")
	}
	if info.Size() == 0 || info.Size() > 2<<30 {
		return File{}, fmt.Errorf("audio file is empty or too large (maximum 2 GB)")
	}
	switch strings.ToLower(filepath.Ext(path)) {
	case ".wav", ".mp3", ".flac", ".m4a", ".ogg", ".opus", ".aac", ".aiff", ".aif", ".wma", ".webm", ".mp4":
	default:
		return File{}, fmt.Errorf("unsupported audio file")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return File{}, fmt.Errorf("analyzer is closed")
	}
	if len(m.files) >= 500 {
		return File{}, fmt.Errorf("too many selected files; restart the app")
	}
	for _, f := range m.files {
		if f.path == path {
			return f.File, nil
		}
	}
	f := selectedFile{File: File{ID: token(), Name: filepath.Base(path)}, path: path, temporary: temporary}
	m.files[f.ID] = f
	return f.File, nil
}
func (m *Manager) RegisterUpload(path, name string) (File, error) {
	file, err := m.Register(path, true)
	if err != nil {
		return file, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	f := m.files[file.ID]
	f.Name = filepath.Base(name)
	m.files[file.ID] = f
	// Browser uploads are temporary copies. Retain only the newest selection
	// and the file used by an active job, not a growing audio library.
	for id, old := range m.files {
		if id == file.ID || !old.temporary {
			continue
		}
		if id == m.state.FileID && (m.state.Status == "running" || m.state.Status == "queued") {
			continue
		}
		if err := os.Remove(old.path); err == nil || os.IsNotExist(err) {
			delete(m.files, id)
		}
	}
	return f.File, nil
}
func (m *Manager) Snapshot() State { m.mu.Lock(); defer m.mu.Unlock(); return m.state }
func (m *Manager) Cancel() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.jobCancel != nil {
		m.jobCancel()
	}
}
func (m *Manager) Close() {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return
	}
	m.closed = true
	m.cancel()
	if m.jobCancel != nil {
		m.jobCancel()
	}
	m.mu.Unlock()
	m.wg.Wait()
	for _, f := range m.files {
		if f.temporary {
			_ = os.Remove(f.path)
		}
	}
}
func (m *Manager) Start(fileID, ffmpeg, mode string) (State, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return State{}, fmt.Errorf("analyzer is closed")
	}
	if m.state.Installing {
		return State{}, fmt.Errorf("wait for analyzer setup to finish")
	}
	if !m.state.Ready {
		return State{}, fmt.Errorf("install the analyzer component first")
	}
	f, ok := m.files[fileID]
	if !ok {
		return State{}, fmt.Errorf("select an audio file first")
	}
	if ffmpeg == "" {
		return State{}, fmt.Errorf("set up FFmpeg in Info first")
	}
	if m.jobCancel != nil {
		m.jobCancel()
	}
	ctx, cancel := context.WithTimeout(m.ctx, 30*time.Minute)
	m.jobCancel = cancel
	state := State{ID: token(), FileID: fileID, Name: f.Name, Status: "queued", Stage: "Waiting", Ready: true}
	m.state = state
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		defer cancel()
		select {
		case m.slot <- struct{}{}:
			defer func() { <-m.slot }()
		case <-ctx.Done():
			m.finish(state.ID, nil, ctx.Err(), false)
			return
		}
		m.report(state.ID, "Verifying analyzer", 1)
		result, cached, err := m.analyze(ctx, f.path, ffmpeg, mode, func(stage string, percent float64) { m.report(state.ID, stage, percent) })
		m.finish(state.ID, result, err, cached)
	}()
	return state, nil
}
func (m *Manager) report(id, stage string, percent float64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.state.ID != id {
		return
	}
	m.state.Status = "running"
	m.state.Stage = stage
	if percent >= 0 && percent <= 100 {
		m.state.Percent = percent
	}
}

type executionError struct{ message, details string }

func (e *executionError) Error() string { return e.message }
func (m *Manager) LastLog() string      { m.mu.Lock(); defer m.mu.Unlock(); return m.lastLog }

func (m *Manager) finish(id string, result *Result, err error, cached bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.state.ID != id {
		return
	}
	m.state.Status = "complete"
	m.state.Stage = "Complete"
	m.state.Percent = 100
	m.state.Result = result
	m.state.Cached = cached
	if err != nil {
		var detail *executionError
		if errors.As(err, &detail) {
			m.lastLog = detail.details
		}
		if errors.Is(err, ErrComponent) {
			m.state.Ready = false
		}
		m.state.Status = "failed"
		m.state.Stage = "Analysis failed"
		m.state.Percent = 0
		m.state.Error = err.Error()
		if errors.Is(err, context.DeadlineExceeded) {
			m.state.Stage = "Analysis timed out"
			m.state.Error = "Analysis timed out. Try a shorter file."
		} else if errors.Is(err, context.Canceled) {
			m.state.Status = "cancelled"
			m.state.Stage = "Cancelled"
			m.state.Error = ""
		}
	}
}

var ErrComponent = errors.New("analyzer component needs repair")

const maxPCM = 44100 * 2 * 4 * 1800

type limitWriter struct {
	writer    io.Writer
	remaining int64
}

func (w *limitWriter) Write(p []byte) (int, error) {
	if int64(len(p)) > w.remaining {
		return 0, fmt.Errorf("decoded audio exceeds 30 minutes")
	}
	n, err := w.writer.Write(p)
	w.remaining -= int64(n)
	return n, err
}

type bounded struct {
	data  []byte
	limit int
}

func (b *bounded) Write(p []byte) (int, error) {
	n := len(p)
	if len(b.data)+n > b.limit {
		return 0, fmt.Errorf("analyzer output limit exceeded")
	}
	b.data = append(b.data, p...)
	return n, nil
}
func hashFile(ctx context.Context, path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	buffer := make([]byte, 128<<10)
	for {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		n, e := f.Read(buffer)
		if n > 0 {
			_, _ = h.Write(buffer[:n])
		}
		if e == io.EOF {
			break
		}
		if e != nil {
			return "", e
		}
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
func (m *Manager) analyze(ctx context.Context, path, ffmpeg, mode string, report func(string, float64)) (*Result, bool, error) {
	executable, models, err := m.paths()
	if err != nil {
		return nil, false, err
	}
	if err = verifyComponent(ctx, filepath.Dir(executable), false); err != nil {
		if ctx.Err() != nil {
			return nil, false, ctx.Err()
		}
		return nil, false, fmt.Errorf("%w: %v", ErrComponent, err)
	}
	if err = verifyComponent(ctx, models, true); err != nil {
		if ctx.Err() != nil {
			return nil, false, ctx.Err()
		}
		return nil, false, fmt.Errorf("%w: %v", ErrComponent, err)
	}
	initial, err := os.Stat(path)
	if err != nil {
		return nil, false, fmt.Errorf("audio file is missing; select it again")
	}
	digest, err := hashFile(ctx, path)
	if err != nil {
		return nil, false, err
	}
	cacheDir := filepath.Join(m.root, "results")
	engineHash, err := hashFile(ctx, executable)
	if err != nil {
		return nil, false, err
	}
	modelHash, err := hashFile(ctx, filepath.Join(models, "manifest.json"))
	if err != nil {
		return nil, false, err
	}
	cachePath := filepath.Join(cacheDir, Version+"-"+ModelsVersion+"-"+engineHash[:16]+"-"+modelHash[:16]+"-"+digest+".json")
	if data, e := os.ReadFile(cachePath); e == nil {
		if result, e := ParseResult(data); e == nil {
			current, e := os.Stat(path)
			if e != nil || !os.SameFile(initial, current) || current.Size() != initial.Size() || !current.ModTime().Equal(initial.ModTime()) {
				return nil, false, fmt.Errorf("audio changed while reading; select it again")
			}
			return &result, true, nil
		}
	}
	directory, err := os.MkdirTemp(m.root, ".job-")
	if err != nil {
		return nil, false, err
	}
	defer os.RemoveAll(directory)
	pcmPath := filepath.Join(directory, "audio.f32")
	pcm, err := os.Create(pcmPath)
	if err != nil {
		return nil, false, err
	}
	report("Decoding audio", 3)
	cmd := process.Command(ctx, ffmpeg, "-nostdin", "-v", "error", "-protocol_whitelist", "file,pipe", "-threads", fmt.Sprint(Threads(mode, 0)), "-filter_threads", "1", "-i", path, "-map", "0:a:0", "-vn", "-t", "1801", "-ac", "2", "-ar", "44100", "-f", "f32le", "pipe:1")
	setPriority(cmd, mode)
	cmd.Stdout = &limitWriter{writer: pcm, remaining: maxPCM + 8}
	var log bounded
	log.limit = 64 << 10
	cmd.Stderr = &log
	cleanup, err := process.Start(cmd)
	if err == nil {
		applyPriority(cmd, mode)
		err = cmd.Wait()
		cleanup()
	}
	closeErr := pcm.Close()
	if ctx.Err() != nil {
		return nil, false, ctx.Err()
	}
	if err != nil || closeErr != nil {
		return nil, false, &executionError{message: "could not decode audio (unsupported, damaged, or longer than 30 minutes)", details: strings.ReplaceAll(string(log.data), path, "<audio>")}
	}
	pcmInfo, statErr := os.Stat(pcmPath)
	if statErr != nil {
		return nil, false, statErr
	}
	if pcmInfo.Size() < 44100*2*4*2 {
		return nil, false, fmt.Errorf("choose at least 2 seconds of audio")
	}
	if pcmInfo.Size() > maxPCM {
		return nil, false, fmt.Errorf("audio must be 30 minutes or shorter")
	}
	report("Analyzing", 5)
	cmd = process.Command(ctx, executable, "--pcm", pcmPath, "--models", models, "--threads", fmt.Sprint(Threads(mode, 0)))
	setPriority(cmd, mode)
	var output bounded
	output.limit = 64 << 10
	cmd.Stdout = &output
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, false, err
	}
	cleanup, err = process.Start(cmd)
	if err != nil {
		if ctx.Err() != nil {
			return nil, false, ctx.Err()
		}
		return nil, false, fmt.Errorf("%w: could not start analyzer: %v", ErrComponent, err)
	}
	defer cleanup()
	applyPriority(cmd, mode)
	nativeLog := ""
	scanDone := make(chan error, 1)
	go func() {
		scanner := bufio.NewScanner(stderr)
		scanner.Buffer(make([]byte, 4096), 64<<10)
		for scanner.Scan() {
			var p struct {
				Stage   string  `json:"stage"`
				Percent float64 `json:"percent"`
			}
			if json.Unmarshal(scanner.Bytes(), &p) == nil && p.Stage != "" {
				report(p.Stage, p.Percent)
			} else {
				nativeLog += scanner.Text() + "\n"
				if len(nativeLog) > 16000 {
					nativeLog = nativeLog[len(nativeLog)-16000:]
				}
			}
		}
		scanDone <- scanner.Err()
	}()
	scanErr := <-scanDone
	if scanErr != nil {
		_ = cmd.Cancel()
	}
	err = cmd.Wait()
	if ctx.Err() != nil {
		return nil, false, ctx.Err()
	}
	if err != nil || scanErr != nil {
		if err != nil {
			nativeLog += "Process: " + err.Error() + "\n"
		}
		if scanErr != nil {
			nativeLog += "Progress: " + scanErr.Error() + "\n"
		}
		return nil, false, &executionError{message: "analysis failed; check the file or repair the analyzer component", details: strings.ReplaceAll(nativeLog, path, "<audio>")}
	}
	result, err := ParseResult(output.data)
	if err != nil {
		return nil, false, fmt.Errorf("%w: invalid analyzer response: %v", ErrComponent, err)
	}
	final, err := os.Stat(path)
	if err != nil || !os.SameFile(initial, final) || final.Size() != initial.Size() || !final.ModTime().Equal(initial.ModTime()) {
		return nil, false, fmt.Errorf("audio changed during analysis; select it again")
	}
	if err = os.MkdirAll(cacheDir, 0700); err == nil {
		pruneCache(cacheDir)
		_ = os.WriteFile(cachePath, output.data, 0600)
	}
	return &result, false, nil
}
func pruneCache(dir string) {
	entries, _ := os.ReadDir(dir)
	if len(entries) < 200 {
		return
	}
	for _, entry := range entries {
		if entry.Type().IsRegular() {
			_ = os.Remove(filepath.Join(dir, entry.Name()))
			return
		}
	}
}
func executableName() string {
	if runtime.GOOS == "windows" {
		return "ytaudio-analyzer.exe"
	}
	return "ytaudio-analyzer"
}

func (m *Manager) SetProxy(proxy string) { m.mu.Lock(); defer m.mu.Unlock(); m.proxy = proxy }
