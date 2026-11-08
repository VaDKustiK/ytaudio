package urlcheck

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

var allowedHosts = map[string]bool{"youtube.com": true, "www.youtube.com": true, "m.youtube.com": true, "music.youtube.com": true, "youtu.be": true}
var identifier = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)

func Normalize(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	parsed, err := url.Parse(raw)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.User != nil {
		return "", fmt.Errorf("enter a valid YouTube video or playlist link")
	}
	host := strings.ToLower(strings.TrimSuffix(parsed.Hostname(), "."))
	if !allowedHosts[host] {
		return "", fmt.Errorf("only YouTube video and playlist links are supported")
	}
	if port := parsed.Port(); port != "" && port != "443" && port != "80" {
		return "", fmt.Errorf("YouTube links cannot use a custom port")
	}
	query := parsed.Query()
	clean := url.Values{}
	if list := query.Get("list"); list != "" {
		if !identifier.MatchString(list) {
			return "", fmt.Errorf("invalid playlist identifier")
		}
		clean.Set("list", list)
	}
	path := strings.TrimSuffix(parsed.Path, "/")
	if host == "youtu.be" {
		id := strings.TrimPrefix(path, "/")
		if !identifier.MatchString(id) {
			return "", fmt.Errorf("invalid YouTube video link")
		}
	} else {
		switch path {
		case "/watch":
			id := query.Get("v")
			if !identifier.MatchString(id) {
				return "", fmt.Errorf("YouTube link is missing a video identifier")
			}
			clean.Set("v", id)
		case "/playlist":
			if clean.Get("list") == "" {
				return "", fmt.Errorf("YouTube link is missing a playlist identifier")
			}
		default:
			parts := strings.Split(strings.TrimPrefix(path, "/"), "/")
			if len(parts) != 2 || (parts[0] != "shorts" && parts[0] != "live" && parts[0] != "embed" && parts[0] != "v") || !identifier.MatchString(parts[1]) {
				return "", fmt.Errorf("paste a YouTube video or playlist link, rather than a channel or page")
			}
		}
	}
	parsed.Scheme = "https"
	parsed.Host = host
	parsed.Path = path
	parsed.RawPath = ""
	parsed.RawQuery = clean.Encode()
	parsed.Fragment = ""
	parsed.ForceQuery = false
	return parsed.String(), nil
}
