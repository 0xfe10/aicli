package fnsrt

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPinnedSpecFile(t *testing.T) {
	path := strings.TrimSpace(os.Getenv("FNS_SPEC_CHECK_FILE"))
	if path == "" {
		t.Skip("FNS_SPEC_CHECK_FILE not set")
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(body)
	got := hex.EncodeToString(sum[:])
	if got != PinnedSpecSHA256 {
		t.Fatalf("pinned swagger SHA256 = %s, want %s (download content mismatch)", got, PinnedSpecSHA256)
	}
	if _, err := ConvertAndFix(body); err != nil {
		t.Fatal(err)
	}
	tree := captureCommandTree(t, body)
	assertApprovedCommandTree(t, tree)

	mux := http.NewServeMux()
	server := httptest.NewServer(mux)
	defer server.Close()
	mux.HandleFunc("/openapi.yaml", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(body)
	})
	stateDir := t.TempDir()
	t.Setenv("RSH_CONFIG_DIR", filepath.Join(stateDir, "config"))
	t.Setenv("RSH_CACHE_DIR", filepath.Join(stateDir, "cache"))
	t.Setenv("FNS_ACCESS_TOKEN", "token")
	cfg := Config{BaseURL: server.URL, SpecURL: server.URL + "/openapi.yaml", Client: "aicli", Version: "test"}
	for _, args := range [][]string{
		{"fns", "--help"},
		{"fns", "note", "--help"},
		{"fns", "file", "--help"},
		{"fns", "folder", "--help"},
	} {
		help := runHelp(t, cfg, args)
		if !strings.Contains(help, "Additional Commands:") && !strings.Contains(help, "Available Commands:") {
			t.Fatalf("%v missing command listing:\n%s", args, help)
		}
		for _, blocked := range []string{"\n  vault ", "\n  admin ", "\n  auth "} {
			if strings.Contains(help, blocked) {
				t.Fatalf("%v exposed blocked command %q:\n%s", args, blocked, help)
			}
		}
	}
}

func TestAuthConfigRejectsWorldReadableExplicitly(t *testing.T) {
	xdg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", xdg)
	path := ConfigPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	wide := path + ".wide"
	if err := os.WriteFile(wide, []byte("access_token = \"x\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(wide, 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := LoadFileConfig(wide)
	if err == nil || !strings.Contains(err.Error(), "0600") {
		t.Fatalf("error = %v", err)
	}
}
