package analyzer

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/VaDKustiK/ytaudio/internal/deps"
)

type releaseTransport func(*http.Request) (*http.Response, error)

func (f releaseTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// Exercise the actual release archives and activation code without contacting
// GitHub or publishing a development build. Only the network transport is fake.
func TestPackagedInstallation(t *testing.T) {
	if os.Getenv("YTAUDIO_TEST_ANALYZER_DIR") == "" {
		t.Skip("prepare native release archives and set YTAUDIO_TEST_ANALYZER_DIR")
	}
	t.Setenv("YTAUDIO_CACHE_DIR", t.TempDir())
	t.Setenv("YTAUDIO_ANALYZER_DIR", "")
	m, err := New(context.Background(), "v9.9.9", "")
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	engine := "ytaudio-analyzer-" + Version + "-" + runtime.GOOS + "-" + runtime.GOARCH + ".zip"
	models := "ytaudio-models-" + ModelsVersion + ".zip"
	paths := map[string]string{}
	release := deps.Release{Tag: m.release}
	for _, name := range []string{engine, models} {
		path := filepath.Join("..", "..", "dist", name)
		digest, e := hashFile(context.Background(), path)
		if e != nil {
			t.Fatal(e)
		}
		url := "https://github.com/VaDKustiK/ytaudio/releases/download/" + m.release + "/" + name
		paths[url] = path
		release.Assets = append(release.Assets, deps.ReleaseAsset{Name: name, URL: url, Digest: "sha256:" + digest})
	}
	downloads := map[string]int{}
	client := &http.Client{Transport: releaseTransport(func(request *http.Request) (*http.Response, error) {
		var body io.ReadCloser
		if request.URL.String() == "https://api.github.com/repos/VaDKustiK/ytaudio/releases/tags/"+m.release {
			data, _ := json.Marshal(release)
			body = io.NopCloser(bytes.NewReader(data))
		} else if path := paths[request.URL.String()]; path != "" {
			downloads[filepath.Base(path)]++
			var e error
			body, e = os.Open(path)
			if e != nil {
				return nil, e
			}
		} else {
			return nil, fmt.Errorf("unexpected network request: %s", request.URL)
		}
		return &http.Response{StatusCode: http.StatusOK, Body: body, Header: make(http.Header), Request: request}, nil
	})}
	if err = m.installWithClient(context.Background(), client); err != nil {
		t.Fatal(err)
	}
	if !m.available() || downloads[models] != 1 || downloads[engine] != 1 {
		t.Fatalf("installation incomplete: %v", downloads)
	}
	installed, err := os.ReadFile(filepath.Join(m.root, "installed.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err = m.installWithClient(context.Background(), client); err != nil {
		t.Fatal(err)
	}
	if downloads[models] != 1 {
		t.Fatal("unchanged verified models were downloaded again")
	}
	// A damaged executable is repaired from the verified runtime archive.
	executable, _, err := m.paths()
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(executable, []byte("damaged"), 0700); err != nil {
		t.Fatal(err)
	}
	if err = m.installWithClient(context.Background(), client); err != nil {
		t.Fatal(err)
	}
	if err = verifyComponent(context.Background(), filepath.Dir(executable), false); err != nil {
		t.Fatal(err)
	}
	// A checksum mismatch must leave the previous working activation untouched.
	release.Assets[0].Digest = "sha256:" + string(bytes.Repeat([]byte("0"), 64))
	if err = m.installWithClient(context.Background(), client); err == nil {
		t.Fatal("corrupted archive was accepted")
	}
	after, err := os.ReadFile(filepath.Join(m.root, "installed.json"))
	if err != nil || !bytes.Equal(after, installed) || !m.available() {
		t.Fatalf("failed update changed the active installation: %v", err)
	}
	if err = verifyComponent(context.Background(), filepath.Dir(executable), false); err != nil {
		t.Fatal(err)
	}
}
