package update

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/VaDKustiK/ytaudio/internal/deps"
)

const repository = "VaDKustiK/ytaudio"

type Notification struct {
	Version string `json:"version"`
	Title   string `json:"title"`
	URL     string `json:"url"`
	Date    string `json:"date"`
	Notes   string `json:"notes"`
}
type Info struct {
	Current       string         `json:"current"`
	Latest        string         `json:"latest,omitempty"`
	URL           string         `json:"url,omitempty"`
	Ready         bool           `json:"ready"`
	Checking      bool           `json:"checking"`
	Error         string         `json:"error,omitempty"`
	Notifications []Notification `json:"notifications"`
}
type release struct {
	TagName    string `json:"tag_name"`
	Name       string `json:"name"`
	HTMLURL    string `json:"html_url"`
	Body       string `json:"body"`
	Date       string `json:"published_at"`
	Draft      bool   `json:"draft"`
	Prerelease bool   `json:"prerelease"`
}

func Check(ctx context.Context, current string) Info { return CheckWithProxy(ctx, current, "") }
func CheckWithProxy(ctx context.Context, current, proxy string) Info {
	result := Info{Current: current, Notifications: []Notification{}}
	client, err := deps.HTTPClient(proxy)
	if err != nil {
		result.Error = "Check your proxy settings."
		return result
	}
	return checkWithClient(ctx, current, client)
}
func checkWithClient(ctx context.Context, current string, client *http.Client) Info {
	result := Info{Current: current, Notifications: []Notification{}}
	client.Timeout = 20 * time.Second
	req, _ := http.NewRequestWithContext(ctx, "GET", "https://api.github.com/repos/"+repository+"/releases?per_page=10", nil)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "ytaudio/"+current)
	response, err := client.Do(req)
	if err != nil {
		result.Error = "Could not check releases. Downloads still work; try again when online."
		return result
	}
	defer response.Body.Close()
	if response.StatusCode == 404 {
		result.Error = "No published releases yet."
		return result
	}
	if response.StatusCode != 200 {
		result.Error = fmt.Sprintf("GitHub update check returned HTTP %d. Try again later.", response.StatusCode)
		return result
	}
	var releases []release
	if json.NewDecoder(io.LimitReader(response.Body, 2<<20)).Decode(&releases) != nil {
		result.Error = "Could not read release information."
		return result
	}
	for _, r := range releases {
		if r.Draft || r.Prerelease || !strings.HasPrefix(r.HTMLURL, "https://github.com/"+repository+"/releases/") {
			continue
		}
		if result.Latest == "" {
			result.Latest = r.TagName
			result.URL = r.HTMLURL
			result.Ready = newer(current, r.TagName)
		}
		if newer(current, r.TagName) {
			title := r.Name
			if title == "" {
				title = r.TagName
			}
			notes := r.Body
			if len(notes) > 1200 {
				notes = notes[:1200] + "…"
			}
			result.Notifications = append(result.Notifications, Notification{r.TagName, title, r.HTMLURL, r.Date, notes})
		}
	}
	return result
}
func newer(current, latest string) bool {
	a, ok := versionParts(current)
	if !ok {
		return false
	}
	b, ok := versionParts(latest)
	if !ok {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return b[i] > a[i]
		}
	}
	return strings.Contains(current, "-") && !strings.Contains(latest, "-")
}
func versionParts(value string) ([3]int, bool) {
	var out [3]int
	parts := strings.Split(strings.SplitN(strings.TrimPrefix(value, "v"), "-", 2)[0], ".")
	if len(parts) != 3 {
		return out, false
	}
	for i, s := range parts {
		v, e := strconv.Atoi(s)
		if e != nil || v < 0 {
			return out, false
		}
		out[i] = v
	}
	return out, true
}
