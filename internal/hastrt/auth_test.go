package hastrt

import (
	"bytes"
	"github.com/0xfe10/aicli/internal/authflow"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAuthConfigAndContext(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("HAST_API_KEY", "")
	t.Setenv("HAST_BASE_URL", "")
	t.Setenv("HAST_API_BASE", "")
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
	if !session.HasCredentials || cfg.BaseURL != "https://api.test" {
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
