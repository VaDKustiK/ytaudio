package ytdlp

import (
	"context"
	"encoding/json"
	"github.com/VaDKustiK/ytaudio/internal/urlcheck"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestSoundCloudGuestArguments(t *testing.T) {
	for _, format := range []Format{MP3, WAV, FLAC, OGG} {
		args, err := BuildArgsForSource("https://soundcloud.com/artist/song?si=tracking", t.TempDir(), format, false, urlcheck.SoundCloud)
		if err != nil {
			t.Fatal(err)
		}
		joined := strings.Join(args, " ")
		for _, want := range []string{"--ignore-config", "--no-plugin-dirs", "--no-cookies --no-cookies-from-browser", "--use-extractors soundcloud,soundcloud:set", "bestaudio[format_id!*=preview]/best[format_id!*=preview]", "--no-playlist", "--no-write-thumbnail --no-embed-thumbnail"} {
			if !strings.Contains(joined, want) {
				t.Errorf("missing %s: %v", want, args)
			}
		}
		if args[len(args)-2] != "--" || args[len(args)-1] != "https://soundcloud.com/artist/song" {
			t.Fatal(args)
		}
	}
	args, err := BuildArgsForSource("https://soundcloud.com/artist/sets/album", t.TempDir(), MP3, true, urlcheck.SoundCloud)
	if err != nil || !strings.Contains(strings.Join(args, " "), "--playlist-end 100") {
		t.Fatalf("%v %v", args, err)
	}
	if _, err := BuildArgsForSource("https://on.soundcloud.com/Short", t.TempDir(), MP3, false, urlcheck.SoundCloud); err == nil {
		t.Fatal("unresolved share link accepted")
	}
}
func TestSoundCloudErrors(t *testing.T) {
	for _, tt := range []struct {
		raw   string
		kind  ErrorKind
		part  string
		retry bool
	}{
		{"ERROR: HTTP Error 401: Unauthorized", Unavailable, "without signing in", false},
		{"ERROR: Requested format is not available", Unavailable, "does not save previews", false},
		{"ERROR: This track requires a SoundCloud Go+ subscription", Unavailable, "Go+", false},
		{"ERROR: Login required", Unavailable, "without an account", false},
		{"ERROR: This track is private", Private, "private", false},
		{"ERROR: HTTP Error 404", Unavailable, "removed", false},
		{"ERROR: This track is DRM protected", Unavailable, "protected by DRM", false},
		{"SoundCloud returned no playable tracks", Unavailable, "playlist may be empty", false},
		{"ERROR: HTTP Error 403", Forbidden403, "guest access", false},
		{"ERROR: HTTP Error 429", RateLimited, "SoundCloud", false},
		{"ERROR: not available in your country", RegionBlocked, "region", false},
		{"ERROR: Unable to extract client id", Unknown, "updating yt-dlp", true},
		{"WARNING: Original download format is only available for registered users\nERROR: Postprocessing: ffmpeg failed", ConvertFailed, "conversion", false},
		{"ERROR: connection timed out", Network, "internet", false},
	} {
		got := ClassifyForSource(tt.raw, urlcheck.SoundCloud)
		if got.Kind != tt.kind || got.Retry != tt.retry || !strings.Contains(got.Message, tt.part) || strings.Contains(got.Message, "YouTube") {
			t.Errorf("%s: %+v", tt.raw, got)
		}
	}
}

// Exercise yt-dlp's actual selector parser, not just the argument string.
// Metadata and media URLs are local fixtures; no SoundCloud connection is made.
func TestSoundCloudRejectsPreviewFormats(t *testing.T) {
	if os.Getenv("YTAUDIO_MEDIA_TESTS") != "1" || runtime.GOOS != "linux" {
		t.Skip("requires local Linux yt-dlp")
	}
	yt, err := filepath.Abs("../deps/assets/linux-amd64/yt-dlp")
	if err != nil {
		t.Fatal(err)
	}
	args, err := BuildArgsForSource("https://soundcloud.com/artist/song", t.TempDir(), MP3, false, urlcheck.SoundCloud)
	if err != nil {
		t.Fatal(err)
	}
	selector := ""
	for i, arg := range args {
		if arg == "-f" {
			selector = args[i+1]
		}
	}
	for _, tt := range []struct {
		name    string
		formats []map[string]any
		want    string
	}{
		{"prefer full track", []map[string]any{{"format_id": "http_mp3_preview", "preference": 100}, {"format_id": "http_opus", "preference": 0}}, "http_opus"},
		{"preview only", []map[string]any{{"format_id": "http_mp3_preview"}}, ""},
		{"fallback also excludes preview", []map[string]any{{"format_id": "hls_aac_preview", "vcodec": "h264"}}, ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			for _, f := range tt.formats {
				f["url"] = "https://example.invalid/song.webm"
				f["ext"] = "webm"
				f["acodec"] = "opus"
				if _, ok := f["vcodec"]; !ok {
					f["vcodec"] = "none"
				}
			}
			metadata := filepath.Join(t.TempDir(), "info.json")
			data, _ := json.Marshal(map[string]any{"id": "123", "title": "Preview regression", "extractor": "soundcloud", "formats": tt.formats})
			if err := os.WriteFile(metadata, data, 0600); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			out, err := exec.CommandContext(ctx, yt, "--ignore-config", "--no-plugin-dirs", "--no-cookies", "--no-cookies-from-browser", "--no-cache-dir", "--load-info-json", metadata, "-f", selector, "--simulate", "--print", "%(format_id)s").CombinedOutput()
			if tt.want == "" {
				if err == nil || ClassifyForSource(string(out), urlcheck.SoundCloud).Kind != Unavailable {
					t.Fatalf("preview accepted: err=%v %s", err, out)
				}
			} else if err != nil || strings.TrimSpace(string(out)) != tt.want {
				t.Fatalf("wrong format: err=%v %s", err, out)
			}
		})
	}
}
