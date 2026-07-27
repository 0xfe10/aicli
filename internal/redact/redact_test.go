package redact_test

import (
	"encoding/json"
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
	if got := redact.HeaderValue("Set-Cookie", "session=super-secret; Path=/"); got != "***" {
		t.Fatalf("set-cookie got %q", got)
	}
	if got := redact.HeaderValue("Cookie", "a=b"); got != "***" {
		t.Fatalf("cookie got %q", got)
	}
}

func TestNormalizeKeyVariants(t *testing.T) {
	cases := []string{"access_token", "accessToken", "Access-Token", "ACCESS_TOKEN", "access-token"}
	for _, k := range cases {
		if !redact.IsSensitiveKey(k) {
			t.Fatalf("%q should be sensitive", k)
		}
	}
	for _, k := range []string{"code", "message", "title", "identifier", "state"} {
		if redact.IsSensitiveKey(k) {
			t.Fatalf("%q should not be sensitive", k)
		}
	}
	for _, k := range []string{"authorizationCode", "auth_code", "clientSecret", "refreshToken", "token", "secret"} {
		if !redact.IsSensitiveKey(k) {
			t.Fatalf("%q should be sensitive", k)
		}
	}
}

func TestValueRedactsNestedJSONSecrets(t *testing.T) {
	raw := map[string]any{
		"accessToken": "tok-live-secret",
		"nested": map[string]any{
			"Authorization":     "Bearer echo-secret",
			"authorizationCode": "auth-code-secret",
			"client_secret":     "client-secret-value",
			"refresh-token":     "refresh-secret",
			"title":             "keep-me",
			"code":              "100009",
		},
		"items": []any{
			map[string]any{"refreshToken": "refresh-secret-2", "id": "1"},
		},
		"note": "Bearer freeform-secret",
	}
	out, ok := redact.Value(raw).(map[string]any)
	if !ok {
		t.Fatalf("type=%T", redact.Value(raw))
	}
	encoded, err := json.Marshal(out)
	if err != nil {
		t.Fatal(err)
	}
	text := string(encoded)
	for _, secret := range []string{
		"tok-live-secret", "echo-secret", "auth-code-secret",
		"client-secret-value", "refresh-secret", "refresh-secret-2", "freeform-secret",
	} {
		if strings.Contains(text, secret) {
			t.Fatalf("secret %q leaked in %s", secret, text)
		}
	}
	nested := out["nested"].(map[string]any)
	if nested["title"] != "keep-me" {
		t.Fatalf("business field lost: %#v", nested)
	}
	if nested["code"] != "100009" {
		t.Fatalf("business code over-redacted: %#v", nested["code"])
	}
}
