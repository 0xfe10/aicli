package pingcodert

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"

	"github.com/0xfe10/aicli/internal/authflow"
	restishauth "github.com/rest-sh/restish/v2/auth"
)

func umask(mask int) int { return syscall.Umask(mask) }

func TestAuthConfigRoundTripClientAndToken(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	path := ConfigPath()

	if err := SaveLogin(path, "https://open.pingcode.com/", &AuthConfig{
		Mode: AuthModeClient, ClientID: "id", ClientSecret: "secret",
	}); err != nil {
		t.Fatal(err)
	}
	file, err := LoadFileConfig(path)
	if err != nil || file.BaseURL != "https://open.pingcode.com" || file.Auth == nil ||
		file.Auth.Mode != AuthModeClient || file.Auth.ClientID != "id" || file.Auth.ClientSecret != "secret" {
		t.Fatalf("client load = %#v err=%v", file, err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Fatalf("file perm = %o", info.Mode().Perm())
	}
	dirInfo, err := os.Stat(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && dirInfo.Mode().Perm() != 0o700 {
		t.Fatalf("dir perm = %o", dirInfo.Mode().Perm())
	}

	if err := SaveLogin(path, "https://open.pingcode.com", &AuthConfig{Mode: AuthModeToken, AccessToken: "tok"}); err != nil {
		t.Fatal(err)
	}
	got, err := LoadAuthConfig(path)
	if err != nil || got.Mode != AuthModeToken || got.AccessToken != "tok" || got.ClientID != "" {
		t.Fatalf("token load = %#v err=%v", got, err)
	}
}

func TestAuthConfigRejectsSymlinkAndWorldReadable(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix permission checks")
	}
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	path := ConfigPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}

	wide := path + ".wide"
	if err := os.WriteFile(wide, []byte("base_url=\"https://open.pingcode.com\"\n[auth]\nmode=\"token\"\naccess_token=\"x\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(wide, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadAuthConfig(wide); err == nil || !strings.Contains(err.Error(), "0600") {
		t.Fatalf("wide file error = %v", err)
	}

	target := path + ".target"
	if err := os.WriteFile(target, []byte("base_url=\"https://open.pingcode.com\"\n[auth]\nmode=\"token\"\naccess_token=\"x\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := path + ".link"
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadAuthConfig(link); err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("symlink error = %v", err)
	}
}

func TestAuthConfigAtomicReplaceAndClearPreservesBaseURL(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	path := ConfigPath()
	if err := SaveLogin(path, "https://open.pingcode.com", &AuthConfig{Mode: AuthModeToken, AccessToken: "one"}); err != nil {
		t.Fatal(err)
	}
	if err := SaveLogin(path, "https://open.pingcode.com", &AuthConfig{Mode: AuthModeToken, AccessToken: "two"}); err != nil {
		t.Fatal(err)
	}
	got, err := LoadAuthConfig(path)
	if err != nil || got.AccessToken != "two" {
		t.Fatalf("got=%#v err=%v", got, err)
	}
	if err := ClearAuthConfig(path); err != nil {
		t.Fatal(err)
	}
	file, err := LoadFileConfig(path)
	if err != nil || file.Auth != nil || file.BaseURL != "https://open.pingcode.com" {
		t.Fatalf("cleared=%#v err=%v", file, err)
	}
}

func TestAuthLoginEmptyInputDoesNotModifyConfig(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	path := ConfigPath()
	if err := SaveLogin(path, "https://open.pingcode.com", &AuthConfig{Mode: AuthModeToken, AccessToken: "keep-me"}); err != nil {
		t.Fatal(err)
	}
	var stdout strings.Builder
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

func TestResolveCredentialsPrecedence(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	path := ConfigPath()
	if err := SaveLogin(path, "https://open.pingcode.com", &AuthConfig{Mode: AuthModeClient, ClientID: "file-id", ClientSecret: "file-secret"}); err != nil {
		t.Fatal(err)
	}

	t.Setenv("PINGCODE_ACCESS_TOKEN", "")
	t.Setenv("PINGCODE_CLIENT_ID", "")
	t.Setenv("PINGCODE_CLIENT_SECRET", "")
	creds, err := resolveCredentials(path)
	if err != nil || creds.Source != CredentialSourceConfig || creds.ClientID != "file-id" {
		t.Fatalf("config creds=%#v err=%v", creds, err)
	}

	t.Setenv("PINGCODE_CLIENT_ID", "env-id")
	if _, err := resolveCredentials(path); err == nil || !strings.Contains(err.Error(), "both PINGCODE_CLIENT_ID and PINGCODE_CLIENT_SECRET") {
		t.Fatalf("incomplete client env error = %v", err)
	}

	t.Setenv("PINGCODE_CLIENT_SECRET", "env-secret")
	creds, err = resolveCredentials(path)
	if err != nil || creds.Source != CredentialSourceEnvironment || creds.ClientID != "env-id" || creds.ClientSecret != "env-secret" {
		t.Fatalf("env client=%#v err=%v", creds, err)
	}

	t.Setenv("PINGCODE_ACCESS_TOKEN", "env-token")
	creds, err = resolveCredentials(path)
	if err != nil || creds.Mode != AuthModeToken || creds.AccessToken != "env-token" || creds.Source != CredentialSourceEnvironment {
		t.Fatalf("env token=%#v err=%v", creds, err)
	}

	fileAuth, err := LoadAuthConfig(path)
	if err != nil || fileAuth.ClientID != "file-id" {
		t.Fatalf("file mutated: %#v err=%v", fileAuth, err)
	}
}

func TestResolveBaseURLPrecedence(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("PINGCODE_API_BASE_URL", "")
	path := ConfigPath()
	if err := SaveLogin(path, "https://file.pingcode.test", &AuthConfig{Mode: AuthModeToken, AccessToken: "tok"}); err != nil {
		t.Fatal(err)
	}
	got, source, err := ResolveBaseURL(path)
	if err != nil || got != "https://file.pingcode.test" || source != authflow.SourceConfig {
		t.Fatalf("config base=%q source=%q err=%v", got, source, err)
	}
	t.Setenv("PINGCODE_API_BASE_URL", "https://env.pingcode.test/")
	got, source, err = ResolveBaseURL(path)
	if err != nil || got != "https://env.pingcode.test" || source != authflow.SourceEnvironment {
		t.Fatalf("env base=%q source=%q err=%v", got, source, err)
	}
	cfg, err := LoadConfig()
	if err != nil || cfg.APIBaseURL != "https://env.pingcode.test" {
		t.Fatalf("LoadConfig=%#v err=%v", cfg, err)
	}
}

func TestAuthStatusDoesNotLeakSecrets(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("PINGCODE_ACCESS_TOKEN", "")
	t.Setenv("PINGCODE_CLIENT_ID", "")
	t.Setenv("PINGCODE_CLIENT_SECRET", "")
	t.Setenv("PINGCODE_API_BASE_URL", "")
	secret := "super-secret-value"
	if err := SaveLogin(ConfigPath(), "https://open.pingcode.com", &AuthConfig{Mode: AuthModeClient, ClientID: "id", ClientSecret: secret}); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr strings.Builder
	if err := RunAuth([]string{"status"}, AuthIO{Stdout: &stdout, Stderr: &stderr}); err != nil {
		t.Fatal(err)
	}
	out := stdout.String()
	if strings.Contains(out, secret) || strings.Contains(out, "client_id") || strings.Contains(out, "client_secret") {
		t.Fatalf("status leaked secrets: %s", out)
	}
	if !strings.Contains(out, `"configured": true`) ||
		!strings.Contains(out, `"credentialSource": "config"`) ||
		!strings.Contains(out, `"baseUrl": "https://open.pingcode.com"`) ||
		!strings.Contains(out, `"baseUrlSource": "config"`) {
		t.Fatalf("status = %s", out)
	}
}

func TestAuthLoginLogoutClearsTokenCache(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("RSH_CONFIG_DIR", filepath.Join(dir, "aicli", "pingcode"))
	t.Setenv("PINGCODE_ACCESS_TOKEN", "")
	t.Setenv("PINGCODE_CLIENT_ID", "")
	t.Setenv("PINGCODE_CLIENT_SECRET", "")
	configureStatePaths()

	cachePath := restishauth.DefaultTokenCachePath()
	if err := os.MkdirAll(filepath.Dir(cachePath), 0o700); err != nil {
		t.Fatal(err)
	}
	cache := restishauth.NewTokenCache(cachePath)
	key := clientCredentialsCacheKey("test", DefaultAPIBaseURL, "id")
	if err := cache.Set(key, restishauth.CachedToken{AccessToken: "cached"}); err != nil {
		t.Fatal(err)
	}

	var stdout strings.Builder
	err := RunAuth([]string{"login", "--mode", "client"}, AuthIO{
		Stdin:  strings.NewReader("https://open.pingcode.com\nid\n"),
		Stdout: &stdout,
		Stderr: &stdout,
		ReadSecret: func(string) (string, error) {
			return "secret", nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	file, err := LoadFileConfig(ConfigPath())
	if err != nil || file.BaseURL != "https://open.pingcode.com" || file.Auth == nil || file.Auth.ClientID != "id" {
		t.Fatalf("login file=%#v err=%v", file, err)
	}
	got, err := cache.Get(key)
	if err != nil {
		t.Fatal(err)
	}
	if got != nil {
		t.Fatalf("expected cache clear after login, got %#v", got)
	}

	if err := cache.Set(key, restishauth.CachedToken{AccessToken: "cached-again"}); err != nil {
		t.Fatal(err)
	}
	stdout.Reset()
	if err := RunAuth([]string{"logout"}, AuthIO{Stdout: &stdout, Stderr: &stdout}); err != nil {
		t.Fatal(err)
	}
	got, err = cache.Get(key)
	if err != nil || got != nil {
		t.Fatalf("expected cache clear after logout, got %#v err=%v", got, err)
	}
	file, err = LoadFileConfig(ConfigPath())
	if err != nil || file.Auth != nil || file.BaseURL != "https://open.pingcode.com" {
		t.Fatalf("after logout = %#v err=%v", file, err)
	}
}

func TestAuthLoginRejectsSecretFlags(t *testing.T) {
	err := RunAuth([]string{"login", "--mode", "token", "--access-token", "x"}, AuthIO{})
	if err == nil || !strings.Contains(err.Error(), "not supported") {
		t.Fatalf("error = %v", err)
	}
}

func TestAuthErrorsDoNotContainSecrets(t *testing.T) {
	secret := "must-not-appear"
	err := validateAuthConfig(&AuthConfig{Mode: AuthModeClient, ClientID: "", ClientSecret: secret})
	if err == nil || strings.Contains(err.Error(), secret) {
		t.Fatalf("error = %v", err)
	}
}

func TestAuthConfigPermissionsIgnoreUmask(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix permission checks")
	}
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	mask := umask(0o077)
	defer umask(mask)
	path := ConfigPath()
	if err := SaveLogin(path, "https://open.pingcode.com", &AuthConfig{Mode: AuthModeToken, AccessToken: "tok"}); err != nil {
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
