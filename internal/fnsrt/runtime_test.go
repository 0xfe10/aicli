package fnsrt

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	restish "github.com/rest-sh/restish/v2"
	restishauth "github.com/rest-sh/restish/v2/auth"
)

func TestEmbeddedRestishGeneratedCommands(t *testing.T) {
	spec, err := os.ReadFile(filepath.Join("testdata", "swagger.json"))
	if err != nil {
		t.Fatal(err)
	}

	var gotAuth, gotClient, gotClientName, gotClientVersion string
	var gotJSONBody map[string]any
	var gotFileBytes []byte
	var downloadHits atomic.Int32

	mux := http.NewServeMux()
	server := httptest.NewServer(mux)
	defer server.Close()

	mux.HandleFunc("/openapi.yaml", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/x-yaml")
		_, _ = w.Write(spec)
	})
	mux.HandleFunc("/api/note", func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotClient = r.Header.Get("X-Client")
		gotClientName = r.Header.Get("X-Client-Name")
		gotClientVersion = r.Header.Get("X-Client-Version")
		switch r.Method {
		case http.MethodGet:
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"path": r.URL.Query().Get("path"), "vault": r.URL.Query().Get("vault")})
		case http.MethodPost:
			if err := json.NewDecoder(r.Body).Decode(&gotJSONBody); err != nil {
				t.Errorf("decode json: %v", err)
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
		default:
			t.Errorf("unexpected method %s", r.Method)
		}
	})
	mux.HandleFunc("/api/file", func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotClient = r.Header.Get("X-Client")
		switch r.Method {
		case http.MethodGet:
			downloadHits.Add(1)
			w.Header().Set("Content-Type", "application/octet-stream")
			_, _ = w.Write([]byte("png-bytes-123"))
		case http.MethodPost:
			mediaType, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
			if err != nil || mediaType != "multipart/form-data" {
				t.Errorf("content-type = %q err=%v", r.Header.Get("Content-Type"), err)
				return
			}
			reader := multipart.NewReader(r.Body, params["boundary"])
			for {
				part, err := reader.NextPart()
				if err == io.EOF {
					break
				}
				if err != nil {
					t.Errorf("multipart: %v", err)
					return
				}
				data, _ := io.ReadAll(part)
				if part.FormName() == "file" {
					gotFileBytes = data
				}
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"uploaded": true})
		default:
			t.Errorf("unexpected method %s", r.Method)
		}
	})

	stateDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", stateDir)
	t.Setenv("RSH_CACHE_DIR", stateDir+"/cache")
	t.Setenv("FNS_ACCESS_TOKEN", "test-token")
	t.Setenv("FNS_CLIENT", "aicli")
	t.Setenv("FNS_WRITE_MODE", "write")

	cfg := Config{BaseURL: server.URL, SpecURL: server.URL + "/openapi.yaml", Client: "aicli", Version: "test"}

	getOut := runTestCLI(t, cfg, "", []string{
		"fns", "note", "get-api-note", "Notes/test.md", "genesis", "-o", "json",
	})
	if !strings.Contains(getOut, `"path": "Notes/test.md"`) || !strings.Contains(getOut, `"vault": "genesis"`) {
		t.Fatalf("GET note output = %s", getOut)
	}
	if gotAuth != "Bearer test-token" {
		t.Fatalf("Authorization = %q", gotAuth)
	}
	if gotClient != "aicli" || gotClientName != "aicli" || gotClientVersion != "test" {
		t.Fatalf("client headers client=%q name=%q version=%q", gotClient, gotClientName, gotClientVersion)
	}

	postOut := runTestCLI(t, cfg, `{"vault":"genesis","path":"Notes/new.md","content":"hello"}`, []string{
		"fns", "note", "post-api-note", "-o", "json",
	})
	if !strings.Contains(postOut, `"ok": true`) {
		t.Fatalf("POST note output = %s", postOut)
	}
	if fmt.Sprint(gotJSONBody["content"]) != "hello" {
		t.Fatalf("json body = %#v", gotJSONBody)
	}

	uploadFile := filepath.Join(t.TempDir(), "upload.bin")
	if err := os.WriteFile(uploadFile, []byte("file-payload"), 0o600); err != nil {
		t.Fatal(err)
	}
	uploadOut := runTestCLI(t, cfg, "", []string{
		"fns", "file", "post-api-file",
		"vault:", "genesis,",
		"path:", "assets/test.bin,",
		"file:", "@" + uploadFile,
		"-o", "json",
	})
	if !strings.Contains(uploadOut, `"uploaded": true`) {
		t.Fatalf("upload output = %s", uploadOut)
	}
	if string(gotFileBytes) != "file-payload" {
		t.Fatalf("uploaded bytes = %q", gotFileBytes)
	}

	downloadOut := runTestCLI(t, cfg, "", []string{
		"fns", "file", "get-api-file", "assets/test.bin", "genesis",
	})
	sum := sha256.Sum256([]byte(downloadOut))
	wantSum := sha256.Sum256([]byte("png-bytes-123"))
	if hex.EncodeToString(sum[:]) != hex.EncodeToString(wantSum[:]) {
		t.Fatalf("download mismatch %q", downloadOut)
	}
	if downloadHits.Load() != 1 {
		t.Fatalf("download hits = %d", downloadHits.Load())
	}
}

func TestEmbeddedRestishBlocksWritesByDefault(t *testing.T) {
	spec, err := os.ReadFile(filepath.Join("testdata", "swagger.json"))
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	server := httptest.NewServer(mux)
	defer server.Close()
	mux.HandleFunc("/openapi.yaml", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(spec)
	})
	mux.HandleFunc("/api/note", func(http.ResponseWriter, *http.Request) {
		t.Error("write reached API while readonly")
	})

	stateDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", stateDir)
	t.Setenv("RSH_CACHE_DIR", stateDir+"/cache")
	t.Setenv("FNS_ACCESS_TOKEN", "token")
	t.Setenv("FNS_WRITE_MODE", "")

	cli := newCLIForTest(Config{BaseURL: server.URL, SpecURL: server.URL + "/openapi.yaml", Client: "aicli", Version: "test"}, "test", "")
	cli.Stdin = strings.NewReader(`{"vault":"genesis","path":"a.md","content":"x"}`)
	var stdout, stderr bytes.Buffer
	cli.Stdout, cli.Stderr = &stdout, &stderr
	err = RunCLI(cli, []string{"fns", "note", "post-api-note", "-o", "json"})
	if err == nil || !strings.Contains(err.Error(), "blocked by FNS_WRITE_MODE=readonly") {
		t.Fatalf("error = %v stdout=%s stderr=%s", err, stdout.String(), stderr.String())
	}
}

func TestEmbeddedRestishDoesNotRetryWriteAfterUnauthorized(t *testing.T) {
	spec, err := os.ReadFile(filepath.Join("testdata", "swagger.json"))
	if err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	mux := http.NewServeMux()
	server := httptest.NewServer(mux)
	defer server.Close()
	mux.HandleFunc("/openapi.yaml", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(spec)
	})
	mux.HandleFunc("/api/note", func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusUnauthorized)
	})

	stateDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", stateDir)
	t.Setenv("RSH_CACHE_DIR", stateDir+"/cache")
	t.Setenv("FNS_ACCESS_TOKEN", "token")
	t.Setenv("FNS_WRITE_MODE", "write")

	cli := newCLIForTest(Config{BaseURL: server.URL, SpecURL: server.URL + "/openapi.yaml", Client: "aicli", Version: "test"}, "test", "")
	cli.Stdin = strings.NewReader(`{"vault":"genesis","path":"a.md","content":"x"}`)
	var stdout, stderr bytes.Buffer
	cli.Stdout, cli.Stderr = &stdout, &stderr
	err = RunCLI(cli, []string{"fns", "note", "post-api-note", "-o", "json"})
	if err == nil || !strings.Contains(err.Error(), "automatic retry is disabled for writes") {
		t.Fatalf("error = %v stdout=%s stderr=%s", err, stdout.String(), stderr.String())
	}
	if calls.Load() != 1 {
		t.Fatalf("calls = %d", calls.Load())
	}
}

func TestEmbeddedRestishBlocksPlaceholderBaseURL(t *testing.T) {
	spec, err := os.ReadFile(filepath.Join("testdata", "swagger.json"))
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	server := httptest.NewServer(mux)
	defer server.Close()
	mux.HandleFunc("/openapi.yaml", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(spec)
	})
	mux.HandleFunc("/", func(http.ResponseWriter, *http.Request) {
		t.Error("placeholder Base URL must not receive API traffic")
	})

	stateDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", stateDir)
	t.Setenv("RSH_CACHE_DIR", stateDir+"/cache")
	t.Setenv("FNS_ACCESS_TOKEN", "token")
	t.Setenv("FNS_WRITE_MODE", "readonly")

	cli := newCLIForTest(Config{BaseURL: DefaultBaseURL, SpecURL: server.URL + "/openapi.yaml", Client: "aicli", Version: "test"}, "test", "")
	var stdout, stderr bytes.Buffer
	cli.Stdout, cli.Stderr = &stdout, &stderr
	err = RunCLI(cli, []string{"fns", "note", "get-api-note", "Notes/test.md", "genesis", "-o", "json"})
	if err == nil || !strings.Contains(err.Error(), "FNS Base URL is not configured") {
		t.Fatalf("error = %v stdout=%s stderr=%s", err, stdout.String(), stderr.String())
	}
}

func TestEmbeddedRestishIgnoresConflictingRestishJSON(t *testing.T) {
	spec, err := os.ReadFile(filepath.Join("testdata", "swagger.json"))
	if err != nil {
		t.Fatal(err)
	}
	var hits atomic.Int32
	mux := http.NewServeMux()
	good := httptest.NewServer(mux)
	defer good.Close()
	mux.HandleFunc("/openapi.yaml", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(spec)
	})
	mux.HandleFunc("/api/note", func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if got := r.Header.Get("Authorization"); got != "Bearer snap-token" {
			t.Errorf("Authorization = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
	})

	evil := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Error("restish.json Base URL must not receive traffic")
	}))
	defer evil.Close()

	xdg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", xdg)
	t.Setenv("RSH_CACHE_DIR", filepath.Join(xdg, "cache"))
	t.Setenv("FNS_ACCESS_TOKEN", "")
	t.Setenv("FNS_WRITE_MODE", "readonly")
	if err := SaveLogin(ConfigPath(), good.URL, &AuthConfig{Mode: AuthModeToken, AccessToken: "snap-token"}); err != nil {
		t.Fatal(err)
	}
	// Poison both the old co-located path and an explicit RSH_CONFIG file.
	poison := []byte(`{"apis":{"fns":{"base_url":"` + evil.URL + `"}}}`)
	if err := os.MkdirAll(filepath.Join(xdg, "aicli", "fns"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(xdg, "aicli", "fns", "restish.json"), poison, 0o600); err != nil {
		t.Fatal(err)
	}
	userPath := filepath.Join(xdg, "evil-restish.json")
	if err := os.WriteFile(userPath, poison, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("RSH_CONFIG", userPath)

	session, cfg, err := LoadSession("test")
	if err != nil {
		t.Fatal(err)
	}
	cfg.SpecURL = good.URL + "/openapi.yaml"
	cli := NewCLIWithSession(cfg, session, "test", "")
	var stdout, stderr bytes.Buffer
	cli.Stdout, cli.Stderr = &stdout, &stderr
	err = RunCLI(cli, []string{"fns", "--rsh-config", userPath, "note", "get-api-note", "Notes/test.md", "genesis", "-o", "json"})
	if err != nil {
		t.Fatalf("error = %v stderr=%s", err, stderr.String())
	}
	if hits.Load() != 1 {
		t.Fatalf("good server hits = %d", hits.Load())
	}
	if !strings.Contains(stderr.String(), "--rsh-config is ignored") {
		t.Fatalf("expected --rsh-config warning, stderr=%s", stderr.String())
	}
}

func TestSessionSnapshotIgnoresLaterConfigRewrite(t *testing.T) {
	xdg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", xdg)
	t.Setenv("FNS_ACCESS_TOKEN", "")
	t.Setenv("FNS_BASE_URL", "")
	if err := SaveLogin(ConfigPath(), "https://tenant-a.example.com", &AuthConfig{Mode: AuthModeToken, AccessToken: "token-a"}); err != nil {
		t.Fatal(err)
	}
	session, _, err := LoadSession("test")
	if err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		<-start
		if err := SaveLogin(ConfigPath(), "https://tenant-b.example.com", &AuthConfig{Mode: AuthModeToken, AccessToken: "token-b"}); err != nil {
			t.Errorf("rewrite: %v", err)
		}
	}()
	close(start)
	req, _ := http.NewRequest(http.MethodGet, "https://tenant-a.example.com/api/note", nil)
	t.Setenv("FNS_WRITE_MODE", "readonly")
	if err := (&BearerAuth{Session: session}).Authenticate(context.Background(), req, restishauth.AuthContext{BaseURL: session.BaseURL}); err != nil {
		t.Fatal(err)
	}
	<-done
	if got := req.Header.Get("Authorization"); got != "Bearer token-a" {
		t.Fatalf("Authorization = %q, want snapshotted token-a", got)
	}
	evil, _ := http.NewRequest(http.MethodGet, "https://tenant-b.example.com/api/note", nil)
	err = (&BearerAuth{Session: session}).Authenticate(context.Background(), evil, restishauth.AuthContext{BaseURL: session.BaseURL})
	if err == nil || !strings.Contains(err.Error(), "refusing to attach FNS credentials") {
		t.Fatalf("cross-host error = %v", err)
	}
	if evil.Header.Get("Authorization") != "" {
		t.Fatal("credentials attached to wrong host")
	}
}

func TestEnvironmentOnlySessionWithoutHome(t *testing.T) {
	t.Setenv("HOME", "")
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("FNS_BASE_URL", "https://fns.example.test")
	t.Setenv("FNS_ACCESS_TOKEN", "env-token")
	session, cfg, err := LoadSession("test")
	if err != nil {
		t.Fatal(err)
	}
	if !session.HasCredentials || session.Credentials.AccessToken != "env-token" || cfg.BaseURL != "https://fns.example.test" {
		t.Fatalf("session=%#v cfg=%#v", session, cfg)
	}
	cli := NewCLIWithSession(cfg, session, "test", "")
	var stdout, stderr bytes.Buffer
	cli.Stdout, cli.Stderr = &stdout, &stderr
	if err := RunCLI(cli, []string{"fns", "--version"}); err != nil {
		t.Fatalf("version failed without home: %v stderr=%s", err, stderr.String())
	}
}

func TestAuthenticatedResponsesAreNotReusedAcrossTokens(t *testing.T) {
	spec, err := os.ReadFile(filepath.Join("testdata", "swagger.json"))
	if err != nil {
		t.Fatal(err)
	}
	var hits atomic.Int32
	mux := http.NewServeMux()
	server := httptest.NewServer(mux)
	defer server.Close()
	mux.HandleFunc("/openapi.yaml", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(spec) })
	mux.HandleFunc("/api/note", func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Header().Set("Cache-Control", "max-age=3600")
		_ = json.NewEncoder(w).Encode(map[string]string{"authorization": r.Header.Get("Authorization")})
	})

	state := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", state)
	t.Setenv("RSH_CACHE_DIR", filepath.Join(state, "cache"))
	cfg := Config{BaseURL: server.URL, SpecURL: server.URL + "/openapi.yaml", Client: "aicli", Version: "test"}
	for _, token := range []string{"token-a", "token-b"} {
		session := Session{BaseURL: server.URL, HasCredentials: true, Credentials: Credentials{AccessToken: token}}
		cli := NewCLIWithSession(cfg, session, "test", "")
		var stdout, stderr bytes.Buffer
		cli.Stdout, cli.Stderr = &stdout, &stderr
		if err := RunCLI(cli, []string{"fns", "note", "get-api-note", "Notes/test.md", "genesis", "-o", "json"}); err != nil {
			t.Fatalf("token %s: %v stderr=%s", token, err, stderr.String())
		}
		if !strings.Contains(stdout.String(), "Bearer "+token) {
			t.Fatalf("token %s output = %s", token, stdout.String())
		}
	}
	if got := hits.Load(); got != 2 {
		t.Fatalf("API hits = %d, want 2 uncached requests", got)
	}
}

func runTestCLI(t *testing.T, cfg Config, stdin string, args []string) string {
	t.Helper()
	if os.Getenv("XDG_CONFIG_HOME") == "" {
		t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	}
	cli := newCLIForTest(cfg, cfg.Version, "")
	cli.Stdin = strings.NewReader(stdin)
	var stdout, stderr bytes.Buffer
	cli.Stdout, cli.Stderr = &stdout, &stderr
	if err := RunCLI(cli, args); err != nil {
		t.Fatalf("Run(%v): %v\nstderr: %s", args, err, stderr.String())
	}
	return stdout.String()
}

func newCLIForTest(cfg Config, version, commit string) *restish.CLI {
	session := Session{BaseURL: cfg.BaseURL}
	if creds, err := ResolveCredentials(); err == nil {
		session.Credentials = creds
		session.HasCredentials = true
		session.CredentialSource = creds.Source
	}
	return NewCLIWithSession(cfg, session, version, commit)
}
