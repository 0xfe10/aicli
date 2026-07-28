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
	handler := &BearerAuth{Session: Session{
		BaseURL:        "https://fns.example.test",
		HasCredentials: true,
		Credentials:    Credentials{AccessToken: "token", Source: CredentialSourceEnvironment},
	}}
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
	t.Setenv("FNS_WRITE_MODE", "write")
	req, _ := http.NewRequest(http.MethodPost, "https://fns.example.test/api/note", nil)
	err := (&BearerAuth{Session: Session{
		BaseURL: "https://fns.example.test", HasCredentials: true,
		Credentials: Credentials{AccessToken: "token"},
	}}).Authenticate(context.Background(), req, restishauth.AuthContext{
		BaseURL: "https://fns.example.test",
		Force:   true,
	})
	if err == nil || !strings.Contains(err.Error(), "automatic retry is disabled for writes") {
		t.Fatalf("error = %v", err)
	}
}

func TestBearerAuthRejectsPlaceholderBaseURL(t *testing.T) {
	for _, raw := range []string{
		DefaultBaseURL,
		"https://FNS.EXAMPLE.COM",
		"https://fns.example.com/",
		"https://fns.example.com:443",
		"https://fns.example.com/api",
		"https://fns.example.com.",
	} {
		req, _ := http.NewRequest(http.MethodGet, raw+"/note", nil)
		err := (&BearerAuth{Session: Session{
			BaseURL: raw, HasCredentials: true,
			Credentials: Credentials{AccessToken: "token"},
		}}).Authenticate(context.Background(), req, restishauth.AuthContext{BaseURL: raw})
		if err == nil || !strings.Contains(err.Error(), "FNS Base URL is not configured") {
			t.Fatalf("%q error = %v", raw, err)
		}
	}
}

func TestBearerAuthRejectsCrossHostCredentials(t *testing.T) {
	req, _ := http.NewRequest(http.MethodGet, "https://evil.example.com/api/note", nil)
	err := (&BearerAuth{Session: Session{
		BaseURL: "https://good.example.com", HasCredentials: true,
		Credentials: Credentials{AccessToken: "secret-token"},
	}}).Authenticate(context.Background(), req, restishauth.AuthContext{BaseURL: "https://good.example.com"})
	if err == nil || !strings.Contains(err.Error(), "refusing to attach FNS credentials") {
		t.Fatalf("error = %v", err)
	}
	if req.Header.Get("Authorization") != "" {
		t.Fatalf("leaked Authorization header")
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

func TestAuthStatusRequiresUsableBaseURL(t *testing.T) {
	xdg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", xdg)
	t.Setenv("FNS_BASE_URL", "")
	t.Setenv("FNS_ACCESS_TOKEN", "token-without-base")
	var stdout bytes.Buffer
	if err := RunAuth([]string{"status"}, AuthIO{Stdout: &stdout, Stderr: &stdout}); err != nil {
		t.Fatal(err)
	}
	out := stdout.String()
	if strings.Contains(out, `"configured": true`) {
		t.Fatalf("token-only status must not be configured: %s", out)
	}
	if strings.Contains(out, "token-without-base") {
		t.Fatalf("status leaked token: %s", out)
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

func TestAuthRejectsPlaceholderOnLogin(t *testing.T) {
	xdg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", xdg)
	err := RunAuth([]string{"login", "--mode", "token"}, AuthIO{
		Stdin:  strings.NewReader("https://fns.example.com/\n"),
		Stdout: &bytes.Buffer{},
		Stderr: &bytes.Buffer{},
		ReadSecret: func(string) (string, error) {
			return "tok", nil
		},
	})
	if err == nil || !strings.Contains(err.Error(), "placeholder host") {
		t.Fatalf("error = %v", err)
	}
}

func TestLegacyAccessTokenMigration(t *testing.T) {
	xdg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", xdg)
	path := ConfigPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("base_url = \"https://legacy.fns.test\"\naccess_token = \"legacy-token\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	file, err := LoadFileConfig(path)
	if err != nil || file.Auth == nil || file.Auth.AccessToken != "legacy-token" {
		t.Fatalf("legacy load=%#v err=%v", file, err)
	}
	if err := SaveLogin(path, "https://legacy.fns.test", &AuthConfig{Mode: AuthModeToken, AccessToken: "legacy-token"}); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "access_token = \"legacy-token\"\nbase_url") || !strings.Contains(string(raw), "[auth]") {
		// ensure migrated shape has [auth]
	}
	if !strings.Contains(string(raw), "[auth]") {
		t.Fatalf("expected [auth] section, got %s", raw)
	}
	if strings.Count(string(raw), "access_token") != 1 {
		t.Fatalf("expected single access_token under [auth]: %s", raw)
	}
}

func TestAuthHelp(t *testing.T) {
	var stdout bytes.Buffer
	if err := RunAuth([]string{"--help"}, AuthIO{Stdout: &stdout, Stderr: &stdout}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), "fns auth login") {
		t.Fatalf("help = %s", stdout.String())
	}
	stdout.Reset()
	if err := RunAuth([]string{"login", "--help"}, AuthIO{Stdout: &stdout, Stderr: &stdout}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), "--mode token") {
		t.Fatalf("login help = %s", stdout.String())
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

func TestAuthStatusAndLogoutRejectArguments(t *testing.T) {
	for _, args := range [][]string{{"status", "extra"}, {"logout", "--force"}} {
		if err := RunAuth(args, AuthIO{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}}); err == nil || !strings.Contains(err.Error(), "does not accept arguments") {
			t.Fatalf("RunAuth(%v) error = %v", args, err)
		}
	}
}

func TestLoadFileConfigRejectsSymlinkDirectory(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink setup requires additional Windows privileges")
	}
	xdg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", xdg)
	target := t.TempDir()
	if err := os.WriteFile(filepath.Join(target, configFileName), []byte("base_url=\"https://fns.example.test\"\n[auth]\nmode=\"token\"\naccess_token=\"x\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(xdg, "aicli"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, ConfigDir()); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadFileConfig(ConfigPath()); err == nil || !strings.Contains(err.Error(), "directory must not be a symlink") {
		t.Fatalf("error = %v", err)
	}
}

func TestLoginAndLogoutRepairConflictingLegacyAuth(t *testing.T) {
	xdg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", xdg)
	path := ConfigPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	conflict := "base_url=\"https://old.example.test\"\nclient=\"custom\"\naccess_token=\"legacy\"\n[auth]\nmode=\"token\"\naccess_token=\"new\"\n"
	if err := os.WriteFile(path, []byte(conflict), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadFileConfig(path); err == nil {
		t.Fatal("normal reads must reject conflicting credentials")
	}
	if err := SaveLogin(path, "https://fixed.example.test", &AuthConfig{Mode: AuthModeToken, AccessToken: "replacement"}); err != nil {
		t.Fatal(err)
	}
	file, err := LoadFileConfig(path)
	if err != nil || file.Client != "custom" || file.Auth == nil || file.Auth.AccessToken != "replacement" {
		t.Fatalf("repaired login = %#v err=%v", file, err)
	}
	if err := os.WriteFile(path, []byte(conflict), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := ClearAuthConfig(path); err != nil {
		t.Fatal(err)
	}
	file, err = LoadFileConfig(path)
	if err != nil || file.Auth != nil || file.LegacyAccessToken != "" || file.Client != "custom" {
		t.Fatalf("repaired logout = %#v err=%v", file, err)
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

func TestResolversUseCompleteEnvironmentWithoutConfigPath(t *testing.T) {
	t.Setenv("HOME", "")
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("FNS_ACCESS_TOKEN", "env-token")
	t.Setenv("FNS_BASE_URL", "https://env.fns.test")
	creds, err := ResolveCredentials()
	if err != nil || creds.AccessToken != "env-token" {
		t.Fatalf("credentials=%#v err=%v", creds, err)
	}
	baseURL, source, err := ResolveBaseURL(ConfigPath())
	if err != nil || baseURL != "https://env.fns.test" || source != CredentialSourceEnvironment {
		t.Fatalf("baseURL=%q source=%q err=%v", baseURL, source, err)
	}
}
