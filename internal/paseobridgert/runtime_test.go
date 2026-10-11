package paseobridgert

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/0xfe10/aicli/internal/authflow"
	restish "github.com/rest-sh/restish/v2"
	restishauth "github.com/rest-sh/restish/v2/auth"
)

// Exercise the complete upstream 3.1 contract through real HTTP and the generated
// command surface, including caches, retry overrides, redirects, and SSE output.
func TestGeneratedBridgeCommands(t *testing.T) {
	body, err := os.ReadFile("testdata/openapi.json")
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	server := httptest.NewServer(mux)
	defer server.Close()
	mux.HandleFunc("/api/v1/openapi.json", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			t.Error("discovery received credentials")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
	})
	var hits, sendHits, redirectHits atomic.Int32
	var responseStatus atomic.Int32
	responseStatus.Store(http.StatusAccepted)
	mux.HandleFunc("/api/v1/hosts", func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if r.Header.Get("Authorization") != "Bearer test-secret" {
			t.Error("missing bearer auth")
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"ok":true,"data":[]}`)
	})
	mux.HandleFunc("/api/v1/hosts/test/agents/agent/messages", func(w http.ResponseWriter, r *http.Request) {
		sendHits.Add(1)
		status := int(responseStatus.Load())
		if r.Method != "POST" || r.Header.Get("Authorization") != "Bearer test-secret" {
			t.Error("wrong method or token")
		}
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Error(err)
		}
		if payload["prompt"] != "test prompt" || payload["interrupt"] == true {
			t.Error("incorrect send body")
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Retry-After", "0")
		if status == 307 || status == 308 {
			w.Header().Set("Location", server.URL+"/redirect")
		}
		if status == 0 {
			conn, _, err := w.(http.Hijacker).Hijack()
			if err != nil {
				t.Error(err)
				return
			}
			_ = conn.Close()
			return
		}
		w.WriteHeader(status)
		if status == http.StatusAccepted {
			fmt.Fprint(w, `{"ok":true,"data":{"status":"accepted"}}`)
		} else if status == http.StatusOK {
			fmt.Fprint(w, `{"ok":true,"data":{"status":"unknown","retrySafe":false}}`)
		} else if status == http.StatusCreated {
			fmt.Fprint(w, `invalid-json`)
		} else {
			fmt.Fprint(w, `{"ok":false,"error":{"code":"test","message":"test failure"}}`)
		}
	})
	mux.HandleFunc("/redirect", func(w http.ResponseWriter, r *http.Request) { redirectHits.Add(1); w.WriteHeader(202) })
	mux.HandleFunc("/api/v1/hosts/test/agents/agent/activity", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("tail") != "0" {
			t.Error("tail=0 not preserved")
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"ok":true,"data":{"entries":[],"gap":true,"epoch":1}}`)
	})
	mux.HandleFunc("/api/v1/hosts/test/agents/agent/events", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-secret" || r.URL.Query().Get("include") != "activity" {
			t.Error("incorrect observation request")
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "event: snapshot\ndata: {\"kind\":\"snapshot\",\"status\":\"idle\"}\n\nevent: error\ndata: {\"kind\":\"error\",\"code\":\"offline\"}\n\n")
	})
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("RSH_CACHE_DIR", t.TempDir())
	t.Setenv("PASEO_BRIDGE_WRITE_MODE", "")
	selection := ContextManager().DefaultSelection()
	cfg := Config{BaseURL: server.URL, SpecURL: server.URL + "/api/v1/openapi.json"}
	session := Session{BaseURL: server.URL, HasCredentials: true, Credentials: Credentials{AccessToken: "test-secret"}}
	run := func(args ...string) (string, error) {
		cli := NewCLIWithContext(cfg, session, "test", "test", selection)
		var output bytes.Buffer
		cli.Stdout, cli.Stderr = &output, &output
		err := RunCLIWithContext(cli, append([]string{"paseo-bridge"}, args...), selection)
		return output.String(), err
	}
	for _, command := range [][]string{
		{"status", "list-host-status"}, {"status", "get-host-status"},
		{"workspaces", "list-workspaces"}, {"agents", "list-agents"},
		{"wait", "get-agent-snapshot"}, {"send", "send-message"},
		{"activity", "get-agent-activity"}, {"observation", "observe-agent"},
	} {
		output, err := run(append(command, "--help")...)
		if err != nil || !strings.Contains(output, command[1]) {
			t.Fatalf("%v: %v, %s", command, err, output)
		}
	}
	for i := 0; i < 2; i++ {
		output, err := run("status", "list-host-status", "-o", "json", "-v")
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(output, "test-secret") {
			t.Fatal("verbose token leak")
		}
	}
	if hits.Load() != 2 {
		t.Fatalf("responses cached: hits=%d", hits.Load())
	}
	for _, args := range [][]string{
		{"send", "send-message", "test", "agent", "prompt: test prompt"},
	} {
		if _, err := run(args...); err == nil {
			t.Fatal("readonly write allowed")
		}
	}
	if sendHits.Load() != 0 {
		t.Fatal("blocked writes reached server")
	}
	t.Setenv("PASEO_BRIDGE_WRITE_MODE", "write")
	t.Setenv("RSH_RETRY_UNSAFE", "1")
	t.Setenv("RSH_RETRY", "3")
	for _, code := range []int{200, 202, 401, 429, 503, 504, 307, 308, 0} {
		responseStatus.Store(int32(code))
		before := sendHits.Load()
		output, err := run("send", "send-message", "test", "agent", "prompt: test prompt", "--rsh-retry-unsafe", "--rsh-retry", "3", "-o", "json")
		if (code == 200 || code == 202) && err != nil {
			t.Fatal(err)
		}
		if code == 200 && (!strings.Contains(output, "unknown") || !strings.Contains(output, "retrySafe")) {
			t.Fatal("uncertain send result lost")
		}
		if code != 200 && code != 202 && err == nil {
			t.Fatalf("status %d should fail", code)
		}
		if sendHits.Load() != before+1 {
			t.Fatalf("send %d replayed: %d requests", code, sendHits.Load()-before)
		}
	}
	responseStatus.Store(http.StatusCreated)
	before := sendHits.Load()
	_, _ = run("send", "send-message", "test", "agent", "prompt: test prompt", "--rsh-retry-unsafe=true", "--rsh-retry", "3")
	if sendHits.Load() != before+1 {
		t.Fatal("invalid send response replayed")
	}
	if redirectHits.Load() != 0 {
		t.Fatal("send replayed through redirect")
	}
	output, err := run("activity", "get-agent-activity", "test", "agent", "--tail", "0", "-o", "json")
	if err != nil || !strings.Contains(output, "gap") {
		t.Fatalf("activity: %v, %s", err, output)
	}
	output, err = run("observation", "observe-agent", "test", "agent", "--include", "activity", "-o", "ndjson")
	if err != nil || !strings.Contains(output, "snapshot") || !strings.Contains(output, "offline") {
		t.Fatalf("SSE: %v, %s", err, output)
	}
}

func TestSpecFiltersAssetsAndServers(t *testing.T) {
	body, err := os.ReadFile("testdata/openapi.json")
	if err != nil {
		t.Fatal(err)
	}
	fixed, err := fixSpec(body)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(fixed, &doc); err != nil {
		t.Fatal(err)
	}
	paths := doc["paths"].(map[string]any)
	if len(paths) != 8 {
		t.Fatalf("expected 8 business paths, got %d", len(paths))
	}
	if _, exists := doc["servers"]; exists {
		t.Fatal("upstream servers retained")
	}
}

func TestAuthSafetyAndOrigin(t *testing.T) {
	a := &HeaderAuth{Session: Session{BaseURL: "https://api.example.test", HasCredentials: true, Credentials: Credentials{AccessToken: "test-secret"}}}
	for _, tc := range []struct {
		method, path, mode string
		allowed            bool
	}{
		{"GET", "/projects", "", true}, {"POST", "/projects", "", false}, {"POST", "/projects", "write", true},
		{"POST", "/projects/foo/issues/bulk/delete", "write", false}, {"POST", "/projects/foo/issues/bulk/delete", "destructive", true},
		{"DELETE", "/projects/foo", "write", false}, {"DELETE", "/projects/foo", "destructive", true}, {"GET", "/projects", "invalid", false},
	} {
		t.Setenv("PASEO_BRIDGE_WRITE_MODE", tc.mode)
		req, _ := http.NewRequest(tc.method, "https://api.example.test"+tc.path, nil)
		err := a.Authenticate(context.Background(), req, restishauth.AuthContext{})
		if (err == nil) != tc.allowed {
			t.Fatalf("%+v: %v", tc, err)
		}
		if tc.allowed && req.Header.Get("Authorization") != "Bearer test-secret" {
			t.Fatal("missing bearer header")
		}
	}
	t.Setenv("PASEO_BRIDGE_WRITE_MODE", "write")
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
	t.Setenv("PASEO_BRIDGE_ACCESS_TOKEN", "")
	t.Setenv("PASEO_BRIDGE_BASE_URL", "")
	t.Setenv("PASEO_BRIDGE_SPEC_URL", "")
	manager := ContextManager()
	sel, _, err := manager.ResolveArgs([]string{"paseo-bridge", "--context", "kahub"})
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	err = RunAuthWithContext([]string{"login", "--mode", "token"}, authflow.IO{Stdin: strings.NewReader("https://api.test\n"), Stdout: &output, Stderr: &output, ReadSecret: func(string) (string, error) { return "secret-for-test", nil }}, sel)
	if err != nil {
		t.Fatal(err)
	}
	session, cfg, err := LoadSessionWithContext(sel)
	if err != nil {
		t.Fatal(err)
	}
	if !session.HasCredentials || cfg.SpecURL != "https://api.test/api/v1/openapi.json" {
		t.Fatalf("bad session %v", cfg)
	}
	info, _ := os.Stat(ConfigPathFor(sel))
	if info.Mode().Perm() != 0600 {
		t.Fatal("insecure permissions")
	}
	other, _, _ := manager.ResolveArgs([]string{"paseo-bridge", "--context", "other"})
	otherSession, _, err := LoadSessionWithContext(other)
	if err != nil || otherSession.HasCredentials {
		t.Fatal("context leak")
	}
	t.Setenv("PASEO_BRIDGE_ACCESS_TOKEN", "environment-secret")
	session, _, _ = LoadSessionWithContext(sel)
	if session.Credentials.AccessToken != "environment-secret" {
		t.Fatal("env override failed")
	}
	output.Reset()
	if err := RunAuthWithContext([]string{"status"}, authflow.IO{Stdout: &output}, sel); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(output.String(), "secret") {
		t.Fatal("status leaks secret")
	}
	t.Setenv("PASEO_BRIDGE_ACCESS_TOKEN", "")
	if err := RunAuthWithContext([]string{"logout"}, authflow.IO{Stdout: &output}, sel); err != nil {
		t.Fatal(err)
	}
	session, _, _ = LoadSessionWithContext(sel)
	if session.HasCredentials || session.BaseURL != "https://api.test" {
		t.Fatal("logout contract failed")
	}
	if err := SaveLogin(ConfigPathFor(sel), "https://api.test/path", &AuthConfig{Mode: "token", AccessToken: "test"}); err == nil {
		t.Fatal("subpath accepted")
	}
}

func TestSpecRejectExternalRefs(t *testing.T) {
	_, err := fixSpec([]byte(`{"openapi":"3.0.3","paths":{},"components":{"schemas":{"bad":{"$ref":"https://other.test/spec"}}}}`))
	if err == nil {
		t.Fatal("external ref accepted")
	}
}
func TestLiveSpec(t *testing.T) {
	url := os.Getenv("PASEO_BRIDGE_SPEC_CHECK_URL")
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
