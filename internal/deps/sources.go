package deps

import (
	"fmt"
	"runtime"
)

// These URLs are the official yt-dlp release assets documented at
// https://github.com/yt-dlp/yt-dlp/releases/latest.
const (
	ytDLPBaseURL = "https://github.com/yt-dlp/yt-dlp/releases/latest/download/"
	checksumsURL = ytDLPBaseURL + "SHA2-256SUMS"
)

type Source struct {
	Name         string
	URL          string
	ChecksumsURL string
	Filename     string
	SHA256       string
	Executable   string
}

func Sources() ([]Source, error) {
	filename, err := ytDLPFilename(runtime.GOOS, runtime.GOARCH)
	if err != nil {
		return nil, err
	}
	return []Source{{
		Name:         "yt-dlp",
		URL:          ytDLPBaseURL + filename,
		ChecksumsURL: checksumsURL,
		Filename:     filename,
		Executable:   executableName("yt-dlp"),
	}}, nil
}

func ytDLPFilename(goos, goarch string) (string, error) {
	switch goos + "/" + goarch {
	case "windows/amd64":
		return "yt-dlp.exe", nil
	case "windows/arm64":
		return "yt-dlp_arm64.exe", nil
	case "linux/amd64":
		return "yt-dlp_linux", nil
	case "linux/arm64":
		return "yt-dlp_linux_aarch64", nil
	case "darwin/amd64", "darwin/arm64":
		// yt-dlp publishes one universal macOS executable.
		return "yt-dlp_macos", nil
	default:
		return "", fmt.Errorf("unsupported yt-dlp platform %s/%s", goos, goarch)
	}
}

func executableName(name string) string {
	if runtime.GOOS == "windows" {
		return name + ".exe"
	}
	return name
}
