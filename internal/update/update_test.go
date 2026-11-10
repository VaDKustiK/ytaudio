package update

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestNewer(t *testing.T) {
	tests := []struct {
		current string
		latest  string
		want    bool
	}{
		{"dev", "v1.0.0", false},
		{"v1.0.0", "v1.1.0", true},
		{"v1.2.0", "v1.1.0", false},
		{"v1.0.0", "v1.0.0", false},
	}
	for _, test := range tests {
		if got := newer(test.current, test.latest); got != test.want {
			t.Errorf("newer(%q, %q) = %v, want %v", test.current, test.latest, got, test.want)
		}
	}
}

type transportFunc func(*http.Request) (*http.Response, error)

func (f transportFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestReleaseFeedFiltersAndOrdersNotifications(t *testing.T) {
	client := &http.Client{Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host != "api.github.com" || r.Header.Get("User-Agent") == "" {
			t.Fatal("invalid release request")
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`[
 {"tag_name":"v9.0.0","draft":true,"html_url":"https://github.com/VaDKustiK/ytaudio/releases/tag/v9"},
 {"tag_name":"v8.0.0","prerelease":true,"html_url":"https://github.com/VaDKustiK/ytaudio/releases/tag/v8"},
 {"tag_name":"v0.3.0","name":"New features","html_url":"https://github.com/VaDKustiK/ytaudio/releases/tag/v0.3.0","body":"notes"},
 {"tag_name":"v0.2.0","html_url":"https://github.com/VaDKustiK/ytaudio/releases/tag/v0.2.0"},
 {"tag_name":"v0.1.0","html_url":"https://github.com/VaDKustiK/ytaudio/releases/tag/v0.1.0"},
 {"tag_name":"v99.0.0","html_url":"https://evil.test/release"}]`)), Header: make(http.Header)}, nil
	})}
	info := checkWithClient(context.Background(), "v0.1.0", client)
	if !info.Ready || info.Latest != "v0.3.0" || len(info.Notifications) != 2 || info.Notifications[0].Title != "New features" {
		t.Fatalf("%#v", info)
	}
}
func TestReleaseFailuresAreFriendly(t *testing.T) {
	for _, status := range []int{403, 404, 500} {
		client := &http.Client{Transport: transportFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader("no")), Header: make(http.Header)}, nil
		})}
		info := checkWithClient(context.Background(), "v0.1.0", client)
		if info.Error == "" || info.Ready {
			t.Fatalf("%d: %#v", status, info)
		}
	}
}

func TestDevelopmentBuildSkipsReleaseCheck(t *testing.T) {
	// This proxy would fail validation if a network client were created.
	info := CheckWithProxy(context.Background(), "dev", "invalid-proxy")
	if info.Current != "dev" || info.Checking || info.Ready || info.Error != "" || info.Latest != "" || len(info.Notifications) != 0 {
		t.Fatalf("development builds must not check releases: %+v", info)
	}
}

func TestReleaseCheckDistinguishesRateLimitsFromOtherDenials(t *testing.T) {
	for _, tc := range []struct {
		status          int
		remaining, want string
	}{
		{403, "0", "update-check limit"},
		{429, "", "update-check limit"},
		{403, "15", "blocked the update check"},
	} {
		client := &http.Client{Transport: transportFunc(func(*http.Request) (*http.Response, error) {
			headers := make(http.Header)
			headers.Set("X-RateLimit-Remaining", tc.remaining)
			return &http.Response{StatusCode: tc.status, Header: headers, Body: io.NopCloser(strings.NewReader("no"))}, nil
		})}
		info := checkWithClient(context.Background(), "v1.0.0", client)
		if !strings.Contains(info.Error, tc.want) || info.Ready {
			t.Fatalf("%d: %+v", tc.status, info)
		}
	}
}
