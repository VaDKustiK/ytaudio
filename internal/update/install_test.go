package update

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func releaseFixture(format, body string) Asset {
	name := fmt.Sprintf("ytaudio-1.1.1-%s-%s%s", runtime.GOOS, runtime.GOARCH, format)
	return Asset{Name: name, URL: "https://github.com/" + repository + "/releases/download/v1.1.1/" + name,
		Size: int64(len(body)), Digest: fmt.Sprintf("sha256:%x", sha256.Sum256([]byte(body)))}
}

func TestTargetRequiresExactVerifiedReleaseAsset(t *testing.T) {
	target := Target{format: ".zip"}
	asset := releaseFixture(target.format, "payload")
	for _, tc := range []struct {
		name   string
		change func(*Asset)
	}{
		{"valid", nil},
		{"wrong target", func(a *Asset) { a.Name = "ytaudio-other.zip" }},
		{"untrusted URL", func(a *Asset) { a.URL = "https://example.invalid/" + a.Name }},
		{"wrong version", func(a *Asset) { a.URL = strings.Replace(a.URL, "/v1.1.1/", "/v1.1.0/", 1) }},
		{"missing digest", func(a *Asset) { a.Digest = "" }},
		{"invalid digest", func(a *Asset) { a.Digest = "sha256:" + strings.Repeat("x", 64) }},
		{"oversized", func(a *Asset) { a.Size = maxAssetBytes + 1 }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			candidate := asset
			if tc.change != nil {
				tc.change(&candidate)
			}
			_, ok := target.Asset(Info{Ready: true, Latest: "v1.1.1", Assets: []Asset{candidate}})
			if ok != (tc.change == nil) {
				t.Fatal("incorrect release asset decision")
			}
		})
	}
}

func TestReleaseRedirectRejectsUnexpectedOrigins(t *testing.T) {
	for _, u := range []string{"https://release-assets.githubusercontent.com/file", "http://github.com/file", "https://github.com.example.invalid/file", "https://github.com:443/file", "https://example.invalid/file"} {
		parsed, _ := url.Parse(u)
		if got := releaseRedirect(&http.Request{URL: parsed}, nil); (got == nil) != (parsed.Host == "release-assets.githubusercontent.com") {
			t.Fatalf("incorrect redirect decision for %s: %v", u, got)
		}
	}
}

func TestDownloadAssetRejectsCorruptionAndTruncation(t *testing.T) {
	const contents = "verified package"
	asset := releaseFixture(".zip", contents)
	for _, body := range []string{contents, "altered package!", contents[:4], contents + "extra"} {
		client := &http.Client{Transport: transportFunc(func(req *http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
		})}
		path := filepath.Join(t.TempDir(), asset.Name)
		err := downloadAsset(context.Background(), client, asset, path, func(int) {})
		if (err == nil) != (body == contents) {
			t.Fatalf("download %q: unexpected result %v", body, err)
		}
		if err == nil {
			if err = verifyFile(path, asset); err != nil {
				t.Fatal(err)
			}
			if err = os.WriteFile(path, []byte("tampered payload"), 0600); err != nil {
				t.Fatal(err)
			}
			if err = verifyFile(path, asset); err == nil {
				t.Fatal("staged file tampering was accepted")
			}
		}
	}
}

func TestBundleExtractionRejectsTraversal(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bundle.zip")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	w := zip.NewWriter(f)
	entry, err := w.Create("ytaudio.app/../unsafe.txt")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = entry.Write([]byte("unsafe")); err != nil {
		t.Fatal(err)
	}
	if err = w.Close(); err != nil {
		t.Fatal(err)
	}
	if err = f.Close(); err != nil {
		t.Fatal(err)
	}
	if err = extractBundle(path, filepath.Join(dir, "app")); err == nil {
		t.Fatal("archive traversal was accepted")
	}
}
