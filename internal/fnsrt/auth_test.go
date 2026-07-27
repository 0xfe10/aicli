package fnsrt

import (
	"bytes"
	"context"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"

	"github.com/0xfe10/aicli/internal/authflow"
	restishauth "github.com/rest-sh/restish/v2/auth"
)

func TestBearerAuthWritePolicy(t *testing.T) {
	handler := &BearerAuth{}
	for _, test := range []struct {
		mode, method string
		wantError    bool
	}{
		{"", http.MethodPost, true},
		{"readonly", http.MethodGet, false},
		{"write", http.MethodPatch, false},
		{"write", http.MethodDelete, true},
		{"destructive", http.MethodDelete, false},
		{"invalid", http.MethodGet, true},
	} {
		t.Run(test.mode+test.method, func(t *testing.T) {
			t.Setenv("FNS_ACCESS_TOKEN", "token")
			t.Setenv("FNS_WRITE_MODE", test.mode)
			req, _ := http.NewRequest(test.method, "https://fns.example.test/api/note", nil)
			err := handler.Authenticate(context.Background(), req, restishauth.AuthContext{BaseURL: "https://fns.example.test"})
			if (err != nil) != test.wantError {
				t.Fatalf("error = %v, wantError=%v", err, test.wantError)
			}
			if err == nil && req.Header.Get("Authorization") != "Bearer token" {
				t.Fatalf("Authorization = %q", req.Header.Get("Authorization"))
			}
		})
	}
}

func TestBearerAuthRejectsWriteForceRetry(t *testing.T) {
	t.Setenv("FNS_ACCESS_TOKEN", "token")
	t.Setenv("FNS_WRITE_MODE", "write")
	req, _ := http.NewRequest(http.MethodPost, "https://fns.example.test/api/note", nil)
	err := (&BearerAuth{}).Authenticate(context.Background(), req, restishauth.AuthContext{
		BaseURL: "https://fns.example.test",
		Force:   true,
	})
	if err == nil || !strings.Contains(err.Error(), "automatic retry is disabled for writes") {
		t.Fatalf("error = %v", err)
	}
}

func TestBearerAuthRejectsPlaceholderBaseURL(t *testing.T) {
	t.Setenv("FNS_ACCESS_TOKEN", "token")
	t.Setenv("FNS_WRITE_MODE", "readonly")
	req, _ := http.NewRequest(http.MethodGet, DefaultBaseURL+"/api/note", nil)
	err := (&BearerAuth{}).Authenticate(context.Background(), req, restishauth.AuthContext{BaseURL: DefaultBaseURL})
	if err == nil || !strings.Contains(err.Error(), "FNS Base URL is not configured") {
		t.Fatalf("error = %v", err)
	}
}

func TestAuthConfigPermissionsAndStatus(t *testing.T) {
	xdg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", xdg)
	t.Setenv("FNS_ACCESS_TOKEN", "")
	t.Setenv("FNS_BASE_URL", "")

	var stdout, stderr bytes.Buffer
	authIO := AuthIO{
		Stdin:  strings.NewReader("https://obsidian-fns.example.org/\n"),
		Stdout: &stdout,
		Stderr: &stderr,
		ReadSecret: func(string) (string, error) {
			return "super-secret-token", nil
		},
	}
	if err := RunAuth([]string{"login", "--mode", "token"}, authIO); err != nil {
		t.Fatal(err)
	}
	path := ConfigPath()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0o077 != 0 {
		t.Fatalf("config permissions = %v", info.Mode())
	}
	file, err := LoadFileConfig(path)
	if err != nil || file.BaseURL != "https://obsidian-fns.example.org" || file.Auth == nil || file.Auth.AccessToken != "super-secret-token" {
		t.Fatalf("login file=%#v err=%v", file, err)
	}

	stdout.Reset()
	if err := RunAuth([]string{"status"}, authIO); err != nil {
		t.Fatal(err)
	}
	status := stdout.String()
	if strings.Contains(status, "super-secret-token") {
		t.Fatalf("status leaked token: %s", status)
	}
	if !strings.Contains(status, `"configured": true`) ||
		!strings.Contains(status, `"credentialSource": "config"`) ||
		!strings.Contains(status, `"baseUrl": "https://obsidian-fns.example.org"`) ||
		!strings.Contains(status, `"mode": "token"`) {
		t.Fatalf("status = %s", status)
	}

	creds, err := ResolveCredentials()
	if err != nil || creds.AccessToken != "super-secret-token" || creds.Source != CredentialSourceConfig {
		t.Fatalf("creds=%#v err=%v", creds, err)
	}

	t.Setenv("FNS_ACCESS_TOKEN", "env-token")
	creds, err = ResolveCredentials()
	if err != nil || creds.AccessToken != "env-token" || creds.Source != CredentialSourceEnvironment {
		t.Fatalf("env creds=%#v err=%v", creds, err)
	}

	stdout.Reset()
	stderr.Reset()
	t.Setenv("FNS_ACCESS_TOKEN", "")
	if err := RunAuth([]string{"logout"}, authIO); err != nil {
		t.Fatal(err)
	}
	file, err = LoadFileConfig(path)
	if err != nil || file.Auth != nil || file.BaseURL != "https://obsidian-fns.example.org" {
		t.Fatalf("logout left unexpected state: %#v err=%v", file, err)
	}
}

func TestAuthLoginEmptyInputDoesNotModifyConfig(t *testing.T) {
	xdg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", xdg)
	path := ConfigPath()
	if err := SaveLogin(path, "https://obsidian-fns.example.org", &AuthConfig{Mode: AuthModeToken, AccessToken: "keep-me"}); err != nil {
		t.Fatal(err)
	}
	var stdout bytes.Buffer
	err := RunAuth([]string{"login", "--mode", "token"}, AuthIO{
		Stdin:  strings.NewReader("\n"),
		Stdout: &stdout,
		Stderr: &stdout,
		ReadSecret: func(string) (string, error) {
			t.Fatal("secret should not be prompted when Base URL is empty")
			return "", nil
		},
	})
	if err == nil || !strings.Contains(err.Error(), "Base URL is required") {
		t.Fatalf("error = %v", err)
	}
	file, loadErr := LoadFileConfig(path)
	if loadErr != nil || file.Auth == nil || file.Auth.AccessToken != "keep-me" {
		t.Fatalf("config mutated: %#v err=%v", file, loadErr)
	}
}

func TestAuthConfigPermissionsIgnoreUmask(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix permission checks")
	}
	xdg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", xdg)
	old := syscall.Umask(0o077)
	defer syscall.Umask(old)
	path := ConfigPath()
	if err := SaveLogin(path, "https://obsidian-fns.example.org", &AuthConfig{Mode: AuthModeToken, AccessToken: "tok"}); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("file perm = %o", info.Mode().Perm())
	}
	dirInfo, err := os.Stat(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	if dirInfo.Mode().Perm() != 0o700 {
		t.Fatalf("dir perm = %o", dirInfo.Mode().Perm())
	}
}

func TestRedactSecrets(t *testing.T) {
	in := `Authorization: Bearer abc.def.ghi access_token=xyz token: secret`
	out := RedactSecrets(in)
	if strings.Contains(out, "abc.def.ghi") || strings.Contains(out, "xyz") || strings.Contains(out, "secret") {
		t.Fatalf("not redacted: %s", out)
	}
	if !strings.Contains(out, "Bearer ***") || !strings.Contains(out, "access_token=***") {
		t.Fatalf("unexpected redaction: %s", out)
	}
}

func TestAuthRejectsArgvSecrets(t *testing.T) {
	err := RunAuth([]string{"login", "--mode", "token", "--access-token", "x"}, AuthIO{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
	if err == nil || !strings.Contains(err.Error(), "not supported") {
		t.Fatalf("error = %v", err)
	}
}

func TestResolveBaseURLPrecedence(t *testing.T) {
	xdg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", xdg)
	t.Setenv("FNS_BASE_URL", "")
	path := ConfigPath()
	if err := SaveLogin(path, "https://file.fns.test", &AuthConfig{Mode: AuthModeToken, AccessToken: "tok"}); err != nil {
		t.Fatal(err)
	}
	got, source, err := ResolveBaseURL(path)
	if err != nil || got != "https://file.fns.test" || source != authflow.SourceConfig {
		t.Fatalf("config base=%q source=%q err=%v", got, source, err)
	}
	t.Setenv("FNS_BASE_URL", "https://env.fns.test/")
	got, source, err = ResolveBaseURL(path)
	if err != nil || got != "https://env.fns.test" || source != authflow.SourceEnvironment {
		t.Fatalf("env base=%q source=%q err=%v", got, source, err)
	}
}
