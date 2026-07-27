package fnsrt

import (
	"os"
	"testing"
)

func TestLoadConfigDefaultsAndOverrides(t *testing.T) {
	xdg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", xdg)
	t.Setenv("FNS_BASE_URL", "")
	t.Setenv("FNS_SPEC_URL", "")
	t.Setenv("FNS_CLIENT", "")
	t.Setenv("FNS_ACCESS_TOKEN", "")

	cfg, err := LoadConfig("1.2.3")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.BaseURL != DefaultBaseURL {
		t.Fatalf("BaseURL = %q", cfg.BaseURL)
	}
	if cfg.SpecURL != DefaultSpecURL {
		t.Fatalf("SpecURL = %q", cfg.SpecURL)
	}
	if cfg.Client != DefaultClient {
		t.Fatalf("Client = %q", cfg.Client)
	}
	if DefaultSpecURL != "https://raw.githubusercontent.com/haierkeys/fast-note-sync-service/3.5.1/docs/swagger.yaml" {
		t.Fatalf("DefaultSpecURL is not pinned: %s", DefaultSpecURL)
	}

	if err := SaveAccessToken(ConfigPath(), "file-token"); err != nil {
		t.Fatal(err)
	}
	// Preserve custom base/client by rewriting file.
	if err := os.WriteFile(ConfigPath(), []byte("base_url = \"https://fns.example.test\"\nclient = \"file-client\"\naccess_token = \"file-token\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(ConfigPath(), 0o600); err != nil {
		t.Fatal(err)
	}

	t.Setenv("FNS_BASE_URL", "")
	t.Setenv("FNS_SPEC_URL", "")
	t.Setenv("FNS_CLIENT", "")
	cfg, err = LoadConfig("1.2.3")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.BaseURL != "https://fns.example.test" || cfg.Client != "file-client" {
		t.Fatalf("file config not applied: %#v", cfg)
	}
	if cfg.SpecURL != DefaultSpecURL {
		t.Fatalf("SpecURL should remain default without env: %q", cfg.SpecURL)
	}

	t.Setenv("FNS_BASE_URL", "https://env.example.test")
	t.Setenv("FNS_SPEC_URL", "https://env.example.test/spec.yaml")
	t.Setenv("FNS_CLIENT", "env-client")
	cfg, err = LoadConfig("1.2.3")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.BaseURL != "https://env.example.test" || cfg.SpecURL != "https://env.example.test/spec.yaml" || cfg.Client != "env-client" {
		t.Fatalf("env override failed: %#v", cfg)
	}

	t.Setenv("FNS_ACCESS_TOKEN", "")
	creds, err := ResolveCredentials()
	if err != nil || creds.AccessToken != "file-token" || creds.Source != CredentialSourceConfig {
		t.Fatalf("file token creds=%#v err=%v", creds, err)
	}
	t.Setenv("FNS_ACCESS_TOKEN", "env-token")
	creds, err = ResolveCredentials()
	if err != nil || creds.AccessToken != "env-token" || creds.Source != CredentialSourceEnvironment {
		t.Fatalf("env token creds=%#v err=%v", creds, err)
	}
}
