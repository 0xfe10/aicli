package itsaplanrt

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
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
		t.Setenv("ITSAPLAN_WRITE_MODE", tc.mode)
		req, _ := http.NewRequest(tc.method, "https://api.example.test"+tc.path, nil)
		err := a.Authenticate(context.Background(), req, restishauth.AuthContext{})
		if (err == nil) != tc.allowed {
			t.Fatalf("%+v: %v", tc, err)
		}
		if tc.allowed && req.Header.Get("x-api-key") != "test-secret" {
			t.Fatal("missing key header")
		}
	}
	t.Setenv("ITSAPLAN_WRITE_MODE", "write")
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
	t.Setenv("ITSAPLAN_API_KEY", "")
	t.Setenv("ITSAPLAN_BASE_URL", "")
	t.Setenv("ITSAPLAN_SPEC_URL", "")
	manager := ContextManager()
	sel, _, err := manager.ResolveArgs([]string{"itsaplan", "--context", "kahub"})
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
	if !session.HasCredentials || cfg.SpecURL != "https://api.test/docs/json" {
		t.Fatalf("bad session %v", cfg)
	}
	info, _ := os.Stat(ConfigPathFor(sel))
	if info.Mode().Perm() != 0600 {
		t.Fatal("insecure permissions")
	}
	other, _, _ := manager.ResolveArgs([]string{"itsaplan", "--context", "other"})
	otherSession, _, err := LoadSessionWithContext(other)
	if err != nil || otherSession.HasCredentials {
		t.Fatal("context leak")
	}
	t.Setenv("ITSAPLAN_API_KEY", "environment-secret")
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
	t.Setenv("ITSAPLAN_API_KEY", "")
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

func TestFullSpecGeneratedCommands(t *testing.T) {
	body, err := os.ReadFile("testdata/openapi.json")
	if err != nil {
		t.Fatal(err)
	}
	fixed, err := fixSpec(body)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	_ = json.Unmarshal(fixed, &doc)
	paths := doc["paths"].(map[string]any)
	if _, ok := paths["/scim/v2/Users"]; ok {
		t.Fatal("SCIM retained")
	}
	if _, ok := paths["/webhooks/git/{webhookId}"]; ok {
		t.Fatal("webhook retained")
	}
	mux := http.NewServeMux()
	server := httptest.NewServer(mux)
	defer server.Close()
	mux.HandleFunc("/docs/json", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-api-key") != "" {
			t.Error("spec receives key")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
	})
	uploadHits := 0
	mux.HandleFunc("/issues/42/attachments", func(w http.ResponseWriter, r *http.Request) {
		uploadHits++
		if r.Header.Get("x-api-key") != "fake-key" {
			t.Error("missing upload auth")
		}
		f, _, err := r.FormFile("file")
		if err != nil {
			t.Error(err)
			return
		}
		defer f.Close()
		data, _ := io.ReadAll(f)
		if string(data) != "attachment-test" {
			t.Error("upload content mismatch")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	})
	hits := 0
	mux.HandleFunc("/projects", func(w http.ResponseWriter, r *http.Request) {
		hits++
		if r.Header.Get("x-api-key") != "fake-key" {
			t.Error("missing auth")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[]`))
	})
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("RSH_CACHE_DIR", t.TempDir())
	t.Setenv("ITSAPLAN_WRITE_MODE", "")
	sel := ContextManager().DefaultSelection()
	cfg := Config{BaseURL: server.URL, SpecURL: server.URL + "/docs/json"}
	session := Session{BaseURL: server.URL, HasCredentials: true, Credentials: Credentials{APIKey: "fake-key"}}
	run := func(args ...string) (string, error) {
		cli := NewCLIWithContext(cfg, session, "test", "test", sel)
		var out bytes.Buffer
		cli.Stdout = &out
		cli.Stderr = &out
		err := RunCLIWithContext(cli, append([]string{"itsaplan"}, args...), sel)
		return out.String(), err
	}
	out, err := run("--help")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "projects") {
		t.Fatal("missing projects commands")
	}
	out, err = run("projects", "--help")
	if err != nil {
		t.Fatal(err)
	}
	t.Log("Projects command group generated")
	_, err = run("projects", "get-projects", "-o", "json")
	if err != nil {
		t.Fatal(err)
	}
	if hits != 1 {
		t.Fatal("request not executed")
	}
	verbose, err := run("projects", "get-projects", "-o", "json", "-v")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(verbose, "fake-key") {
		t.Fatal("verbose leaks API key")
	}
	hits = 1
	_, err = run("projects", "post-projects", "--help")
	if err != nil {
		t.Fatal(err)
	}
	// Cached command generation and raw request must both keep the write gate.
	_, err = run("cli", "post", server.URL+"/projects", `name: test`)
	if err == nil {
		t.Fatal("readonly raw POST allowed")
	}
	if hits != 1 {
		t.Fatal("blocked write reached server")
	}
	t.Setenv("ITSAPLAN_WRITE_MODE", "write")
	file := filepath.Join(t.TempDir(), "attachment.txt")
	_ = os.WriteFile(file, []byte("attachment-test"), 0600)
	_, err = run("attachments", "post-issues-by-issue-id-attachments", "42", "file: @"+file)
	if err != nil {
		t.Fatal(err)
	}
	if uploadHits != 1 {
		t.Fatal("upload not executed")
	}

}

func TestSpecRejectExternalRefs(t *testing.T) {
	_, err := fixSpec([]byte(`{"openapi":"3.0.3","paths":{},"components":{"schemas":{"bad":{"$ref":"https://other.test/spec"}}}}`))
	if err == nil {
		t.Fatal("external ref accepted")
	}
}
func TestLiveSpec(t *testing.T) {
	url := os.Getenv("ITSAPLAN_SPEC_CHECK_URL")
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
