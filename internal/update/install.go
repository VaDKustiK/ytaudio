package update

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/VaDKustiK/ytaudio/internal/deps"
)

const maxAssetBytes = 128 << 20

var versionTag = regexp.MustCompile(`^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`)

func stableVersion(tag string) bool {
	_, valid := versionParts(tag)
	return valid && versionTag.MatchString(tag)
}

type Asset struct {
	Name   string `json:"name"`
	URL    string `json:"browser_download_url"`
	Digest string `json:"digest"`
	Size   int64  `json:"size"`
}

type Target struct {
	executable string
	format     string
}

func DetectTarget() Target {
	executable, err := os.Executable()
	if err != nil {
		return Target{}
	}
	executable, err = filepath.EvalSymlinks(executable)
	if err != nil {
		return Target{}
	}
	destination, format := installLocation(executable)
	// PackageKit handles writes to system-owned Linux installations.
	if format != "" && (format == ".deb" || format == ".rpm" || format == ".pkg.tar.zst" || writable(filepath.Dir(destination))) {
		return Target{executable: executable, format: format}
	}
	return Target{}
}

func writable(dir string) bool {
	file, err := os.CreateTemp(dir, ".ytaudio-write-")
	if err != nil {
		return false
	}
	file.Close()
	os.Remove(file.Name())
	return true
}

func (t Target) Asset(info Info) (Asset, bool) {
	if t.format == "" || !info.Ready || !stableVersion(info.Latest) {
		return Asset{}, false
	}
	name := fmt.Sprintf("ytaudio-%s-%s-%s%s", info.Latest[1:], runtime.GOOS, runtime.GOARCH, t.format)
	expectedURL := "https://github.com/" + repository + "/releases/download/" + info.Latest + "/" + name
	for _, asset := range info.Assets {
		if asset.Name == name && asset.URL == expectedURL && asset.Size > 0 && asset.Size <= maxAssetBytes && validDigest(asset.Digest) {
			return asset, true
		}
	}
	return Asset{}, false
}

func validDigest(digest string) bool {
	if !strings.HasPrefix(digest, "sha256:") || len(digest) != 71 {
		return false
	}
	_, err := hex.DecodeString(digest[7:])
	return err == nil
}

type plan struct {
	Executable string
	Format     string
	Version    string
	Asset      Asset
}

func cacheRoot() (string, error) {
	root := os.Getenv("YTAUDIO_CACHE_DIR")
	if root == "" {
		var err error
		root, err = os.UserCacheDir()
		if err != nil {
			return "", err
		}
	}
	return filepath.Join(root, "ytaudio", "app-updates"), nil
}

// Stage downloads into a private directory and starts a helper that waits for
// this process to exit. The caller quits the desktop only after Stage succeeds.
func (t Target) Stage(ctx context.Context, info Info, proxy string, report func(int)) error {
	asset, ok := t.Asset(info)
	if !ok {
		return fmt.Errorf("no verified update is available for this installation")
	}
	root, err := cacheRoot()
	if err != nil {
		return err
	}
	if err = os.MkdirAll(root, 0700); err != nil {
		return err
	}
	dir, err := os.MkdirTemp(root, "update-")
	if err != nil {
		return err
	}
	handedOff := false
	defer func() {
		if !handedOff {
			os.RemoveAll(dir)
		}
	}()
	client, err := deps.HTTPClient(proxy)
	if err != nil {
		return err
	}
	defer client.CloseIdleConnections()
	client.CheckRedirect = releaseRedirect
	if err = downloadAsset(ctx, client, asset, filepath.Join(dir, asset.Name), report); err != nil {
		return err
	}
	data, err := json.Marshal(plan{t.executable, t.format, info.Latest, asset})
	if err != nil {
		return err
	}
	if err = os.WriteFile(filepath.Join(dir, "plan.json"), data, 0600); err != nil {
		return err
	}
	helper := filepath.Join(dir, filepath.Base(t.executable))
	if err = copyExecutable(t.executable, helper); err != nil {
		return err
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	cmd := childCommand(helper, "--apply-update", strconv.Itoa(os.Getpid()))
	cmd.Dir = dir
	if err = cmd.Start(); err != nil {
		return err
	}
	go func() { _ = cmd.Wait() }()
	handedOff = true
	return nil
}

func releaseRedirect(req *http.Request, via []*http.Request) error {
	host := req.URL.Host
	if len(via) >= 5 || req.URL.Scheme != "https" || req.URL.User != nil ||
		(host != "github.com" && host != "release-assets.githubusercontent.com" && host != "objects.githubusercontent.com") {
		return fmt.Errorf("untrusted release download redirect")
	}
	return nil
}

func downloadAsset(ctx context.Context, client *http.Client, asset Asset, path string, report func(int)) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, asset.URL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept-Encoding", "identity")
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("could not download the app update. Check your connection and try again")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("app update download returned HTTP %d", resp.StatusCode)
	}

	out, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0700)
	if err != nil {
		return err
	}
	hash := sha256.New()
	var total int64
	last := -1
	buffer := make([]byte, 64<<10)
	writer := io.MultiWriter(out, hash)
	reader := io.LimitReader(resp.Body, asset.Size+1)
	for {
		n, readErr := reader.Read(buffer)
		if n > 0 {
			total += int64(n)
			if _, err = writer.Write(buffer[:n]); err != nil {
				out.Close()
				return err
			}
			percent := min(99, int(total*100/asset.Size))
			if percent != last {
				last = percent
				report(percent)
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			out.Close()
			return fmt.Errorf("the update download was interrupted. Try again")
		}
	}
	if err = out.Close(); err != nil {
		return err
	}
	if total != asset.Size || !strings.EqualFold("sha256:"+hex.EncodeToString(hash.Sum(nil)), asset.Digest) {
		return fmt.Errorf("the app update failed its integrity check. Try again later")
	}
	return nil
}

func copyExecutable(source, destination string) error {
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	output, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0755)
	if err != nil {
		return err
	}
	_, err = io.Copy(output, input)
	closeErr := output.Close()
	if err != nil {
		return err
	}
	return closeErr
}

func versionOf(executable string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cmd := childCommand(executable, "--version")
	cmd.Dir = filepath.Dir(executable)
	// Use CommandContext for this bounded, read-only probe while retaining
	// platform-specific process attributes.
	probe := exec.CommandContext(ctx, cmd.Path, cmd.Args[1:]...)
	probe.Dir, probe.SysProcAttr = cmd.Dir, cmd.SysProcAttr
	out, err := probe.Output()
	return strings.TrimSpace(string(out)), err
}
