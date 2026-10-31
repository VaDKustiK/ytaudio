package deps

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type Manager struct {
	dir    string
	client *http.Client
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
	cache, err := os.UserCacheDir()
	if err != nil {
		return nil, fmt.Errorf("find user cache directory: %w", err)
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	if proxy != "" {
		parsed, err := url.Parse(proxy)
		if err != nil || parsed.Scheme == "" || parsed.Host == "" {
			return nil, fmt.Errorf("invalid proxy URL")
		}
		transport.Proxy = http.ProxyURL(parsed)
	}
	manager := &Manager{
		dir: filepath.Join(cache, "ytaudio", "bin"),
		// The setup operation supplies its own overall context timeout. A
		// short client timeout incorrectly aborts large downloads on slow links.
		client: &http.Client{Transport: transport},
	}
	if err := os.MkdirAll(manager.dir, 0o700); err != nil {
		return nil, fmt.Errorf("create dependency directory: %w", err)
	}
	if err := installBundled(manager.dir); err != nil {
		return nil, err
	}
	if err := prependPath(manager.dir); err != nil {
		return nil, err
	}
	return manager, nil
}

func prependPath(dir string) error {
	pathSeparator := string(os.PathListSeparator)
	current := os.Getenv("PATH")
	if current == "" {
		return os.Setenv("PATH", dir)
	}
	return os.Setenv("PATH", dir+pathSeparator+current)
}

func (m *Manager) Status(ctx context.Context) []Status {
	sources, err := Sources()
	if err != nil {
		return []Status{{Name: "yt-dlp", Error: err.Error()}}
	}
	statuses := make([]Status, 0, len(sources)+1)
	for _, source := range sources {
		path := filepath.Join(m.dir, source.Executable)
		status := Status{Name: source.Name, Path: path}
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			status.Ready = true
			status.Version = commandVersion(ctx, path)
		} else if err != nil && !errors.Is(err, os.ErrNotExist) {
			status.Error = err.Error()
		}
		statuses = append(statuses, status)
	}
	ffmpegPath, err := exec.LookPath("ffmpeg")
	ffmpeg := Status{Name: "ffmpeg"}
	if err == nil {
		ffmpeg.Path = ffmpegPath
		ffmpeg.Version = commandVersionWithArgs(ctx, ffmpegPath, "-version")
		ffmpeg.Ready = ffmpeg.Version != ""
		if !ffmpeg.Ready {
			ffmpeg.Error = "ffmpeg could not be executed"
		}
	} else {
		ffmpeg.Error = "ffmpeg is not installed"
	}
	statuses = append(statuses, ffmpeg)
	denoPath, err := exec.LookPath("deno")
	deno := Status{Name: "deno"}
	if err == nil {
		deno.Path = denoPath
		deno.Version = commandVersion(ctx, denoPath)
		deno.Ready = deno.Version != ""
	} else {
		deno.Error = "Deno is not installed"
	}
	statuses = append(statuses, deno)
	return statuses
}

func (m *Manager) YTDLPPath() (string, error) {
	sources, err := Sources()
	if err != nil {
		return "", err
	}
	path := filepath.Join(m.dir, sources[0].Executable)
	if _, err := os.Stat(path); err != nil {
		return "", fmt.Errorf("yt-dlp is not ready")
	}
	return path, nil
}

func (m *Manager) Setup(ctx context.Context, progress func(Progress)) error {
	sources, err := Sources()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(m.dir, 0o700); err != nil {
		return fmt.Errorf("create dependency directory: %w", err)
	}
	for _, source := range sources {
		target := filepath.Join(m.dir, source.Executable)
		if _, err := os.Stat(target); err == nil {
			continue
		}
		if progress != nil {
			progress(Progress{Message: "Downloading " + source.Name})
		}
		if err := m.downloadVerified(ctx, source, target, progress); err != nil {
			return fmt.Errorf("%s setup failed: %w", source.Name, err)
		}
	}
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		return fmt.Errorf("ffmpeg is not installed; install ffmpeg or place it on PATH")
	}
	if version := commandVersionWithArgs(ctx, "ffmpeg", "-version"); version == "" {
		return fmt.Errorf("ffmpeg was found but could not be executed")
	}
	if _, err := exec.LookPath("deno"); err != nil {
		return fmt.Errorf("Deno 2.3 or newer is required for YouTube extraction; install Deno and retry setup")
	}
	if version := commandVersionWithArgs(ctx, "deno", "--version"); version == "" {
		return fmt.Errorf("Deno was found but could not be executed")
	}
	return nil
}

func (m *Manager) Update(ctx context.Context) error {
	sources, err := Sources()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(m.dir, 0o700); err != nil {
		return err
	}
	for _, source := range sources {
		if err := m.downloadVerified(ctx, source, filepath.Join(m.dir, source.Executable), nil); err != nil {
			return err
		}
	}
	return nil
}

func (m *Manager) downloadVerified(ctx context.Context, source Source, target string, progress func(Progress)) error {
	checksumBody, _, err := m.fetch(ctx, source.ChecksumsURL)
	if err != nil {
		return fmt.Errorf("download checksum list: %w", err)
	}
	defer checksumBody.Close()
	expected, err := checksumFor(checksumBody, source.Filename)
	if err != nil {
		return err
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
	output, err := exec.CommandContext(versionCtx, path, args...).CombinedOutput()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(output))
}
