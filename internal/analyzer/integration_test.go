package analyzer

import (
	"context"
	"encoding/binary"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func writeWAV(t *testing.T, path string, seconds int) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	samples := 44100 * seconds
	f.WriteString("RIFF")
	_ = binary.Write(f, binary.LittleEndian, uint32(36+samples*2))
	f.WriteString("WAVEfmt ")
	_ = binary.Write(f, binary.LittleEndian, uint32(16))
	for _, n := range []uint16{1, 1} {
		_ = binary.Write(f, binary.LittleEndian, n)
	}
	_ = binary.Write(f, binary.LittleEndian, uint32(44100))
	_ = binary.Write(f, binary.LittleEndian, uint32(88200))
	_ = binary.Write(f, binary.LittleEndian, uint16(2))
	_ = binary.Write(f, binary.LittleEndian, uint16(16))
	f.WriteString("data")
	_ = binary.Write(f, binary.LittleEndian, uint32(samples*2))
	for i := 0; i < samples; i++ {
		value := int16(6000 * math.Sin(2*math.Pi*220*float64(i)/44100))
		_ = binary.Write(f, binary.LittleEndian, value)
	}
}
func waitComplete(t *testing.T, m *Manager) State {
	t.Helper()
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		s := m.Snapshot()
		if s.Status != "queued" && s.Status != "running" {
			return s
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("analyzer did not finish")
	return State{}
}
func TestNativeIntegration(t *testing.T) {
	if os.Getenv("YTAUDIO_TEST_ANALYZER_DIR") == "" {
		t.Skip("set YTAUDIO_TEST_ANALYZER_DIR for the packaged native engine")
	}
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("FFmpeg required")
	}
	t.Setenv("YTAUDIO_ANALYZER_DIR", os.Getenv("YTAUDIO_TEST_ANALYZER_DIR"))
	t.Setenv("YTAUDIO_CACHE_DIR", t.TempDir())
	m, err := New(context.Background(), "dev", "")
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	path := filepath.Join(t.TempDir(), "Music & звук.wav")
	writeWAV(t, path, 4)
	file, err := m.Register(path, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = m.Start(file.ID, ffmpeg, "balanced"); err != nil {
		t.Fatal(err)
	}
	s := waitComplete(t, m)
	if s.Status != "complete" || s.Result == nil {
		t.Fatalf("%+v", s)
	}
	if s.Cached {
		t.Fatal("first analysis should not be cached")
	}
	if _, err = m.Start(file.ID, ffmpeg, "low"); err != nil {
		t.Fatal(err)
	}
	s = waitComplete(t, m)
	if !s.Cached {
		t.Fatal("unchanged audio did not use cache")
	}
	writeWAV(t, path, 5)
	if _, err = m.Start(file.ID, ffmpeg, "maximum"); err != nil {
		t.Fatal(err)
	}
	s = waitComplete(t, m)
	if s.Status != "complete" || s.Cached {
		t.Fatalf("changed audio reused cache: %+v", s)
	}
	// Exercise actual source formats and non-default sample rates.
	for _, extension := range []string{".mp3", ".flac", ".m4a", ".ogg", ".opus"} {
		encoded := filepath.Join(t.TempDir(), "Audio & звук"+extension)
		command := exec.Command(ffmpeg, "-nostdin", "-v", "error", "-i", path, "-ar", "48000", encoded)
		if output, e := command.CombinedOutput(); e != nil {
			t.Fatalf("encode %s: %v %s", extension, e, output)
		}
		selected, e := m.Register(encoded, false)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = m.Start(selected.ID, ffmpeg, "balanced"); e != nil {
			t.Fatal(e)
		}
		result := waitComplete(t, m)
		if result.Status != "complete" {
			t.Fatalf("%s: %+v", extension, result)
		}
	}
	// A new request must cancel/replace the old job without stale completion.
	otherPath := filepath.Join(t.TempDir(), "replacement.wav")
	writeWAV(t, otherPath, 3)
	other, e := m.Register(otherPath, false)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = m.Start(file.ID, ffmpeg, "balanced"); e != nil {
		t.Fatal(e)
	}
	replacement, e := m.Start(other.ID, ffmpeg, "balanced")
	if e != nil {
		t.Fatal(e)
	}
	result := waitComplete(t, m)
	if result.ID != replacement.ID || result.FileID != other.ID || result.Status != "complete" {
		t.Fatalf("replacement: %+v", result)
	}
	writeWAV(t, path, 6)
	if _, err = m.Start(file.ID, ffmpeg, "balanced"); err != nil {
		t.Fatal(err)
	}
	m.Cancel()
	s = waitComplete(t, m)
	if s.Status != "cancelled" || !s.Ready {
		t.Fatalf("cancellation: %+v", s)
	}
	// Cancel the actual native inference process, not just preparatory hashing.
	writeWAV(t, path, 8)
	if _, err = m.Start(file.ID, ffmpeg, "balanced"); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(15 * time.Second)
	seen := false
	for time.Now().Before(deadline) {
		state := m.Snapshot()
		if state.Stage == "Beat detection" {
			seen = true
			break
		}
		if state.Status == "failed" {
			t.Fatalf("before native cancellation: %+v", state)
		}
		time.Sleep(5 * time.Millisecond)
	}
	if !seen {
		t.Fatal("native inference stage was not observed")
	}
	m.Cancel()
	s = waitComplete(t, m)
	if s.Status != "cancelled" || !s.Ready {
		t.Fatalf("native cancellation: %+v", s)
	}
	leftover, _ := filepath.Glob(filepath.Join(m.root, ".job-*"))
	if len(leftover) != 0 {
		t.Fatalf("temporary PCM leaked: %v", leftover)
	}

	_ = os.Remove(path)
	if _, err = m.Start(file.ID, ffmpeg, "balanced"); err != nil {
		t.Fatal(err)
	}
	if s = waitComplete(t, m); s.Status != "failed" {
		t.Fatalf("missing file: %+v", s)
	}
}

func TestCloseStopsNativeWorker(t *testing.T) {
	if os.Getenv("YTAUDIO_TEST_ANALYZER_DIR") == "" {
		t.Skip("set YTAUDIO_TEST_ANALYZER_DIR for the packaged native engine")
	}
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("FFmpeg required")
	}
	t.Setenv("YTAUDIO_ANALYZER_DIR", os.Getenv("YTAUDIO_TEST_ANALYZER_DIR"))
	t.Setenv("YTAUDIO_CACHE_DIR", t.TempDir())
	m, err := New(context.Background(), "dev", "")
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	path := filepath.Join(t.TempDir(), "keep this audio.wav")
	writeWAV(t, path, 20)
	file, err := m.Register(path, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = m.Start(file.ID, ffmpeg, "balanced"); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(30 * time.Second)
	for m.Snapshot().Stage != "Beat detection" {
		if time.Now().After(deadline) || m.Snapshot().Status == "failed" {
			t.Fatalf("native worker did not start: %+v", m.Snapshot())
		}
		time.Sleep(5 * time.Millisecond)
	}
	closed := make(chan struct{})
	go func() { m.Close(); close(closed) }()
	select {
	case <-closed:
	case <-time.After(15 * time.Second):
		t.Fatal("closing the manager did not terminate the native worker")
	}
	if state := m.Snapshot(); state.Status != "cancelled" {
		t.Fatalf("close left an active job: %+v", state)
	}
	if _, err = os.Stat(path); err != nil {
		t.Fatal("closing the analyzer removed the original audio")
	}
	if leftover, _ := filepath.Glob(filepath.Join(m.root, ".job-*")); len(leftover) != 0 {
		t.Fatalf("close leaked decoded audio: %v", leftover)
	}
	if _, err = m.Start(file.ID, ffmpeg, "balanced"); err == nil {
		t.Fatal("closed manager accepted another job")
	}
}
