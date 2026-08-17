package fnsrt

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/0xfe10/aicli/internal/authflow"
)

func TestContextSessionsAndStatusAreIsolated(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	t.Setenv("FNS_CONTEXT", "")
	t.Setenv("FNS_ACCESS_TOKEN", "")
	t.Setenv("FNS_BASE_URL", "")

	manager := ContextManager()
	defaultContext := manager.DefaultSelection()
	namedContext, _, err := manager.ResolveArgs([]string{"fns", "--context", "personal"})
	if err != nil {
		t.Fatal(err)
	}
	if err := SaveLogin(ConfigPathFor(defaultContext), "https://work.example.test", &AuthConfig{
		Mode: AuthModeToken, AccessToken: "work-token",
	}); err != nil {
		t.Fatal(err)
	}
	if err := SaveLogin(ConfigPathFor(namedContext), "https://personal.example.test", &AuthConfig{
		Mode: AuthModeToken, AccessToken: "personal-token",
	}); err != nil {
		t.Fatal(err)
	}

	defaultSession, _, err := LoadSessionWithContext("test", defaultContext)
	if err != nil {
		t.Fatal(err)
	}
	namedSession, _, err := LoadSessionWithContext("test", namedContext)
	if err != nil {
		t.Fatal(err)
	}
	if defaultSession.BaseURL != "https://work.example.test" || defaultSession.Credentials.AccessToken != "work-token" {
		t.Fatalf("default session = %#v", defaultSession)
	}
	if namedSession.BaseURL != "https://personal.example.test" || namedSession.Credentials.AccessToken != "personal-token" {
		t.Fatalf("named session = %#v", namedSession)
	}

	var stdout bytes.Buffer
	if err := RunAuthWithContext([]string{"status"}, authflow.IO{Stdout: &stdout}, namedContext); err != nil {
		t.Fatal(err)
	}
	var report authflow.StatusReport
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if report.Context != "personal" || report.ContextSource != "flag" || report.ConfigPath != ConfigPathFor(namedContext) {
		t.Fatalf("status = %#v", report)
	}
	if err := RunAuthWithContext([]string{"logout"}, authflow.IO{Stdout: &bytes.Buffer{}}, namedContext); err != nil {
		t.Fatal(err)
	}
	if session, _, err := LoadSessionWithContext("test", defaultContext); err != nil || !session.HasCredentials {
		t.Fatalf("default session after named logout = %#v, err = %v", session, err)
	}
	if session, _, err := LoadSessionWithContext("test", namedContext); err != nil || session.HasCredentials {
		t.Fatalf("named session after logout = %#v, err = %v", session, err)
	}
}
