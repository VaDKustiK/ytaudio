package urlcheck

import (
	"fmt"
	"net/url"
	"strings"
)

var allowedHosts = map[string]bool{
	"youtube.com":       true,
	"www.youtube.com":   true,
	"m.youtube.com":     true,
	"music.youtube.com": true,
	"youtu.be":          true,
}

func Normalize(raw string) (string, error) {
	if strings.HasPrefix(raw, "-") {
		return "", fmt.Errorf("URL must not start with a dash")
	}
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.User != nil {
		return "", fmt.Errorf("enter a valid YouTube URL")
	}
	host := strings.ToLower(strings.TrimSuffix(parsed.Hostname(), "."))
	if !allowedHosts[host] {
		return "", fmt.Errorf("only YouTube URLs are supported")
	}
	parsed.Scheme = "https"
	parsed.Host = host
	query := parsed.Query()
	for key := range query {
		if strings.HasPrefix(strings.ToLower(key), "utm_") || key == "si" || key == "feature" {
			delete(query, key)
		}
	}
	parsed.RawQuery = query.Encode()
	parsed.Fragment = ""
	if parsed.Path == "" {
		return "", fmt.Errorf("YouTube URL is missing a video or playlist")
	}
	return parsed.String(), nil
}
