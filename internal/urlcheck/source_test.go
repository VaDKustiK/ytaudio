package urlcheck

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestSoundCloudLinks(t *testing.T) {
	for _, tt := range []struct {
		raw, want       string
		playlist, short bool
	}{
		{" http://www.soundcloud.com/artist/track/?si=tracking#t=10:00 ", "https://soundcloud.com/artist/track", false, false},
		{"https://m.soundcloud.com/artist/sets/album?utm_source=share", "https://soundcloud.com/artist/sets/album", true, false},
		{"https://on.soundcloud.com/Abc123?si=tracking", "https://on.soundcloud.com/Abc123", false, true},
		{"https://snd.sc/Abc123", "https://snd.sc/Abc123", false, true},
		{"https://soundcloud.com/artist/song/s-Token", "https://soundcloud.com/artist/song/s-Token", false, false},
		{"https://soundcloud.com/artist/sets/album/s-Token", "https://soundcloud.com/artist/sets/album/s-Token", true, false},
		{"https://soundcloud.com/artist/song?secret_token=s-Token&si=tracking", "https://soundcloud.com/artist/song/s-Token", false, false},
	} {
		got, err := Parse(tt.raw, SoundCloud)
		if err != nil || got.URL != tt.want || got.Playlist != tt.playlist || got.Short != tt.short || got.Source != SoundCloud {
			t.Errorf("%s: %+v %v", tt.raw, got, err)
		}
	}
	for _, raw := range []string{"https://soundcloud.com/artist", "https://soundcloud.com/artist/likes", "https://soundcloud.com/artist/sets", "https://soundcloud.com/search/tracks", "https://soundcloud.com/discover/sets", "https://soundcloud.com/artist/track/extra", "https://soundcloud.com/artist/../track", "https://soundcloud.com/artist%2ftrack", "https://soundcloud.com:444/artist/track", "https://user:pass@soundcloud.com/artist/track", "https://soundcloud.com.evil/artist/track", "https://evil.soundcloud.com/artist/track", "https://on.soundcloud.com/artist/track", "file:///etc/passwd", "--exec=bad", "https://youtube.com/watch?v=abc"} {
		if _, err := Parse(raw, SoundCloud); err == nil {
			t.Error("accepted", raw)
		}
	}
	if _, err := Parse("https://soundcloud.com/artist/track", ""); err == nil {
		t.Error("default source must remain YouTube")
	}
	if _, err := Parse("https://youtu.be/abc", "arbitrary"); err == nil {
		t.Error("unknown source accepted")
	}
}

type linkTransport func(*http.Request) (*http.Response, error)

func (f linkTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestSoundCloudShareResolution(t *testing.T) {
	for _, target := range []string{"https://soundcloud.com/artist/sets/album?si=tracking", "https://evil.test/artist/song", "http://127.0.0.1/private", "https://soundcloud.com/artist", "https://soundcloud.com:999/artist/song", "https://user:password@soundcloud.com/artist/song"} {
		t.Run(target, func(t *testing.T) {
			calls := 0
			client := &http.Client{Transport: linkTransport(func(r *http.Request) (*http.Response, error) {
				calls++
				if calls == 1 {
					return &http.Response{StatusCode: 302, Header: http.Header{"Location": {target}}, Body: io.NopCloser(strings.NewReader("")), Request: r}, nil
				}
				if r.URL.String() != "https://soundcloud.com/artist/sets/album" {
					t.Fatal("unsafe redirect requested", r.URL)
				}
				if r.Header.Get("Cookie") != "" || r.Header.Get("Authorization") != "" {
					t.Fatal("guest request contains credentials")
				}
				return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("")), Request: r}, nil
			})}
			got, err := Resolve(context.Background(), client, "https://on.soundcloud.com/Share", SoundCloud)
			if strings.Contains(target, "/sets/album") {
				if err != nil || !got.Playlist || got.Short || calls != 2 {
					t.Fatalf("%+v %v calls=%d", got, err, calls)
				}
			} else if err == nil || calls != 1 {
				t.Fatalf("unsafe redirect accepted: %+v %v calls=%d", got, err, calls)
			}
		})
	}
}
func TestShareRedirectLoopIsBounded(t *testing.T) {
	calls := 0
	client := &http.Client{Transport: linkTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		return &http.Response{StatusCode: 302, Header: http.Header{"Location": {"https://on.soundcloud.com/Loop"}}, Body: io.NopCloser(strings.NewReader("")), Request: r}, nil
	})}
	if _, err := Resolve(context.Background(), client, "https://snd.sc/Loop", SoundCloud); err == nil || calls != 5 {
		t.Fatalf("err=%v calls=%d", err, calls)
	}
}
func TestDirectSoundCloudLinkDoesNotUseNetwork(t *testing.T) {
	client := &http.Client{Transport: linkTransport(func(r *http.Request) (*http.Response, error) { t.Fatal("unexpected network request"); return nil, nil })}
	if _, err := Resolve(context.Background(), client, "https://soundcloud.com/artist/song", SoundCloud); err != nil {
		t.Fatal(err)
	}
}
