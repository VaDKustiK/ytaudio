package deps

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/VaDKustiK/ytaudio/internal/process"
)

type Manager struct {
	dir    string
	client *http.Client
	mu     sync.Mutex
}

type Progress struct {
	Message string
	Percent float64
	Bytes   int64
	Total   int64
}

type Status struct {
	Name    string `json:"name"`
	Path    string `json:"-"`
	Version string `json:"version,omitempty"`
	Ready   bool   `json:"ready"`
	Error   string `json:"error,omitempty"`
}

func New(proxy string) (*Manager, error) {
	cache := os.Getenv("YTAUDIO_CACHE_DIR")
	var err error
	if cache == "" {
		cache, err = os.UserCacheDir()
	}
	if err != nil {
		return nil, fmt.Errorf("find user cache directory: %w", err)
	}
	client, err := newHTTPClient(proxy)
	if err != nil {
		return nil, err
	}
	manager := &Manager{
		dir: filepath.Join(cache, "ytaudio", "bin"),
		// The setup operation supplies its own overall context timeout. A
		// short client timeout incorrectly aborts large downloads on slow links.
		client: client,
	}
	if err := os.MkdirAll(manager.dir, 0o700); err != nil {
		return nil, fmt.Errorf("create dependency directory: %w", err)
	}
	return manager, nil
}

func newHTTPClient(proxy string) (*http.Client, error) {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.ResponseHeaderTimeout = 30 * time.Second
	if proxy != "" {
		parsed, err := url.Parse(proxy)
		if err != nil || parsed.Host == "" {
			return nil, fmt.Errorf("invalid proxy URL")
		}
		transport.Proxy = http.ProxyURL(parsed)
	}
	return &http.Client{Transport: transport}, nil
}

func (m *Manager) toolPath(name string) string {
	path := filepath.Join(m.dir, executableName(name))
	if info, err := os.Stat(path); err == nil && !info.IsDir() {
		return path
	}
	path, _ = exec.LookPath(name)
	return path
}
func (m *Manager) Environment() []string {
	env := os.Environ()
	for i, v := range env {
		if strings.HasPrefix(strings.ToUpper(v), "PATH=") {
			env[i] = "PATH=" + m.dir + string(os.PathListSeparator) + v[5:]
			return env
		}
	}
	return append(env, "PATH="+m.dir)
}
func (m *Manager) Status(ctx context.Context) []Status {
	var statuses []Status
	for _, name := range []string{"yt-dlp", "ffmpeg", "ffprobe", "deno"} {
		path := m.toolPath(name)
		status := Status{Name: name, Path: path}
		flag := "--version"
		if name == "ffmpeg" || name == "ffprobe" {
			flag = "-version"
		}
		if path != "" {
			status.Version = commandVersionWithArgs(ctx, path, flag)
			status.Ready = status.Version != ""
		}
		if name == "yt-dlp" && len(status.Version) >= 10 && status.Version[:10] < "2025.11.12" {
			status.Ready = false
			status.Error = "This yt-dlp is too old; run setup to update."
		}
		if !status.Ready && status.Error == "" {
			status.Error = "Not installed or could not start"
		}
		statuses = append(statuses, status)
	}
	return statuses
}
func (m *Manager) YTDLPPath() (string, error) {
	path := m.toolPath("yt-dlp")
	if path == "" {
		return "", fmt.Errorf("yt-dlp is not ready")
	}
	return path, nil
}
func (m *Manager) Setup(ctx context.Context, progress func(Progress)) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, s := range m.Status(ctx) {
		if s.Ready {
			continue
		}
		if progress != nil {
			progress(Progress{Message: "Installing " + s.Name})
		}
		var err error
		if s.Name == "yt-dlp" {
			err = m.updateYTDLP(ctx, progress)
		} else {
			err = m.installTool(ctx, s.Name, progress)
		}
		if err != nil {
			return fmt.Errorf("%s setup failed: %w", s.Name, err)
		}
	}
	for _, s := range m.Status(ctx) {
		if !s.Ready {
			return fmt.Errorf("%s could not start after setup", s.Name)
		}
	}
	return nil
}
func (m *Manager) Update(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.updateYTDLP(ctx, nil)
}
func (m *Manager) updateYTDLP(ctx context.Context, progress func(Progress)) error {
	release, err := m.release(ctx, "yt-dlp/yt-dlp", "")
	// Checksummed public downloads also work when GitHub's API quota is exhausted.
	if err != nil {
		release.Tag = ""
	}
	filename, err := ytDLPFilename(runtime.GOOS, runtime.GOARCH)
	if err != nil {
		return err
	}
	base := "https://github.com/yt-dlp/yt-dlp/releases/download/" + release.Tag + "/"
	if release.Tag == "" {
		base = ytDLPBaseURL
	}
	target := filepath.Join(m.dir, executableName("yt-dlp"))
	if release.Tag != "" && commandVersion(ctx, target) == release.Tag {
		return nil
	}
	if err = m.downloadVerified(ctx, Source{Name: "yt-dlp", URL: base + filename, ChecksumsURL: base + "SHA2-256SUMS", Filename: filename}, target, progress); err != nil {
		return err
	}
	if commandVersion(ctx, target) == "" {
		os.Remove(target)
		os.Rename(target+".previous", target)
		return fmt.Errorf("new yt-dlp could not start; previous version restored")
	}
	return nil
}

func (m *Manager) downloadVerified(ctx context.Context, source Source, target string, progress func(Progress)) error {
	expected := source.SHA256
	if expected == "" {
		checksumBody, _, err := m.fetch(ctx, source.ChecksumsURL)
		if err != nil {
			return fmt.Errorf("download checksum list: %w", err)
		}
		expected, err = checksumFor(io.LimitReader(checksumBody, 4<<20), source.Filename)
		checksumBody.Close()
		if err != nil {
			return err
		}
	}
	if decoded, err := hex.DecodeString(expected); err != nil || len(decoded) != sha256.Size {
		return fmt.Errorf("invalid SHA-256 checksum")
	}
	body, total, err := m.fetch(ctx, source.URL)
	if err != nil {
		return fmt.Errorf("download binary: %w", err)
	}
	defer body.Close()
	temp, err := os.CreateTemp(filepath.Dir(target), ".ytaudio-download-*")
	if err != nil {
		return err
	}
	tempName := temp.Name()
	defer os.Remove(tempName)
	hash := sha256.New()
	writer := io.MultiWriter(temp, hash)
	var downloaded int64
	buffer := make([]byte, 32*1024)
	for {
		read, readErr := body.Read(buffer)
		if read > 0 {
			if _, err := writer.Write(buffer[:read]); err != nil {
				temp.Close()
				return fmt.Errorf("write download: %w", err)
			}
			downloaded += int64(read)
			if progress != nil {
				percent := 0.0
				if total > 0 {
					percent = float64(downloaded) * 100 / float64(total)
				}
				progress(Progress{Message: "Downloading " + source.Name, Percent: percent, Bytes: downloaded, Total: total})
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			temp.Close()
			return fmt.Errorf("write download: %w", readErr)
		}
	}
	if downloaded == 0 {
		temp.Close()
		return fmt.Errorf("download was empty")
	}
	if err := temp.Close(); err != nil {
		return err
	}
	if actual := hex.EncodeToString(hash.Sum(nil)); !strings.EqualFold(actual, expected) {
		return fmt.Errorf("checksum mismatch for %s", source.Filename)
	}
	if err := os.Chmod(tempName, 0o700); err != nil {
		return err
	}
	return replaceWithFallback(tempName, target)
}

func (m *Manager) fetch(ctx context.Context, rawURL string) (io.ReadCloser, int64, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, 0, err
	}
	request.Header.Set("User-Agent", "ytaudio")
	response, err := m.client.Do(request)
	if err != nil {
		return nil, 0, err
	}
	if response.StatusCode != http.StatusOK {
		response.Body.Close()
		return nil, 0, fmt.Errorf("server returned %s", response.Status)
	}
	return response.Body, response.ContentLength, nil
}

func checksumFor(data io.Reader, filename string) (string, error) {
	scanner := bufio.NewScanner(data)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) == 2 && strings.TrimPrefix(fields[1], "*") == filename {
			if len(fields[0]) != sha256.Size*2 {
				return "", fmt.Errorf("invalid checksum for %s", filename)
			}
			return fields[0], nil
		}
	}
	if err := scanner.Err(); err != nil {
		return "", fmt.Errorf("read checksum list: %w", err)
	}
	return "", fmt.Errorf("checksum for %s not found", filename)
}

func replaceWithFallback(tempName, target string) error {
	backup := target + ".previous"
	if _, err := os.Stat(target); err == nil {
		if err := os.Remove(backup); err != nil && !os.IsNotExist(err) {
			return err
		}
		if err := os.Rename(target, backup); err != nil {
			return fmt.Errorf("preserve previous binary: %w", err)
		}
	}
	if err := os.Rename(tempName, target); err != nil {
		if _, backupErr := os.Stat(backup); backupErr == nil {
			_ = os.Rename(backup, target)
		}
		return fmt.Errorf("activate downloaded binary: %w", err)
	}
	return nil
}

func commandVersion(ctx context.Context, path string) string {
	return commandVersionWithArgs(ctx, path, "--version")
}

func commandVersionWithArgs(ctx context.Context, path string, args ...string) string {
	versionCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	output, err := process.Command(versionCtx, path, args...).CombinedOutput()
	if err != nil {
		return ""
	}
	return strings.SplitN(strings.TrimSpace(string(output)), "\n", 2)[0]
}

// CheckDaily compares the installed extraction engine with the current release.
// It never replaces a running tool. The caller schedules Update when idle.
func (m *Manager) CheckDaily(ctx context.Context) (bool, error) {
	marker := filepath.Join(m.dir, ".last-update-check")
	if data, err := os.ReadFile(marker); err == nil {
		if last, err := time.Parse(time.RFC3339, string(data)); err == nil && time.Since(last) < 24*time.Hour {
			return false, nil
		}
	}
	release, err := m.release(ctx, "yt-dlp/yt-dlp", "")
	if err != nil {
		return false, err
	}
	temp, err := os.CreateTemp(m.dir, ".check-*")
	if err != nil {
		return false, err
	}
	name := temp.Name()
	defer os.Remove(name)
	_, err = temp.WriteString(time.Now().UTC().Format(time.RFC3339))
	closeErr := temp.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return false, err
	}
	if err = os.Rename(name, marker); err != nil {
		return false, err
	}
	path := m.toolPath("yt-dlp")
	if path == "" {
		return false, nil
	}
	version := commandVersion(ctx, path)
	return version != "" && version < release.Tag, nil
}
