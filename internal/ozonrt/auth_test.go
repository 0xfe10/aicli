package ozonrt

import (
	"context"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	restishauth "github.com/rest-sh/restish/v2/auth"
)

func TestSaveLoginRoundTripAndLogout(t *testing.T) {
	path := filepath.Join(t.TempDir(), "aicli", "ozon", "config.toml")
	if err := SaveLogin(path, "https://api-seller.ozon.ru/", &AuthConfig{
		Mode: AuthModeKey, ClientID: "123", APIKey: "secret",
	}); err != nil {
		t.Fatal(err)
	}
	file, err := LoadFileConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if file.BaseURL != DefaultBaseURL || file.Auth == nil || file.Auth.ClientID != "123" || file.Auth.APIKey != "secret" {
		t.Fatalf("loaded config = %#v", file)
	}
	if info, err := os.Stat(path); err != nil {
		t.Fatal(err)
	} else if info.Mode().Perm() != 0o600 {
		t.Fatalf("config mode = %o", info.Mode().Perm())
	}
	if err := ClearAuthConfig(path); err != nil {
		t.Fatal(err)
	}
	file, err = LoadFileConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if file.Auth != nil || file.BaseURL != DefaultBaseURL {
		t.Fatalf("logout config = %#v", file)
	}
}

func TestEnvironmentCredentialsMustBeComplete(t *testing.T) {
	_, _, err := resolveCredentials(FileConfig{}, environmentSnapshot{ClientID: "only-client"})
	if err == nil || !strings.Contains(err.Error(), "must be set together") {
		t.Fatalf("error = %v", err)
	}
}

func TestHeaderAuthBlocksUnauthorizedWriteRetry(t *testing.T) {
	policy := &SafetyPolicy{}
	policy.Replace([]safetyRoute{{Method: http.MethodPost, Path: "/v3/product/import", Level: "write"}})
	req := &http.Request{Method: http.MethodPost, URL: &url.URL{Scheme: "https", Host: "api-seller.ozon.ru", Path: "/v3/product/import"}, Header: http.Header{}}
	auth := HeaderAuth{
		Session: Session{BaseURL: DefaultBaseURL, HasCredentials: true, Credentials: Credentials{ClientID: "client", APIKey: "secret"}},
		Policy:  policy,
	}
	err := auth.Authenticate(context.Background(), req, restishauth.AuthContext{Force: true})
	if err == nil || !strings.Contains(err.Error(), "automatic retry is disabled") {
		t.Fatalf("error = %v", err)
	}
}

func TestRedactSecrets(t *testing.T) {
	for _, input := range []string{"Api-Key: super-secret", "OZON_API_KEY=super-secret"} {
		if got := RedactSecrets(input); strings.Contains(got, "super-secret") {
			t.Fatalf("secret leaked: %q", got)
		}
	}
}
