package analyzer

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/VaDKustiK/ytaudio/internal/deps"
)

type Manifest struct {
	Engine  string            `json:"engine_version,omitempty"`
	Version string            `json:"version"`
	Models  string            `json:"models_version"`
	Files   map[string]string `json:"files"`
}
type installation struct {
	Engine string `json:"engine"`
	Models string `json:"models"`
}

func safeName(name string) bool {
	return fs.ValidPath(name) && !strings.ContainsAny(name, "\\:") && filepath.IsLocal(filepath.FromSlash(name))
}
func verifyManifest(ctx context.Context, dir string) error {
	data, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if err != nil {
		return err
	}
	if len(data) > 128<<10 {
		return fmt.Errorf("manifest too large")
	}
	var manifest Manifest
	if json.Unmarshal(data, &manifest) != nil || manifest.Version != manifestVersion || manifest.Models != ModelsVersion || len(manifest.Files) == 0 || len(manifest.Files) > 128 {
		return fmt.Errorf("invalid component manifest")
	}
	for name, want := range manifest.Files {
		if !safeName(name) || len(want) != 64 {
			return fmt.Errorf("unsafe component manifest")
		}
		path := filepath.Join(dir, filepath.FromSlash(name))
		info, e := os.Lstat(path)
		if e != nil || !info.Mode().IsRegular() {
			return fmt.Errorf("missing or unsafe component file")
		}
		got, e := hashFile(ctx, path)
		if e != nil {
			return e
		}
		if got != want {
			return fmt.Errorf("component checksum mismatch")
		}
	}
	return nil
}
func verifyComponent(ctx context.Context, dir string, models bool) error {
	if err := verifyManifest(ctx, dir); err != nil {
		return err
	}
	data, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if err != nil {
		return err
	}
	var manifest Manifest
	if err = json.Unmarshal(data, &manifest); err != nil {
		return err
	}
	if models {
		for name, digest := range modelDigests {
			if manifest.Files[name] != digest {
				return fmt.Errorf("unexpected model artifact")
			}
		}
	} else if manifest.Engine != Version || manifest.Files[executableName()] == "" {
		return fmt.Errorf("analyzer executable missing from manifest")
	}
	return nil
}
func (m *Manager) paths() (string, string, error) {
	if m.local != "" {
		root, err := filepath.Abs(m.local)
		return filepath.Join(root, executableName()), filepath.Join(root, "models"), err
	}
	data, err := os.ReadFile(filepath.Join(m.root, "installed.json"))
	if err != nil {
		return "", "", fmt.Errorf("install the analyzer component first")
	}
	var i installation
	if json.Unmarshal(data, &i) != nil || !safeName(i.Engine) || !safeName(i.Models) {
		return "", "", fmt.Errorf("analyzer installation needs repair")
	}
	return filepath.Join(m.root, filepath.FromSlash(i.Engine), executableName()), filepath.Join(m.root, filepath.FromSlash(i.Models)), nil
}
func (m *Manager) available() bool {
	executable, models, err := m.paths()
	if err != nil {
		return false
	}
	for _, path := range []string{executable, filepath.Join(models, "beat_this.onnx"), filepath.Join(models, "keynet.onnx"), filepath.Join(filepath.Dir(executable), "manifest.json"), filepath.Join(models, "manifest.json")} {
		if info, e := os.Stat(path); e != nil || !info.Mode().IsRegular() {
			return false
		}
	}
	for _, directory := range []string{filepath.Dir(executable), models} {
		data, e := os.ReadFile(filepath.Join(directory, "manifest.json"))
		if e != nil || len(data) > 128<<10 {
			return false
		}
		var manifest Manifest
		if json.Unmarshal(data, &manifest) != nil || manifest.Version != manifestVersion || manifest.Models != ModelsVersion || (directory != models && manifest.Engine != Version) {
			return false
		}
	}
	return true
}
func (m *Manager) Install() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return fmt.Errorf("analyzer is closed")
	}
	if m.state.Installing || m.state.Status == "running" || m.state.Status == "queued" {
		return fmt.Errorf("analyzer is busy")
	}
	if m.release == "dev" {
		return fmt.Errorf("local build: prepare the analyzer with native-analyzer/tools/package.py and set YTAUDIO_ANALYZER_DIR")
	}
	proxy := m.proxy
	m.state.Installing = true
	m.state.InstallError = ""
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		ctx, cancel := context.WithTimeout(m.ctx, 20*time.Minute)
		defer cancel()
		err := m.install(ctx, proxy)
		m.mu.Lock()
		defer m.mu.Unlock()
		m.state.Installing = false
		m.state.Ready = m.available()
		if err != nil {
			m.state.InstallError = err.Error()
		}
	}()
	return nil
}
func (m *Manager) install(ctx context.Context, proxy string) error {
	client, err := deps.HTTPClient(proxy)
	if err != nil {
		return err
	}
	client.Timeout = 20 * time.Minute
	return m.installWithClient(ctx, client)
}

func (m *Manager) installWithClient(ctx context.Context, client *http.Client) error {
	endpoint := "https://api.github.com/repos/VaDKustiK/ytaudio/releases/tags/" + m.release
	request, err := http.NewRequestWithContext(ctx, "GET", endpoint, nil)
	if err != nil {
		return err
	}
	request.Header.Set("User-Agent", "ytaudio-analyzer/1")
	response, err := client.Do(request)
	if err != nil {
		return fmt.Errorf("cannot contact the release service; try again when online")
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		return fmt.Errorf("analyzer release lookup returned HTTP %d; try again later", response.StatusCode)
	}
	var release deps.Release
	if err = json.NewDecoder(io.LimitReader(response.Body, 4<<20)).Decode(&release); err != nil {
		return err
	}
	if release.Tag != m.release {
		return fmt.Errorf("unexpected analyzer release")
	}
	target := runtime.GOOS + "-" + runtime.GOARCH
	engineName := "ytaudio-analyzer-" + Version + "-" + target + ".zip"
	modelName := "ytaudio-models-" + ModelsVersion + ".zip"
	var installed installation
	for _, name := range []string{modelName, engineName} {
		// Unchanged models survive app updates and need no network download.
		if name == modelName {
			directory := filepath.Join(m.root, "models", ModelsVersion)
			if verifyComponent(ctx, directory, true) == nil {
				installed.Models = "models/" + ModelsVersion
				continue
			}
		}
		var asset deps.ReleaseAsset
		for _, item := range release.Assets {
			if item.Name == name {
				asset = item
				break
			}
		}
		if !strings.HasPrefix(asset.Digest, "sha256:") || len(asset.Digest) != 71 {
			return fmt.Errorf("verified analyzer package is not available for this release")
		}
		if !strings.HasPrefix(asset.URL, "https://github.com/VaDKustiK/ytaudio/releases/download/"+m.release+"/") {
			return fmt.Errorf("untrusted analyzer download")
		}
		staging, e := os.MkdirTemp(m.root, ".install-")
		if e != nil {
			return e
		}
		e = func() error {
			defer os.RemoveAll(staging)
			archive := filepath.Join(staging, "download.zip")
			if err := download(ctx, client, asset, archive); err != nil {
				return err
			}
			directory := filepath.Join(staging, "component")
			if err := extractArchive(archive, directory); err != nil {
				return err
			}
			if err := verifyComponent(ctx, directory, name == modelName); err != nil {
				return err
			}
			relative := "engines/" + Version + "-" + target + "-" + strings.TrimPrefix(asset.Digest, "sha256:")[:16]
			if name == modelName {
				relative = "models/" + ModelsVersion
				installed.Models = relative
			} else {
				installed.Engine = relative
			}
			destination := filepath.Join(m.root, filepath.FromSlash(relative))
			if err := os.MkdirAll(filepath.Dir(destination), 0700); err != nil {
				return err
			}
			if verifyComponent(ctx, destination, name == modelName) == nil {
				return nil
			}
			// A damaged prior directory is renamed aside, never overwritten in place.
			backup := destination + ".old-" + token()
			old := false
			if _, err := os.Stat(destination); err == nil {
				if err = os.Rename(destination, backup); err != nil {
					return err
				}
				old = true
			}
			if err := os.Rename(directory, destination); err != nil {
				if old {
					_ = os.Rename(backup, destination)
				}
				return err
			}
			if old {
				_ = os.RemoveAll(backup)
			}
			return nil
		}()
		if e != nil {
			return e
		}
	}
	if installed.Engine == "" || installed.Models == "" {
		return fmt.Errorf("incomplete analyzer package")
	}
	data, _ := json.Marshal(installed)
	temp, err := os.CreateTemp(m.root, ".installed-")
	if err != nil {
		return err
	}
	name := temp.Name()
	defer os.Remove(name)
	if _, err = temp.Write(data); err != nil {
		temp.Close()
		return err
	}
	if err = temp.Close(); err != nil {
		return err
	}
	return os.Rename(name, filepath.Join(m.root, "installed.json"))
}
func download(ctx context.Context, client *http.Client, asset deps.ReleaseAsset, path string) error {
	req, err := http.NewRequestWithContext(ctx, "GET", asset.URL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "ytaudio-analyzer/1")
	response, err := client.Do(req)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		return fmt.Errorf("analyzer download returned HTTP %d", response.StatusCode)
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(f, h), io.LimitReader(response.Body, (512<<20)+1))
	if err != nil {
		return err
	}
	if n > 512<<20 {
		return fmt.Errorf("analyzer archive too large")
	}
	if hex.EncodeToString(h.Sum(nil)) != strings.TrimPrefix(asset.Digest, "sha256:") {
		return fmt.Errorf("analyzer download checksum mismatch")
	}
	return nil
}
func extractArchive(archive, directory string) error {
	z, err := zip.OpenReader(archive)
	if err != nil {
		return err
	}
	defer z.Close()
	if len(z.File) > 128 {
		return fmt.Errorf("too many analyzer archive entries")
	}
	var total uint64
	seen := map[string]bool{}
	for _, item := range z.File {
		name := item.Name
		if !safeName(name) || item.FileInfo().Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("unsafe analyzer archive path")
		}
		if item.FileInfo().IsDir() {
			continue
		}
		if seen[strings.ToLower(name)] {
			return fmt.Errorf("duplicate analyzer archive path")
		}
		seen[strings.ToLower(name)] = true
		total += item.UncompressedSize64
		if total > 512<<20 || item.UncompressedSize64 > 256<<20 {
			return fmt.Errorf("analyzer archive exceeds size limit")
		}
		destination := filepath.Join(directory, filepath.FromSlash(name))
		if err = os.MkdirAll(filepath.Dir(destination), 0700); err != nil {
			return err
		}
		err = func() error {
			input, e := item.Open()
			if e != nil {
				return e
			}
			defer input.Close()
			output, e := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0700)
			if e != nil {
				return e
			}
			_, e = io.Copy(output, io.LimitReader(input, int64(item.UncompressedSize64)+1))
			ce := output.Close()
			if e != nil {
				return e
			}
			return ce
		}()
		if err != nil {
			return err
		}
	}
	return nil
}
