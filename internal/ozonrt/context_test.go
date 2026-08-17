package ozonrt

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/0xfe10/aicli/internal/authflow"
)

func TestContextSessionsAndStatusAreIsolated(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	t.Setenv("OZON_CONTEXT", "")
	t.Setenv("OZON_CLIENT_ID", "")
	t.Setenv("OZON_API_KEY", "")

	manager := ContextManager()
	defaultContext := manager.DefaultSelection()
	namedContext, _, err := manager.ResolveArgs([]string{"ozon", "--context", "seller-b"})
	if err != nil {
		t.Fatal(err)
	}
	if err := SaveLogin(ConfigPathFor(defaultContext), DefaultBaseURL, &AuthConfig{
		Mode: AuthModeKey, ClientID: "client-a", APIKey: "key-a",
	}); err != nil {
		t.Fatal(err)
	}
	if err := SaveLogin(ConfigPathFor(namedContext), DefaultBaseURL, &AuthConfig{
		Mode: AuthModeKey, ClientID: "client-b", APIKey: "key-b",
	}); err != nil {
		t.Fatal(err)
	}

	defaultSession, _, err := LoadSessionWithContext(defaultContext)
	if err != nil {
		t.Fatal(err)
	}
	namedSession, _, err := LoadSessionWithContext(namedContext)
	if err != nil {
		t.Fatal(err)
	}
	if defaultSession.Credentials.ClientID != "client-a" || defaultSession.Credentials.APIKey != "key-a" {
		t.Fatalf("default session = %#v", defaultSession)
	}
	if namedSession.Credentials.ClientID != "client-b" || namedSession.Credentials.APIKey != "key-b" {
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
	if report.Context != "seller-b" || report.ContextSource != "flag" || report.ConfigPath != ConfigPathFor(namedContext) {
		t.Fatalf("status = %#v", report)
	}
	if err := RunAuthWithContext([]string{"logout"}, authflow.IO{Stdout: &bytes.Buffer{}}, namedContext); err != nil {
		t.Fatal(err)
	}
	if session, _, err := LoadSessionWithContext(defaultContext); err != nil || !session.HasCredentials {
		t.Fatalf("default session after named logout = %#v, err = %v", session, err)
	}
	if session, _, err := LoadSessionWithContext(namedContext); err != nil || session.HasCredentials {
		t.Fatalf("named session after logout = %#v, err = %v", session, err)
	}
}
