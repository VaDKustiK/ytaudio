package update

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"runtime"
	"strconv"
	"strings"
	"time"
)

const repository = "VaDKustiK/ytaudio"

type Info struct {
	Current string `json:"current"`
	Latest  string `json:"latest,omitempty"`
	URL     string `json:"url,omitempty"`
	Asset   string `json:"asset,omitempty"`
	Ready   bool   `json:"ready"`
	Error   string `json:"error,omitempty"`
}

type release struct {
	TagName string `json:"tag_name"`
	HTMLURL string `json:"html_url"`
}

func Check(ctx context.Context, current string) Info {
	result := Info{Current: current}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.github.com/repos/"+repository+"/releases/latest", nil)
	if err != nil {
		result.Error = err.Error()
		return result
	}
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("User-Agent", "ytaudio/"+strings.TrimPrefix(current, "v"))
	client := &http.Client{Timeout: 15 * time.Second}
	response, err := client.Do(request)
	if err != nil {
		result.Error = "Unable to check for updates."
		return result
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		if response.StatusCode == http.StatusForbidden {
			result.Error = "GitHub temporarily refused the update check. Try again later."
			return result
		}
		result.Error = fmt.Sprintf("Update check returned HTTP %d.", response.StatusCode)
		return result
	}
	var latest release
	if err := json.NewDecoder(response.Body).Decode(&latest); err != nil {
		result.Error = "Unable to read update information."
		return result
	}
	result.Latest, result.URL = latest.TagName, latest.HTMLURL
	result.Asset = fmt.Sprintf("ytaudio-%s-%s-%s.tar.gz", strings.TrimPrefix(latest.TagName, "v"), runtime.GOOS, runtime.GOARCH)
	result.Ready = newer(current, latest.TagName)
	return result
}

func newer(current, latest string) bool {
	currentParts, ok := versionParts(current)
	if !ok {
		return false
	}
	latestParts, ok := versionParts(latest)
	if !ok {
		return false
	}
	for i := range currentParts {
		if latestParts[i] != currentParts[i] {
			return latestParts[i] > currentParts[i]
		}
	}
	return false
}

func versionParts(value string) ([3]int, bool) {
	var parts [3]int
	value = strings.TrimPrefix(value, "v")
	if value == "" || value == "dev" {
		return parts, false
	}
	fields := strings.SplitN(value, "-", 2)[0]
	segments := strings.Split(fields, ".")
	if len(segments) != 3 {
		return parts, false
	}
	for i, segment := range segments {
		number, err := strconv.Atoi(segment)
		if err != nil || number < 0 {
			return parts, false
		}
		parts[i] = number
	}
	return parts, true
}
