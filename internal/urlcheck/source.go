package urlcheck

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
)

type Source string

const (
	YouTube    Source = "youtube"
	SoundCloud Source = "soundcloud"
)

type Link struct {
	URL      string `json:"url"`
	Source   Source `json:"source"`
	Playlist bool   `json:"playlist"`
	Short    bool   `json:"-"`
}

var soundcloudPart = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)
var soundcloudReserved = map[string]bool{"discover": true, "stream": true, "search": true, "you": true, "charts": true, "upload": true, "settings": true, "pages": true, "terms-of-use": true}
var soundcloudPages = map[string]bool{"tracks": true, "popular-tracks": true, "albums": true, "sets": true, "reposts": true, "likes": true, "followers": true, "following": true, "spotlight": true, "comments": true}

// Parse accepts only direct provider links. An omitted source keeps older
// clients restricted to YouTube, rather than enabling arbitrary extractors.
func Parse(raw string, source Source) (Link, error) {
	if source == "" {
		source = YouTube
	}
	if source == YouTube {
		normalized, err := Normalize(raw)
		if err != nil {
			return Link{}, err
		}
		u, _ := url.Parse(normalized)
		return Link{URL: normalized, Source: source, Playlist: u.Query().Get("list") != ""}, nil
	}
	if source != SoundCloud {
		return Link{}, fmt.Errorf("unsupported download source")
	}
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.User != nil {
		return Link{}, fmt.Errorf("enter a valid SoundCloud track or playlist link")
	}
	host := strings.ToLower(strings.TrimSuffix(u.Hostname(), "."))
	short := host == "on.soundcloud.com" || host == "snd.sc"
	if !short && host != "soundcloud.com" && host != "www.soundcloud.com" && host != "m.soundcloud.com" {
		return Link{}, fmt.Errorf("only SoundCloud track and playlist links are supported")
	}
	if port := u.Port(); port != "" && port != "443" && port != "80" {
		return Link{}, fmt.Errorf("SoundCloud links cannot use a custom port")
	}
	// Reject encoded separators and dot segments before any network request.
	if u.RawPath != "" {
		return Link{}, fmt.Errorf("paste a direct SoundCloud track or playlist link")
	}
	parts := strings.Split(strings.TrimSuffix(strings.TrimPrefix(u.Path, "/"), "/"), "/")
	for _, part := range parts {
		if !soundcloudPart.MatchString(part) || len(part) > 256 {
			return Link{}, fmt.Errorf("invalid SoundCloud link")
		}
	}
	playlist := !short && len(parts) >= 3 && parts[1] == "sets"
	valid := short && len(parts) == 1
	if !short && !soundcloudReserved[parts[0]] {
		n := 2
		if playlist {
			n = 3
		}
		valid = (len(parts) == n || (len(parts) == n+1 && strings.HasPrefix(parts[n], "s-") && len(parts[n]) > 2)) && (playlist || !soundcloudPages[parts[1]])
	}
	if !valid {
		return Link{}, fmt.Errorf("paste a SoundCloud track or playlist link, rather than a profile or page")
	}
	if secret := u.Query().Get("secret_token"); secret != "" {
		if short || len(secret) <= 2 || !strings.HasPrefix(secret, "s-") || !soundcloudPart.MatchString(secret) {
			return Link{}, fmt.Errorf("invalid SoundCloud private link")
		}
		// The web-page extractor expects private tokens in the path; its query
		// form is only supported on API URLs (which the UI does not accept).
		n := 2
		if playlist {
			n = 3
		}
		if len(parts) == n {
			parts = append(parts, secret)
		} else if parts[n] != secret {
			return Link{}, fmt.Errorf("conflicting SoundCloud private-link tokens")
		}
	}
	if !short {
		host = "soundcloud.com"
	}
	u.Scheme = "https"
	u.Host = host
	u.Path = "/" + strings.Join(parts, "/")
	u.RawQuery = ""
	u.Fragment = ""
	u.ForceQuery = false
	return Link{URL: u.String(), Source: source, Playlist: playlist, Short: short}, nil
}

// Resolve expands mobile share links without following redirects to other
// websites or forwarding credentials. Direct track links never make a request.
func Resolve(ctx context.Context, client *http.Client, raw string, source Source) (Link, error) {
	link, err := Parse(raw, source)
	if err != nil || !link.Short {
		return link, err
	}
	copyClient := *client
	copyClient.Jar = nil
	copyClient.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 {
			return fmt.Errorf("too many SoundCloud share-link redirects")
		}
		next, err := Parse(req.URL.String(), SoundCloud)
		if err != nil {
			return fmt.Errorf("SoundCloud share link did not lead to a track or playlist")
		}
		u, _ := url.Parse(next.URL)
		req.URL = u
		return nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, link.URL, nil)
	if err != nil {
		return Link{}, err
	}
	req.Header.Set("User-Agent", "ytaudio")
	response, err := copyClient.Do(req)
	if err != nil {
		return Link{}, fmt.Errorf("could not open this SoundCloud share link. Check your connection or paste the full track/playlist link")
	}
	defer response.Body.Close()
	resolved, err := Parse(response.Request.URL.String(), SoundCloud)
	if err != nil || resolved.Short {
		return Link{}, fmt.Errorf("this SoundCloud share link could not be expanded. Open it in your browser and paste the full track/playlist link")
	}
	return resolved, nil
}
