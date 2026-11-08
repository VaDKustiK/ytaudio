package urlcheck

import "testing"

func TestNormalize(t *testing.T) {
	tests := []struct {
		raw  string
		want string
		ok   bool
	}{
		{"https://www.youtube.com/watch?v=abc&utm_source=x", "https://www.youtube.com/watch?v=abc", true},
		{"https://youtu.be/abc", "https://youtu.be/abc", true},
		{"https://youtube.com.evil.example/watch?v=abc", "", false},
		{"https://user:pass@youtube.com/watch?v=abc", "", false},
		{"javascript:alert(1)", "", false},
		{"-https://youtube.com/watch?v=abc", "", false},
	}
	for _, test := range tests {
		got, err := Normalize(test.raw)
		if (err == nil) != test.ok || got != test.want {
			t.Errorf("Normalize(%q) = %q, %v; want %q, ok=%v", test.raw, got, err, test.want, test.ok)
		}
	}
}

func TestRejectRedirectsAndPageURLs(t *testing.T) {
	for _, u := range []string{"https://youtube.com/redirect?q=https://evil.test", "https://youtube.com/@channel", "https://youtube.com/watch", "https://youtu.be/../x", "https://youtube.com:999/watch?v=x", "https://youtube.com/playlist?list=../../x"} {
		if _, err := Normalize(u); err == nil {
			t.Fatal(u)
		}
	}
}
func TestStripsUnknownQueryData(t *testing.T) {
	got, err := Normalize("https://youtube.com/watch?v=abc&secret=private&t=25&si=tracking")
	if err != nil || got != "https://youtube.com/watch?v=abc" {
		t.Fatalf("%s %v", got, err)
	}
}
