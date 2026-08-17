package pingcodert

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/0xfe10/aicli/internal/authflow"
	"github.com/0xfe10/aicli/internal/restishengine"
	restishauth "github.com/rest-sh/restish/v2/auth"
)

func TestContextSessionsAndTokenCachesAreIsolated(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	t.Setenv("PINGCODE_CONTEXT", "")
	t.Setenv("PINGCODE_ACCESS_TOKEN", "")
	t.Setenv("PINGCODE_CLIENT_ID", "")
	t.Setenv("PINGCODE_CLIENT_SECRET", "")

	manager := ContextManager()
	defaultContext := manager.DefaultSelection()
	namedContext, _, err := manager.ResolveArgs([]string{"pingcode", "--context", "company-b"})
	if err != nil {
		t.Fatal(err)
	}
	if err := SaveLogin(ConfigPathFor(defaultContext), "https://company-a.example.test", &AuthConfig{
		Mode: AuthModeToken, AccessToken: "token-a",
	}); err != nil {
		t.Fatal(err)
	}
	if err := SaveLogin(ConfigPathFor(namedContext), "https://company-b.example.test", &AuthConfig{
		Mode: AuthModeToken, AccessToken: "token-b",
	}); err != nil {
		t.Fatal(err)
	}

	defaultSession, err := LoadSessionWithContext(defaultContext)
	if err != nil {
		t.Fatal(err)
	}
	namedSession, err := LoadSessionWithContext(namedContext)
	if err != nil {
		t.Fatal(err)
	}
	if defaultSession.BaseURL != "https://company-a.example.test" || defaultSession.Credentials.AccessToken != "token-a" {
		t.Fatalf("default session = %#v", defaultSession)
	}
	if namedSession.BaseURL != "https://company-b.example.test" || namedSession.Credentials.AccessToken != "token-b" {
		t.Fatalf("named session = %#v", namedSession)
	}

	defaultTokens := restishauth.NewTokenCache(restishengine.TokenCachePath(defaultContext.ConfigDir))
	namedTokens := restishauth.NewTokenCache(restishengine.TokenCachePath(namedContext.ConfigDir))
	if err := defaultTokens.Set("pingcode:default", restishauth.CachedToken{AccessToken: "cached-a"}); err != nil {
		t.Fatal(err)
	}
	if err := namedTokens.Set("pingcode:named", restishauth.CachedToken{AccessToken: "cached-b"}); err != nil {
		t.Fatal(err)
	}
	if err := clearCachedClientCredentialsTokensFor(namedContext); err != nil {
		t.Fatal(err)
	}
	if token, err := defaultTokens.Get("pingcode:default"); err != nil || token == nil || token.AccessToken != "cached-a" {
		t.Fatalf("default cached token = %#v, err = %v", token, err)
	}
	if token, err := namedTokens.Get("pingcode:named"); err != nil || token != nil {
		t.Fatalf("named cached token = %#v, err = %v", token, err)
	}
}

func TestAuthStatusReportsSelectedContext(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	t.Setenv("PINGCODE_ACCESS_TOKEN", "")
	t.Setenv("PINGCODE_CLIENT_ID", "")
	t.Setenv("PINGCODE_CLIENT_SECRET", "")
	selection, _, err := ContextManager().ResolveArgs([]string{"pingcode", "--context", "company-b"})
	if err != nil {
		t.Fatal(err)
	}
	if err := SaveLogin(ConfigPath(), "https://company-a.example.test", &AuthConfig{
		Mode: AuthModeToken, AccessToken: "token-a",
	}); err != nil {
		t.Fatal(err)
	}
	if err := SaveLogin(ConfigPathFor(selection), "https://company-b.example.test", &AuthConfig{
		Mode: AuthModeToken, AccessToken: "token-b",
	}); err != nil {
		t.Fatal(err)
	}
	var stdout bytes.Buffer
	if err := RunAuthWithContext([]string{"status"}, authflow.IO{Stdout: &stdout}, selection); err != nil {
		t.Fatal(err)
	}
	var report authflow.StatusReport
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if report.Context != "company-b" || report.ContextSource != "flag" || report.ConfigPath != ConfigPathFor(selection) {
		t.Fatalf("status = %#v", report)
	}
	if err := RunAuthWithContext([]string{"logout"}, authflow.IO{Stdout: &bytes.Buffer{}}, selection); err != nil {
		t.Fatal(err)
	}
	if session, err := LoadSessionWithContext(ContextManager().DefaultSelection()); err != nil || !session.HasCredentials {
		t.Fatalf("default session after named logout = %#v, err = %v", session, err)
	}
	if session, err := LoadSessionWithContext(selection); err != nil || session.HasCredentials {
		t.Fatalf("named session after logout = %#v, err = %v", session, err)
	}
}
