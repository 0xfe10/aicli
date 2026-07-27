package authflow

import "testing"

func TestNormalizeBaseURL(t *testing.T) {
	for _, tc := range []struct {
		in, want string
	}{
		{"https://open.pingcode.com/", "https://open.pingcode.com"},
		{"https://open.pingcode.com/v1/", "https://open.pingcode.com/v1"},
		{"http://localhost:9000/", "http://localhost:9000"},
		{"http://127.0.0.1:8080", "http://127.0.0.1:8080"},
	} {
		got, err := NormalizeBaseURL(tc.in)
		if err != nil || got != tc.want {
			t.Fatalf("%q => %q err=%v, want %q", tc.in, got, err, tc.want)
		}
	}
	for _, bad := range []string{
		"",
		"open.pingcode.com",
		"ftp://example.com",
		"http://example.com",
		"https://user:pass@example.com",
		"https://example.com?x=1",
		"https://example.com#frag",
	} {
		if _, err := NormalizeBaseURL(bad); err == nil {
			t.Fatalf("expected error for %q", bad)
		}
	}
}
