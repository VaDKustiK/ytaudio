package update

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// RunHelper runs in a private copy of the old executable after user approval.
func RunHelper(parentPID int, currentVersion string) error {
	self, err := os.Executable()
	if err != nil {
		return err
	}
	dir, err := updateDirectory(filepath.Base(filepath.Dir(self)))
	if err != nil || !samePath(dir, filepath.Dir(self)) {
		return fmt.Errorf("invalid app update staging directory")
	}
	pending := filepath.Join(dir, "plan.json")
	claimed := filepath.Join(dir, "applying.json")
	if err = os.Rename(pending, claimed); err != nil {
		return err
	}
	data, err := os.ReadFile(claimed)
	if err != nil {
		return err
	}
	var p plan
	if json.Unmarshal(data, &p) != nil || !stableVersion(p.Version) || !newer(currentVersion, p.Version) || !filepath.IsAbs(p.Executable) {
		return fmt.Errorf("invalid app update plan")
	}
	target := Target{executable: p.Executable, format: p.Format}
	if _, ok := target.Asset(Info{Ready: true, Latest: p.Version, Assets: []Asset{p.Asset}}); !ok {
		return fmt.Errorf("invalid app update asset")
	}
	if err = waitForExit(parentPID); err != nil {
		return err
	}
	_, format := installLocation(p.Executable)
	outcome := "installed"
	if installed, e := versionOf(p.Executable); format != p.Format || e != nil || installed != currentVersion {
		outcome = "failed"
	} else if err = applyVerified(p, dir); err != nil {
		fmt.Fprintln(os.Stderr, "app update:", err)
		outcome = "failed"
	}
	cmd := childCommand(p.Executable, "--update-result", outcome, "--update-cleanup", filepath.Base(dir))
	cmd.Dir = filepath.Dir(p.Executable)
	if err = cmd.Start(); err != nil {
		return fmt.Errorf("could not reopen ytaudio: %w", err)
	}
	return cmd.Process.Release()
}

func updateDirectory(name string) (string, error) {
	if !strings.HasPrefix(name, "update-") || !fs.ValidPath(name) ||
		strings.ContainsAny(name, "/\\:") || filepath.Base(name) != name {
		return "", fmt.Errorf("invalid app update directory")
	}
	root, err := cacheRoot()
	if err != nil {
		return "", err
	}
	root, err = filepath.Abs(root)
	if err != nil {
		return "", err
	}
	dir := filepath.Join(root, name)
	stat, err := os.Lstat(dir)
	if err != nil || !stat.IsDir() {
		return "", fmt.Errorf("app update directory is missing")
	}
	return dir, nil
}

func Cleanup(name string) {
	dir, err := updateDirectory(name)
	if err != nil {
		return
	}
	// The helper may still hold its executable open on Windows.
	for range 20 {
		if os.RemoveAll(dir) == nil {
			return
		}
		time.Sleep(250 * time.Millisecond)
	}
}

func applyVerified(p plan, dir string) error {
	archive := filepath.Join(dir, p.Asset.Name)
	if err := verifyFile(archive, p.Asset); err != nil {
		return err
	}
	switch p.Format {
	case ".zip", ".tar.gz":
		return replaceFromArchive(p, archive)
	default:
		return installPackage(p, archive)
	}
}

func verifyFile(path string, asset Asset) error {
	input, err := os.Open(path)
	if err != nil {
		return err
	}
	defer input.Close()
	digest := sha256.New()
	size, err := io.Copy(digest, io.LimitReader(input, asset.Size+1))
	if err != nil || size != asset.Size || !strings.EqualFold(fmt.Sprintf("sha256:%x", digest.Sum(nil)), asset.Digest) {
		return fmt.Errorf("staged update failed its integrity check")
	}
	return nil
}

func replaceFromArchive(p plan, archive string) error {
	destination, _ := installLocation(p.Executable)
	staging, err := os.MkdirTemp(filepath.Dir(destination), ".ytaudio-update-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(staging)
	incoming := filepath.Join(staging, filepath.Base(destination))
	probe := incoming
	base := strings.TrimSuffix(p.Asset.Name, p.Format)
	switch {
	case strings.HasSuffix(destination, ".app") && p.Executable != destination:
		err = extractBundle(archive, incoming)
		probe = filepath.Join(incoming, "Contents", "MacOS", "ytaudio")
		if err == nil {
			err = verifyBundle(incoming)
		}
	case p.Format == ".zip":
		err = extractZipExe(archive, base+"/ytaudio.exe", incoming)
	default:
		err = extractTarExe(archive, base+"/ytaudio", incoming)
	}
	if err != nil {
		return err
	}
	if got, e := versionOf(probe); e != nil || got != p.Version {
		return fmt.Errorf("update executable did not report %s", p.Version)
	}
	return replaceApplication(incoming, destination)
}

func extractZipExe(archive, expected, destination string) error {
	z, err := zip.OpenReader(archive)
	if err != nil {
		return err
	}
	defer z.Close()
	for _, entry := range z.File {
		if entry.Name == expected && entry.Mode().IsRegular() && entry.UncompressedSize64 <= maxAssetBytes {
			input, err := entry.Open()
			if err != nil {
				return err
			}
			defer input.Close()
			return writeMember(input, destination, 0755, int64(entry.UncompressedSize64))
		}
	}
	return fmt.Errorf("update executable is missing")
}

func extractTarExe(archive, expected, destination string) error {
	file, err := os.Open(archive)
	if err != nil {
		return err
	}
	defer file.Close()
	gz, err := gzip.NewReader(file)
	if err != nil {
		return err
	}
	defer gz.Close()
	tarball := tar.NewReader(io.LimitReader(gz, 2*maxAssetBytes))
	for {
		entry, err := tarball.Next()
		if err != nil {
			return fmt.Errorf("update executable is missing: %w", err)
		}
		if entry.Name == expected && entry.Typeflag == tar.TypeReg && entry.Size <= maxAssetBytes {
			return writeMember(tarball, destination, 0755, entry.Size)
		}
	}
}

func extractBundle(archive, destination string) error {
	z, err := zip.OpenReader(archive)
	if err != nil {
		return err
	}
	defer z.Close()
	var total uint64
	for _, entry := range z.File {
		if entry.FileInfo().IsDir() {
			continue
		}
		name, ok := strings.CutPrefix(entry.Name, "ytaudio.app/")
		if !ok || !fs.ValidPath(name) || name == "." || strings.ContainsAny(name, "\\:") || !entry.Mode().IsRegular() ||
			entry.UncompressedSize64 > uint64(2*maxAssetBytes)-total {
			return fmt.Errorf("invalid app bundle archive")
		}
		total += entry.UncompressedSize64
		path := filepath.Join(destination, filepath.FromSlash(name))
		if err = os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			return err
		}
		input, err := entry.Open()
		if err != nil {
			return err
		}
		err = writeMember(input, path, entry.Mode().Perm(), int64(entry.UncompressedSize64))
		input.Close()
		if err != nil {
			return err
		}
	}
	return nil
}

func writeMember(input io.Reader, path string, mode fs.FileMode, size int64) error {
	out, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if err != nil {
		return err
	}
	written, err := io.Copy(out, io.LimitReader(input, size+1))
	closeErr := out.Close()
	if err != nil {
		return err
	}
	if written != size {
		return fmt.Errorf("update archive entry has an unexpected size")
	}
	return closeErr
}
