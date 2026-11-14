package server

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/VaDKustiK/ytaudio/internal/deps"
	"github.com/VaDKustiK/ytaudio/internal/urlcheck"
	"github.com/VaDKustiK/ytaudio/internal/ytdlp"
)

// A portable fake yt-dlp process lets the real queue/runner/error/file handoff
// execute on Windows and macOS too, without shell scripts or network access.
func TestMain(m *testing.M) {
	if mode := os.Getenv("YTAUDIO_DOWNLOAD_TEST_HELPER"); mode != "" {
		if mode == "saved" || mode == "partial" {
			fmt.Fprintln(os.Stderr, "WARNING: Original download format is only available for registered users")
			data, _ := json.Marshal(os.Getenv("YTAUDIO_DOWNLOAD_TEST_FILE"))
			fmt.Printf("filepath:%s\n", data)
		}
		if mode == "restricted" || mode == "partial" {
			fmt.Fprintln(os.Stderr, "ERROR: Requested format is not available")
			os.Exit(1)
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}
func TestSourceValidationAndPlaylistConfirmation(t *testing.T) {
	s := testServer(t)
	for _, tt := range []struct {
		body    string
		status  int
		message string
	}{
		{`{"source":"soundcloud","url":"https://soundcloud.com/artist/sets/album","format":"mp3"}`, 409, "playlist"},
		{`{"source":"soundcloud","url":"https://soundcloud.com/artist/sets/album","format":"mp3","confirm_playlist":true}`, 409, "tools"},
		{`{"source":"soundcloud","url":"https://youtu.be/abc","format":"mp3"}`, 400, "SoundCloud"},
		{`{"source":"youtube","url":"https://soundcloud.com/artist/track","format":"mp3"}`, 400, "YouTube"},
		{`{"source":"unknown","url":"https://soundcloud.com/artist/track","format":"mp3"}`, 400, "source"},
		{`{"url":"https://youtube.com/playlist?list=PLabc","format":"mp3"}`, 409, "playlist"},
	} {
		w := request(s, "POST", "/api/download", tt.body)
		if w.Code != tt.status || !strings.Contains(w.Body.String(), tt.message) {
			t.Errorf("%s: %d %s", tt.body, w.Code, w.Body.String())
		}
	}
	w := request(s, "POST", "/api/resolve-link", `{"url":"https://soundcloud.com/artist/sets/album?si=tracking","source":"soundcloud"}`)
	var link urlcheck.Link
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &link) != nil || !link.Playlist || link.URL != "https://soundcloud.com/artist/sets/album" {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	if request(s, "GET", "/api/resolve-link", "").Code != 405 {
		t.Fatal("resolve accepts GET")
	}
}
func TestSoundCloudRunReportsGuestRestrictionsAndSavedFiles(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"saved", "restricted", "partial", "empty"} {
		t.Run(mode, func(t *testing.T) {
			s := testServer(t)
			file := filepath.Join(s.cfg.OutputFolder, "track.flac")
			if err := os.WriteFile(file, []byte("audio fixture"), 0600); err != nil {
				t.Fatal(err)
			}
			t.Setenv("YTAUDIO_DOWNLOAD_TEST_HELPER", mode)
			t.Setenv("YTAUDIO_DOWNLOAD_TEST_FILE", file)
			job := &Job{Source: urlcheck.SoundCloud, Format: ytdlp.FLAC}
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			err := s.run(ctx, job, s.deps, executable, nil)
			if mode == "saved" {
				if err != nil || job.State != "complete" || len(job.Files) != 1 || job.Files[0].Name != "track.flac" {
					t.Fatalf("%+v %v", job, err)
				}
			} else {
				if err == nil || job.State != "failed" {
					t.Fatalf("false success: %+v %v", job, err)
				}
				if mode == "empty" {
					if !strings.Contains(job.Message, "unavailable") {
						t.Fatal(job.Message)
					}
				} else if !strings.Contains(job.Message, "without signing in") {
					t.Fatal(job.Message)
				}
			}
			if mode == "partial" && (len(job.Files) != 1 || !strings.Contains(job.Message, "Some tracks were saved")) {
				t.Fatalf("partial tracks lost: %+v", job)
			}
		})
	}
}
func TestSoundCloudQueuePreservesSource(t *testing.T) {
	s := testServer(t)
	// Only the path lookup is needed: a deliberately missing executable makes the
	// queue fail locally, while still exercising request creation and snapshots.
	dir := t.TempDir()
	name := "yt-dlp"
	if os.PathSeparator == '\\' {
		name += ".exe"
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte("invalid executable fixture"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	s.statuses = []deps.Status{{Ready: true}, {Ready: true}, {Ready: true}, {Ready: true}}
	w := request(s, "POST", "/api/download", `{"url":"https://soundcloud.com/artist/song","source":"soundcloud","format":"ogg"}`)
	if w.Code != 200 {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	s.mu.RLock()
	jobs := s.jobSnapshotLocked()
	s.mu.RUnlock()
	if len(jobs) != 1 || jobs[0].Source != urlcheck.SoundCloud || jobs[0].Format != ytdlp.OGG {
		t.Fatalf("%+v", jobs)
	}
}
