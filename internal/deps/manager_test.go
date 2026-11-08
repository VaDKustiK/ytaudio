package deps

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestChecksumFor(t *testing.T) {
	data := "aabbcc  yt-dlp\n" + strings.Repeat("0", sha256.Size*2) + " *target\n"
	got, err := checksumFor(strings.NewReader(data), "target")
	if err != nil {
		t.Fatal(err)
	}
	if got != strings.Repeat("0", sha256.Size*2) {
		t.Fatalf("checksum = %q", got)
	}
}

func TestReplaceWithFallback(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "yt-dlp")
	if err := os.WriteFile(target, []byte("old"), 0o700); err != nil {
		t.Fatal(err)
	}
	temp, err := os.CreateTemp(dir, "download")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.WriteString(temp, "new"); err != nil {
		t.Fatal(err)
	}
	temp.Close()
	if err := replaceWithFallback(temp.Name(), target); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(target)
	if string(got) != "new" {
		t.Fatalf("target = %q", got)
	}
	old, _ := os.ReadFile(target + ".previous")
	if string(old) != "old" {
		t.Fatalf("backup = %q", old)
	}
}

func TestDownloadVerifiedUsesChecksumAndAtomicReplace(t *testing.T) {
	payload := []byte("verified binary")
	sum := sha256.Sum256(payload)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/checksums":
			_, _ = io.WriteString(w, hex.EncodeToString(sum[:])+"  artifact\n")
		case "/artifact":
			_, _ = w.Write(payload)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	dir := t.TempDir()
	manager := &Manager{dir: dir, client: server.Client()}
	target := filepath.Join(dir, "binary")
	source := Source{Name: "test", URL: server.URL + "/artifact", ChecksumsURL: server.URL + "/checksums", Filename: "artifact"}
	if err := manager.downloadVerified(context.Background(), source, target, nil); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(payload) {
		t.Fatalf("downloaded %q, want %q", got, payload)
	}
}

func TestYTDLPFilenameUsesUniversalMacOSAsset(t *testing.T) {
	for _, arch := range []string{"amd64", "arm64"} {
		got, err := ytDLPFilename("darwin", arch)
		if err != nil {
			t.Fatal(err)
		}
		if got != "yt-dlp_macos" {
			t.Fatalf("darwin/%s filename = %q", arch, got)
		}
	}
}

func TestChecksumMismatchPreservesWorkingBinary(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "tampered") }))
	defer server.Close()
	dir := t.TempDir()
	target := filepath.Join(dir, "binary")
	os.WriteFile(target, []byte("working"), 0700)
	m := &Manager{dir: dir, client: server.Client()}
	err := m.downloadVerified(context.Background(), Source{Name: "test", URL: server.URL, SHA256: strings.Repeat("0", 64)}, target, nil)
	if err == nil {
		t.Fatal("accepted invalid checksum")
	}
	data, _ := os.ReadFile(target)
	if string(data) != "working" {
		t.Fatal("destroyed working binary")
	}
}
func TestRepeatedReplaceAndRollback(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "tool")
	os.WriteFile(target, []byte("old"), 0700)
	for _, value := range []string{"one", "two"} {
		p := filepath.Join(dir, "temp")
		os.WriteFile(p, []byte(value), 0700)
		if err := replaceWithFallback(p, target); err != nil {
			t.Fatal(err)
		}
	}
	if err := replaceWithFallback(filepath.Join(dir, "missing"), target); err == nil {
		t.Fatal("missing update succeeded")
	}
	data, _ := os.ReadFile(target)
	if string(data) != "two" {
		t.Fatalf("rollback lost tool: %s", data)
	}
}
