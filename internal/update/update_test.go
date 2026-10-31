package update

import "testing"

func TestNewer(t *testing.T) {
	tests := []struct {
		current string
		latest  string
		want    bool
	}{
		{"dev", "v1.0.0", false},
		{"v1.0.0", "v1.1.0", true},
		{"v1.2.0", "v1.1.0", false},
		{"v1.0.0", "v1.0.0", false},
	}
	for _, test := range tests {
		if got := newer(test.current, test.latest); got != test.want {
			t.Errorf("newer(%q, %q) = %v, want %v", test.current, test.latest, got, test.want)
		}
	}
}
