package hastrt

import (
	"bytes"
	"context"

	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"testing"
	"time"

	"github.com/0xfe10/aicli/internal/authflow"
	restish "github.com/rest-sh/restish/v2"
	restishauth "github.com/rest-sh/restish/v2/auth"
)

func TestAuthSafetyAndOrigin(t *testing.T) {
	a := &HeaderAuth{Session: Session{BaseURL: "https://api.example.test", HasCredentials: true, Credentials: Credentials{APIKey: "test-secret"}}}
	for _, tc := range []struct {
		method, path, mode string
		allowed            bool
	}{
		{"GET", "/projects", "", true}, {"POST", "/projects", "", false}, {"POST", "/projects", "write", true},
		{"POST", "/projects/foo/issues/bulk/delete", "write", false}, {"POST", "/projects/foo/issues/bulk/delete", "destructive", true},
		{"DELETE", "/projects/foo", "write", false}, {"DELETE", "/projects/foo", "destructive", true}, {"GET", "/projects", "invalid", false},
	} {
		t.Setenv("HAST_WRITE_MODE", tc.mode)
		req, _ := http.NewRequest(tc.method, "https://api.example.test"+tc.path, nil)
		err := a.Authenticate(context.Background(), req, restishauth.AuthContext{})
		if (err == nil) != tc.allowed {
			t.Fatalf("%+v: %v", tc, err)
		}
		if tc.allowed && req.Header.Get("Authorization") != "Bearer test-secret" {
			t.Fatal("missing bearer header")
		}
	}
	t.Setenv("HAST_WRITE_MODE", "write")
	req, _ := http.NewRequest("GET", "https://other.test/projects", nil)
	if a.Authenticate(context.Background(), req, restishauth.AuthContext{}) == nil {
		t.Fatal("cross-origin credential leak")
	}
	req, _ = http.NewRequest("POST", "https://api.example.test/projects", nil)
	if a.Authenticate(context.Background(), req, restishauth.AuthContext{Force: true}) == nil {
		t.Fatal("write auth retry allowed")
	}
}

func TestAuthConfigAndContext(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("HAST_API_KEY", "")
	t.Setenv("HAST_BASE_URL", "")
	t.Setenv("HAST_API_BASE", "")
	t.Setenv("HAST_SPEC_URL", "")
	manager := ContextManager()
	sel, _, err := manager.ResolveArgs([]string{"hast", "--context", "kahub"})
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	err = RunAuthWithContext([]string{"login", "--mode", "key"}, authflow.IO{Stdin: strings.NewReader("https://api.test\n"), Stdout: &output, Stderr: &output, ReadSecret: func(string) (string, error) { return "secret-for-test", nil }}, sel)
	if err != nil {
		t.Fatal(err)
	}
	session, cfg, err := LoadSessionWithContext(sel)
	if err != nil {
		t.Fatal(err)
	}
	if !session.HasCredentials || cfg.SpecURL != DefaultSpecURL {
		t.Fatalf("bad session %v", cfg)
	}
	info, _ := os.Stat(ConfigPathFor(sel))
	if info.Mode().Perm() != 0600 {
		t.Fatal("insecure permissions")
	}
	other, _, _ := manager.ResolveArgs([]string{"hast", "--context", "other"})
	otherSession, _, err := LoadSessionWithContext(other)
	if err != nil || otherSession.HasCredentials {
		t.Fatal("context leak")
	}
	t.Setenv("HAST_API_KEY", "environment-secret")
	session, _, _ = LoadSessionWithContext(sel)
	if session.Credentials.APIKey != "environment-secret" {
		t.Fatal("env override failed")
	}
	output.Reset()
	if err := RunAuthWithContext([]string{"status"}, authflow.IO{Stdout: &output}, sel); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(output.String(), "secret") {
		t.Fatal("status leaks secret")
	}
	t.Setenv("HAST_API_KEY", "")
	if err := RunAuthWithContext([]string{"logout"}, authflow.IO{Stdout: &output}, sel); err != nil {
		t.Fatal(err)
	}
	session, _, _ = LoadSessionWithContext(sel)
	if session.HasCredentials || session.BaseURL != "https://api.test" {
		t.Fatal("logout contract failed")
	}
	if err := SaveLogin(ConfigPathFor(sel), "https://api.test/path", &AuthConfig{Mode: "key", APIKey: "test"}); err == nil {
		t.Fatal("subpath accepted")
	}
}

func TestSpecRejectExternalRefs(t *testing.T) {
	_, err := fixSpec([]byte(`{"swagger":"2.0","paths":{},"definitions":{"bad":{"$ref":"https://other.test/spec"}}}`))
	if err == nil {
		t.Fatal("external ref accepted")
	}
}
func TestLiveSpec(t *testing.T) {
	url := os.Getenv("HAST_SPEC_CHECK_URL")
	if url == "" {
		t.Skip("opt-in live spec check")
	}
	req, _ := http.NewRequest("GET", url, nil)
	req.Header.Set("User-Agent", "aicli-spec-check")
	resp, err := (&http.Client{Timeout: 20 * time.Second}).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("spec HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := (SpecLoader{}).LoadWithOptions(body, restish.LoadOptions{}); err != nil {
		t.Fatal(err)
	}
	t.Logf("loaded live spec: %d bytes", len(body))
}
func TestSymlinkConfigRejected(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	target := filepath.Join(dir, "target")
	_ = os.WriteFile(target, []byte(""), 0600)
	if err := os.Symlink(target, path); err != nil {
		t.Skip(err)
	}
	if _, err := LoadFileConfig(path); err == nil {
		t.Fatal("symlink accepted")
	}
}
