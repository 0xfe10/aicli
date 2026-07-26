package redact_test

import (
	"strings"
	"testing"

	"github.com/0xfe10/aicli/internal/redact"
)

func TestStringRedactsSecrets(t *testing.T) {
	in := `access_token=abc123&refresh_token=def456&client_secret=zzz Authorization: Bearer tok.en-1 code=authcode secret=s1 token=t1`
	out := redact.String(in)
	for _, secret := range []string{"abc123", "def456", "zzz", "tok.en-1", "authcode", "s1", "t1"} {
		if strings.Contains(out, secret) {
			t.Fatalf("secret %q leaked in %q", secret, out)
		}
	}
	if !strings.Contains(out, "Authorization: ***") && !strings.Contains(out, "Bearer ***") {
		t.Fatalf("expected authorization mask: %q", out)
	}
}

func TestStringKeepsBusinessFields(t *testing.T) {
	in := `{"title":"fix access panel","state":"处理中","identifier":"DEMO-1"}`
	out := redact.String(in)
	if out != in {
		t.Fatalf("over-redacted business JSON: %q", out)
	}
}

func TestHeaderValue(t *testing.T) {
	if got := redact.HeaderValue("Authorization", "Bearer secret-token"); got != "Bearer ***" {
		t.Fatalf("got %q", got)
	}
}
