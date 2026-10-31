package ytdlp

import "testing"

func TestClassify(t *testing.T) {
	tests := []struct {
		input string
		kind  ErrorKind
		retry bool
	}{
		{"HTTP Error 429: Too Many Requests", RateLimited, false},
		{"Sign in to confirm you're not a bot", BotCheck, false},
		{"ERROR: HTTP Error 403: Forbidden", Forbidden403, true},
		{"This video is private", Private, false},
		{"ffmpeg not found", ConvertFailed, false},
		{"network timeout", Network, false},
	}
	for _, test := range tests {
		got := Classify(test.input)
		if got.Kind != test.kind || got.Retry != test.retry {
			t.Errorf("Classify(%q) = %#v", test.input, got)
		}
	}
}
