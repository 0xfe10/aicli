package authflow

import (
	"net/url"
	"strings"
	"testing"
)

func TestNormalizeBaseURL(t *testing.T) {
	for _, tc := range []struct {
		in, want string
	}{
		{"https://open.pingcode.com/", "https://open.pingcode.com"},
		{"https://open.pingcode.com/v1/", "https://open.pingcode.com/v1"},
		{"http://localhost:9000/", "http://localhost:9000"},
		{"http://127.0.0.1:8080", "http://127.0.0.1:8080"},
		{"http://[::1]:8080/", "http://[::1]:8080"},
		{"http://[::1]/", "http://[::1]"},
		{"https://API.Example.COM.", "https://api.example.com"},
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
		"https://host/a%2Fb/",
		"https://host/a%2fb",
	} {
		if _, err := NormalizeBaseURL(bad); err == nil {
			t.Fatalf("expected error for %q", bad)
		}
	}
}

func TestRequestUnderBaseURL(t *testing.T) {
	base := "https://api.example.com/v1"
	ok, _ := url.Parse("https://api.example.com/v1/items")
	if err := RequestUnderBaseURL(ok, base); err != nil {
		t.Fatal(err)
	}
	badHost, _ := url.Parse("https://evil.example.com/v1/items")
	if err := RequestUnderBaseURL(badHost, base); err == nil {
		t.Fatal("expected host mismatch")
	}
	badPath, _ := url.Parse("https://api.example.com/other/items")
	if err := RequestUnderBaseURL(badPath, base); err == nil {
		t.Fatal("expected path mismatch")
	}
}

func TestRequestUnderEscapedBaseURLPath(t *testing.T) {
	for _, raw := range []string{
		"https://api.example.com/tenant%20one/items",
		"https://api.example.com/tenant%25one/items",
	} {
		req, err := url.Parse(raw)
		if err != nil {
			t.Fatal(err)
		}
		base := strings.TrimSuffix(raw, "/items")
		if err := RequestUnderBaseURL(req, base); err != nil {
			t.Fatalf("RequestUnderBaseURL(%q, %q): %v", raw, base, err)
		}
	}
}

func TestCanonicalHostname(t *testing.T) {
	if got := CanonicalHostname("FNS.EXAMPLE.COM."); got != "fns.example.com" {
		t.Fatalf("got %q", got)
	}
}

func TestRedactSecrets(t *testing.T) {
	in := `Authorization: Bearer abc.def client_secret = "very-secret" access_token=token-value`
	out := RedactSecrets(in)
	for _, secret := range []string{"abc.def", "very-secret", "token-value"} {
		if strings.Contains(out, secret) {
			t.Fatalf("secret %q leaked in %q", secret, out)
		}
	}
}

func TestLocalCommandArgs(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want []string
	}{
		{[]string{"fns", "auth", "status"}, []string{"status"}},
		{[]string{"fns", "-v", "auth", "status"}, []string{"status"}},
		{[]string{"fns", "help", "auth"}, []string{"--help"}},
		{[]string{"fns", "help", "auth", "login"}, []string{"login", "--help"}},
	} {
		got, handled, err := LocalCommandArgs(tc.args, "auth")
		if err != nil || !handled || strings.Join(got, "\x00") != strings.Join(tc.want, "\x00") {
			t.Fatalf("LocalCommandArgs(%v) = %v, %v, %v", tc.args, got, handled, err)
		}
	}
	if _, handled, err := LocalCommandArgs([]string{"fns", "--rsh-header", "auth", "note", "get"}, "auth"); handled || err != nil {
		t.Fatalf("flag value misclassified: handled=%v err=%v", handled, err)
	}
	if _, handled, err := LocalCommandArgs([]string{"pingcode", "--rsh-config", "auth", "logout"}, "auth"); handled || err != nil {
		t.Fatalf("config path misclassified as auth command: handled=%v err=%v", handled, err)
	}
	if _, handled, err := LocalCommandArgs([]string{"fns", "note", "get", "auth"}, "auth"); handled || err != nil {
		t.Fatalf("operation argument misclassified: handled=%v err=%v", handled, err)
	}
}
