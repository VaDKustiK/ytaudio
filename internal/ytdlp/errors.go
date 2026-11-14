package ytdlp

import (
	"github.com/VaDKustiK/ytaudio/internal/urlcheck"
	"strings"
)

type ErrorKind string

const (
	RateLimited   ErrorKind = "rate_limited"
	BotCheck      ErrorKind = "bot_check"
	Forbidden403  ErrorKind = "forbidden"
	Unavailable   ErrorKind = "unavailable"
	Private       ErrorKind = "private"
	AgeRestricted ErrorKind = "age_restricted"
	RegionBlocked ErrorKind = "region_blocked"
	Livestream    ErrorKind = "livestream"
	Network       ErrorKind = "network"
	ConvertFailed ErrorKind = "convert_failed"
	Unknown       ErrorKind = "unknown"
)

type ClassifiedError struct {
	Kind    ErrorKind
	Message string
	Retry   bool
	Raw     string
}

func Classify(raw string) ClassifiedError {
	lower := strings.ToLower(raw)
	result := ClassifiedError{Kind: Unknown, Message: "The download failed. Show details for the raw error.", Raw: raw}
	switch {
	case strings.Contains(lower, "429") || strings.Contains(lower, "too many requests"):
		result.Kind, result.Message = RateLimited, "YouTube is temporarily limiting requests from your connection. Try again later or switch network."
	case strings.Contains(lower, "sign in to confirm you're not a bot") || strings.Contains(lower, "not a bot"):
		result.Kind, result.Message = BotCheck, "YouTube asked for a bot check. Try again later or switch network."
	case strings.Contains(lower, "403") || strings.Contains(lower, "forbidden"):
		result.Kind, result.Message, result.Retry = Forbidden403, "The download stream was refused. Try updating the download tools in Info.", true
	case strings.Contains(lower, "private video") || strings.Contains(lower, "video is private"):
		result.Kind, result.Message = Private, "This video is private."
	case strings.Contains(lower, "age-restricted"):
		result.Kind, result.Message = AgeRestricted, "This age-restricted video cannot be downloaded without browser cookies."
	case strings.Contains(lower, "not available in your country") || strings.Contains(lower, "geo-restricted") || strings.Contains(lower, "not made this video available in your country"):
		result.Kind, result.Message = RegionBlocked, "This video is not available in your region."
	case strings.Contains(lower, "not available") || strings.Contains(lower, "unavailable"):
		result.Kind, result.Message = Unavailable, "This video is unavailable."
	case strings.Contains(lower, "not available in your country") || strings.Contains(lower, "region"):
		result.Kind, result.Message = RegionBlocked, "This video is not available in your region."
	case strings.Contains(lower, "livestream") || strings.Contains(lower, "live stream"):
		result.Kind, result.Message = Livestream, "Live streams are not supported."
	case strings.Contains(lower, "ffmpeg") || strings.Contains(lower, "postprocessing"):
		result.Kind, result.Message = ConvertFailed, "Audio conversion failed. Re-run dependency setup and try again."
	case strings.Contains(lower, "unable to extract") || strings.Contains(lower, "signature extraction"):
		result.Message, result.Retry = "YouTube extraction failed. Try updating the download tools in Info.", true
	case strings.Contains(lower, "timeout") || strings.Contains(lower, "temporary failure") || strings.Contains(lower, "name resolution"):
		result.Kind, result.Message = Network, "Check your internet connection and proxy settings."
	}
	return result
}

func ClassifyForSource(raw string, source urlcheck.Source) ClassifiedError {
	if source != urlcheck.SoundCloud {
		return Classify(raw)
	}
	lower := strings.ToLower(raw)
	result := ClassifiedError{Kind: Unknown, Message: "SoundCloud download failed. Try updating yt-dlp in Info, or copy diagnostics for details.", Raw: raw}
	// Match the fatal error, not incidental warnings (the original-download
	// endpoint may require login even while a full guest stream is available).
	if i := strings.LastIndex(lower, "error:"); i >= 0 {
		lower = lower[i:]
	}
	switch {
	case strings.Contains(lower, "429"), strings.Contains(lower, "too many requests"):
		result.Kind, result.Message = RateLimited, "SoundCloud is temporarily limiting requests from your connection. Try again later."
	case strings.Contains(lower, "not available in your country"), strings.Contains(lower, "geo-restricted"), strings.Contains(lower, "region"):
		result.Kind, result.Message = RegionBlocked, "This SoundCloud track is not available in your region."
	case strings.Contains(lower, "private"):
		result.Kind, result.Message = Private, "This SoundCloud track is private or its private link is invalid. Use an accessible track link; ytaudio does not sign in."
	case strings.Contains(lower, "drm"):
		result.Kind, result.Message = Unavailable, "This SoundCloud track is protected by DRM and cannot be downloaded by ytaudio."
	case strings.Contains(lower, "no playable tracks"):
		result.Kind, result.Message = Unavailable, "SoundCloud returned no playable tracks. The playlist may be empty or its tracks unavailable without signing in."
	case strings.Contains(lower, "requested format is not available"), strings.Contains(lower, "no video formats"), strings.Contains(lower, "no formats"), strings.Contains(lower, "preview"), strings.Contains(lower, "log in"), strings.Contains(lower, "login"), strings.Contains(lower, "sign in"), strings.Contains(lower, "registered users"), strings.Contains(lower, "subscription"), strings.Contains(lower, "401"):
		result.Kind, result.Message = Unavailable, "SoundCloud did not provide a full track without signing in. It may require an account or SoundCloud Go+ subscription, or offer only a preview. ytaudio downloads without an account and does not save previews."
	case strings.Contains(lower, "404"), strings.Contains(lower, "not available"), strings.Contains(lower, "unavailable"), strings.Contains(lower, "not found"):
		result.Kind, result.Message = Unavailable, "This SoundCloud track or playlist is unavailable, private, or has been removed."
	case strings.Contains(lower, "403"), strings.Contains(lower, "forbidden"):
		result.Kind, result.Message = Forbidden403, "SoundCloud refused guest access to this track. It may be restricted or temporarily blocked. Try another public track or try again later."
	case strings.Contains(lower, "ffmpeg"), strings.Contains(lower, "postprocessing"):
		result.Kind, result.Message = ConvertFailed, "Audio conversion failed. Re-run dependency setup in Info and try again."
	case strings.Contains(lower, "unable to extract"):
		result.Message, result.Retry = "SoundCloud extraction failed. Try updating yt-dlp in Info.", true
	case strings.Contains(lower, "timeout"), strings.Contains(lower, "timed out"), strings.Contains(lower, "temporary failure"), strings.Contains(lower, "name resolution"), strings.Contains(lower, "connection"):
		result.Kind, result.Message = Network, "Check your internet connection and proxy settings."
	}
	return result
}
