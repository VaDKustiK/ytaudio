package deps

// Asset names verified against the respective GitHub release APIs. Runtime
// resolution fixes the release once and requires GitHub's SHA-256 asset digest.
import (
	"archive/zip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

type ReleaseAsset struct {
	Name   string `json:"name"`
	URL    string `json:"browser_download_url"`
	Digest string `json:"digest"`
}
type Release struct {
	Tag    string         `json:"tag_name"`
	Assets []ReleaseAsset `json:"assets"`
}

func (m *Manager) release(ctx context.Context, repo, tag string) (Release, error) {
	endpoint := "https://api.github.com/repos/" + repo + "/releases/"
	if tag == "" {
		endpoint += "latest"
	} else {
		endpoint += "tags/" + tag
	}
	body, _, err := m.fetch(ctx, endpoint)
	if err != nil {
		return Release{}, err
	}
	defer body.Close()
	var release Release
	err = json.NewDecoder(io.LimitReader(body, 4<<20)).Decode(&release)
	return release, err
}

func (m *Manager) installAsset(ctx context.Context, repo, tag, asset, name string, progress func(Progress)) error {
	release, err := m.release(ctx, repo, tag)
	if repo == "eugeneware/ffmpeg-static" && ffmpegPins[asset] != "" {
		release = Release{Tag: tag, Assets: []ReleaseAsset{{Name: asset, URL: "https://github.com/" + repo + "/releases/download/" + tag + "/" + asset, Digest: "sha256:" + ffmpegPins[asset]}}}
		err = nil
	}
	if err != nil {
		if repo == "denoland/deno" {
			return m.installDenoDirect(ctx, tag, asset, name, progress)
		}
		return err
	}
	for _, a := range release.Assets {
		if a.Name != asset {
			continue
		}
		digest := strings.TrimPrefix(a.Digest, "sha256:")
		if len(digest) != 64 {
			return fmt.Errorf("no trusted SHA-256 digest for %s", asset)
		}
		target := filepath.Join(m.dir, executableName(name))
		if strings.HasSuffix(asset, ".zip") {
			archive, err := os.CreateTemp(m.dir, ".archive-*")
			if err != nil {
				return err
			}
			p := archive.Name()
			archive.Close()
			defer os.Remove(p)
			if err = m.downloadVerified(ctx, Source{Name: name, URL: a.URL, Filename: asset, SHA256: digest}, p, progress); err != nil {
				return err
			}
			return extractZip(p, m.dir, []string{executableName(name)})
		}
		return m.downloadVerified(ctx, Source{Name: name, URL: a.URL, Filename: asset, SHA256: digest}, target, progress)
	}
	return fmt.Errorf("%s release does not provide %s", repo, asset)
}

func extractZip(archive, dir string, names []string) error {
	z, err := zip.OpenReader(archive)
	if err != nil {
		return err
	}
	defer z.Close()
	for _, name := range names {
		found := false
		for _, f := range z.File {
			if filepath.Base(f.Name) != name || f.FileInfo().IsDir() {
				continue
			}
			if f.UncompressedSize64 > 512<<20 {
				return fmt.Errorf("archive executable too large")
			}
			reader, err := f.Open()
			if err != nil {
				return err
			}
			temp, err := os.CreateTemp(dir, ".extract-*")
			if err != nil {
				reader.Close()
				return err
			}
			p := temp.Name()
			_, err = io.Copy(temp, io.LimitReader(reader, 512<<20+1))
			reader.Close()
			closeErr := temp.Close()
			if err == nil {
				err = closeErr
			}
			if err == nil {
				err = os.Chmod(p, 0o700)
			}
			if err == nil {
				err = replaceWithFallback(p, filepath.Join(dir, name))
			}
			os.Remove(p)
			if err != nil {
				return err
			}
			found = true
			break
		}
		if !found {
			return fmt.Errorf("%s missing from archive", name)
		}
	}
	return nil
}

func (m *Manager) installTool(ctx context.Context, name string, progress func(Progress)) error {
	arch := "x86_64"
	if runtime.GOARCH == "arm64" {
		arch = "aarch64"
	}
	switch name {
	case "deno":
		target := map[string]string{"linux": "unknown-linux-gnu", "darwin": "apple-darwin", "windows": "pc-windows-msvc"}[runtime.GOOS]
		return m.installAsset(ctx, "denoland/deno", "v2.9.8", "deno-"+arch+"-"+target+".zip", name, progress)
	case "ffmpeg", "ffprobe":
		if runtime.GOOS == "windows" && runtime.GOARCH == "arm64" {
			release, err := m.release(ctx, "yt-dlp/FFmpeg-Builds", "latest")
			if err != nil {
				return err
			}
			for _, a := range release.Assets {
				if a.Name != "ffmpeg-master-latest-winarm64-gpl.zip" {
					continue
				}
				temp, err := os.CreateTemp(m.dir, ".ffmpeg-*")
				if err != nil {
					return err
				}
				p := temp.Name()
				temp.Close()
				defer os.Remove(p)
				if !strings.HasPrefix(a.Digest, "sha256:") {
					return fmt.Errorf("FFmpeg checksum unavailable")
				}
				if err = m.downloadVerified(ctx, Source{Name: "FFmpeg", URL: a.URL, Filename: a.Name, SHA256: strings.TrimPrefix(a.Digest, "sha256:")}, p, progress); err != nil {
					return err
				}
				return extractZip(p, m.dir, []string{"ffmpeg.exe", "ffprobe.exe"})
			}
			return fmt.Errorf("windows ARM64 FFmpeg asset unavailable")
		}
		platform := runtime.GOOS
		a := "x64"
		if runtime.GOARCH == "arm64" {
			a = "arm64"
		}
		if platform == "windows" {
			platform = "win32"
		}
		return m.installAsset(ctx, "eugeneware/ffmpeg-static", "b6.1.1", name+"-"+platform+"-"+a, name, progress)
	}
	return fmt.Errorf("unknown tool %s", name)
}

func HTTPClient(proxy string) (*http.Client, error) {
	return newHTTPClient(proxy)
}

func (m *Manager) installDenoDirect(ctx context.Context, tag, asset, name string, progress func(Progress)) error {
	base := "https://github.com/denoland/deno/releases/download/" + tag + "/"
	f, err := os.CreateTemp(m.dir, ".deno-*")
	if err != nil {
		return err
	}
	p := f.Name()
	f.Close()
	defer os.Remove(p)
	if err = m.downloadVerified(ctx, Source{Name: name, URL: base + asset, ChecksumsURL: base + asset + ".sha256sum", Filename: asset}, p, progress); err != nil {
		return err
	}
	return extractZip(p, m.dir, []string{executableName(name)})
}
