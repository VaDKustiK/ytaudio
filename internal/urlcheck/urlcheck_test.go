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
