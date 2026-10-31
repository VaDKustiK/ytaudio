package ytdlp

import "strings"

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
		result.Kind, result.Message, result.Retry = Forbidden403, "The download stream was refused. yt-dlp will be updated and retried once.", true
	case strings.Contains(lower, "private video") || strings.Contains(lower, "video is private"):
		result.Kind, result.Message = Private, "This video is private."
	case strings.Contains(lower, "age-restricted"):
		result.Kind, result.Message = AgeRestricted, "This age-restricted video cannot be downloaded without browser cookies."
	case strings.Contains(lower, "not available") || strings.Contains(lower, "unavailable"):
		result.Kind, result.Message = Unavailable, "This video is unavailable."
	case strings.Contains(lower, "not available in your country") || strings.Contains(lower, "region"):
		result.Kind, result.Message = RegionBlocked, "This video is not available in your region."
	case strings.Contains(lower, "livestream") || strings.Contains(lower, "live stream"):
		result.Kind, result.Message = Livestream, "Live streams are not supported."
	case strings.Contains(lower, "ffmpeg") || strings.Contains(lower, "postprocessing"):
		result.Kind, result.Message = ConvertFailed, "Audio conversion failed. Re-run dependency setup and try again."
	case strings.Contains(lower, "timeout") || strings.Contains(lower, "temporary failure") || strings.Contains(lower, "name resolution"):
		result.Kind, result.Message = Network, "Check your internet connection and proxy settings."
	}
	return result
}
