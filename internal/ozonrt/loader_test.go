package ozonrt

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/fxamacker/cbor/v2"
	restish "github.com/rest-sh/restish/v2"
)

func fixture(t *testing.T) []byte {
	t.Helper()
	body, err := os.ReadFile(filepath.Join("testdata", "openapi.json"))
	if err != nil {
		t.Fatal(err)
	}
	return body
}

func TestFixSpecKeepsAllOperationsAndOwnsAuth(t *testing.T) {
	fixed, routes, err := fixSpec(fixture(t))
	if err != nil {
		t.Fatal(err)
	}
	if len(routes) != 3 {
		t.Fatalf("routes = %d, want 3", len(routes))
	}
	var doc map[string]any
	if err := json.Unmarshal(fixed, &doc); err != nil {
		t.Fatal(err)
	}
	if _, ok := doc["servers"]; ok {
		t.Fatal("upstream servers must not override configured Base URL")
	}
	paths := doc["paths"].(map[string]any)
	list := paths["/v1/product/list"].(map[string]any)["post"].(map[string]any)
	if params, ok := list["parameters"].([]any); !ok || len(params) != 0 {
		t.Fatalf("auth parameters were not stripped: %#v", list["parameters"])
	}
	archive := paths["/v1/product/archive"].(map[string]any)["post"].(map[string]any)
	ref := archive["responses"].(map[string]any)["400"].(map[string]any)["content"].(map[string]any)["application/json"].(map[string]any)["schema"].(map[string]any)["$ref"]
	if ref != "#/components/schemas/rpcStatus" {
		t.Fatalf("broken ref = %v", ref)
	}
}

func TestSafetyPolicyUsesOperationSemanticsForPOST(t *testing.T) {
	_, routes, err := fixSpec(fixture(t))
	if err != nil {
		t.Fatal(err)
	}
	policy := &SafetyPolicy{}
	policy.Replace(routes)
	checks := []struct {
		path, mode string
		wantError  bool
	}{
		{"/v1/product/list", "", false},
		{"/v3/product/import", "", true},
		{"/v3/product/import", "write", false},
		{"/v1/product/archive", "write", true},
		{"/v1/product/archive", "destructive", false},
		{"/unknown", "destructive", true},
	}
	for _, check := range checks {
		err := policy.Allow(http.MethodPost, check.path, check.mode)
		if (err != nil) != check.wantError {
			t.Errorf("Allow(%s, %q) error = %v", check.path, check.mode, err)
		}
	}
}

func TestSafetyPolicyPrimesFromLegacyRawSpecCache(t *testing.T) {
	cacheDir := t.TempDir()
	configPath := filepath.Join(t.TempDir(), "restish.json")
	cachePath := specCachePath(cacheDir, configPath, "ozon")
	data, err := cbor.Marshal(struct {
		Raw []byte `cbor:"raw"`
	}{Raw: fixture(t)})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(cachePath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cachePath, data, 0o600); err != nil {
		t.Fatal(err)
	}
	policy := &SafetyPolicy{}
	policy.PrimeFromSpecCache(cacheDir, configPath, "ozon")
	if err := policy.Allow(http.MethodPost, "/v1/product/list", ""); err != nil {
		t.Fatal(err)
	}
}

func TestSafetyPolicyRejectsSymlinkedSpecCache(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink permissions vary on Windows")
	}
	for _, symlinkFile := range []bool{false, true} {
		name := "directory"
		if symlinkFile {
			name = "file"
		}
		t.Run(name, func(t *testing.T) {
			cacheDir := t.TempDir()
			configPath := filepath.Join(t.TempDir(), "restish.json")
			cachePath := specCachePath(cacheDir, configPath, "ozon")
			if symlinkFile {
				if err := os.MkdirAll(filepath.Dir(cachePath), 0o700); err != nil {
					t.Fatal(err)
				}
				target := filepath.Join(t.TempDir(), "ozon.cbor")
				if err := os.WriteFile(target, []byte("untrusted"), 0o600); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(target, cachePath); err != nil {
					t.Fatal(err)
				}
			} else {
				configsDir := filepath.Join(cacheDir, "specs", "configs")
				if err := os.MkdirAll(filepath.Dir(configsDir), 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(t.TempDir(), configsDir); err != nil {
					t.Fatal(err)
				}
			}
			policy := &SafetyPolicy{}
			policy.PrimeFromSpecCache(cacheDir, configPath, "ozon")
			if policy.Ready() {
				t.Fatal("policy loaded from symlinked cache")
			}
		})
	}
}

func TestSafetyPolicyRejectsWritableSpecCache(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permissions are unavailable")
	}
	cacheDir := t.TempDir()
	configPath := filepath.Join(t.TempDir(), "restish.json")
	cachePath := specCachePath(cacheDir, configPath, "ozon")
	if err := os.MkdirAll(filepath.Dir(cachePath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cachePath, []byte("untrusted"), 0o666); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(cachePath, 0o666); err != nil {
		t.Fatal(err)
	}
	policy := &SafetyPolicy{}
	policy.PrimeFromSpecCache(cacheDir, configPath, "ozon")
	if policy.Ready() {
		t.Fatal("policy loaded from group- or other-writable cache")
	}
}

func TestRunCLIRejectsSymlinkedPrimarySpecCache(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink permissions vary on Windows")
	}
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	manager := ContextManager()
	selection, _, err := manager.ResolveArgs([]string{"ozon", "--context", "seller-a"})
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.Prepare(selection); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(t.TempDir(), filepath.Join(selection.CacheDir, "specs")); err != nil {
		t.Fatal(err)
	}
	t.Setenv("RSH_CACHE_DIR", selection.CacheDir)
	err = RunCLIWithContext(restish.New(), []string{"ozon", "--help"}, selection)
	if err == nil || !strings.Contains(err.Error(), "validate Ozon spec cache") {
		t.Fatalf("RunCLIWithContext error = %v", err)
	}
}

func TestSafetyOverridesAmbiguousPOSTRoutes(t *testing.T) {
	checks := map[string]string{
		"/v2/chat/read":                      "write",
		"/v1/posting/fbo/cancel-reason/list": "read",
		"/v1/order/cancel/status":            "read",
	}
	for path, want := range checks {
		if got := classifySafety(http.MethodPost, path, ""); got != want {
			t.Errorf("classifySafety(%q) = %q, want %q", path, got, want)
		}
	}
}

func TestGeneratedReadCommandInjectsHeaders(t *testing.T) {
	var gotClientID, gotAPIKey string
	mux := http.NewServeMux()
	server := httptest.NewServer(mux)
	defer server.Close()
	mux.HandleFunc("/openapi.json", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(fixture(t)) })
	mux.HandleFunc("/v1/product/list", func(w http.ResponseWriter, r *http.Request) {
		gotClientID, gotAPIKey = r.Header.Get("Client-Id"), r.Header.Get("Api-Key")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	})
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("RSH_CACHE_DIR", filepath.Join(t.TempDir(), "cache"))
	session := Session{BaseURL: server.URL, HasCredentials: true, Credentials: Credentials{ClientID: "client", APIKey: "secret"}}
	cli := NewCLIWithSession(Config{BaseURL: server.URL, SpecURL: server.URL + "/openapi.json"}, session, "test", "")
	cli.Stdin = strings.NewReader(`{}`)
	var stdout, stderr bytes.Buffer
	cli.Stdout, cli.Stderr = &stdout, &stderr
	if err := RunCLI(cli, []string{"ozon", "product-api", "get-product-list", "-o", "json"}); err != nil {
		t.Fatalf("run: %v\nstdout=%s\nstderr=%s", err, stdout.String(), stderr.String())
	}
	if gotClientID != "client" || gotAPIKey != "secret" {
		t.Fatalf("headers Client-Id=%q Api-Key=%q", gotClientID, gotAPIKey)
	}
	if !strings.Contains(stdout.String(), `"ok": true`) {
		t.Fatalf("stdout = %s", stdout.String())
	}
}

func TestGeneratedReadCommandUsesSafetyPolicyWithOperationCache(t *testing.T) {
	var specHits, apiHits int
	mux := http.NewServeMux()
	server := httptest.NewServer(mux)
	defer server.Close()
	mux.HandleFunc("/openapi.json", func(w http.ResponseWriter, _ *http.Request) {
		specHits++
		_, _ = w.Write(fixture(t))
	})
	mux.HandleFunc("/v1/product/list", func(w http.ResponseWriter, _ *http.Request) {
		apiHits++
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	})
	configRoot := t.TempDir()
	if runtime.GOOS != "windows" {
		linkedRoot := filepath.Join(t.TempDir(), "config-link")
		if err := os.Symlink(configRoot, linkedRoot); err != nil {
			t.Fatal(err)
		}
		configRoot = linkedRoot
	}
	t.Setenv("XDG_CONFIG_HOME", configRoot)
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	t.Setenv("RSH_CACHE_DIR", "")
	selection, _, err := ContextManager().ResolveArgs([]string{"ozon", "--context", "seller-a"})
	if err != nil {
		t.Fatal(err)
	}
	session := Session{BaseURL: server.URL, HasCredentials: true, Credentials: Credentials{ClientID: "client", APIKey: "secret"}}
	cfg := Config{BaseURL: server.URL, SpecURL: server.URL + "/openapi.json"}
	run := func() error {
		cli := NewCLIWithContext(cfg, session, "test", "", selection)
		cli.Stdin = strings.NewReader(`{}`)
		cli.Stdout, cli.Stderr = &bytes.Buffer{}, &bytes.Buffer{}
		return RunCLIWithContext(cli, []string{"ozon", "product-api", "get-product-list", "-o", "json"}, selection)
	}
	if err := run(); err != nil {
		t.Fatalf("first run: %v", err)
	}
	if err := run(); err != nil {
		t.Fatalf("second run from operation cache: %v", err)
	}
	if specHits != 1 {
		t.Fatalf("spec hits = %d, want 1", specHits)
	}
	if apiHits != 2 {
		t.Fatalf("api hits = %d, want 2", apiHits)
	}
	if got := os.Getenv("RSH_CACHE_DIR"); got != selection.CacheDir {
		t.Fatalf("RSH_CACHE_DIR = %q, want selected context cache %q", got, selection.CacheDir)
	}
}

func TestFullSellerSpecCorpus(t *testing.T) {
	path := os.Getenv("OZON_SPEC_CHECK_FILE")
	if path == "" {
		t.Skip("OZON_SPEC_CHECK_FILE is not set")
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	_, routes, err := fixSpec(body)
	if err != nil {
		t.Fatal(err)
	}
	if len(routes) != 420 {
		t.Fatalf("got %d operations; expected pinned Seller corpus of 420", len(routes))
	}
	if _, err := (SpecLoader{}).LoadWithOptions(body, restish.LoadOptions{}); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/openapi.json" {
			_, _ = w.Write(body)
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("RSH_CACHE_DIR", filepath.Join(t.TempDir(), "cache"))
	cli := NewCLIWithSession(Config{BaseURL: server.URL, SpecURL: server.URL + "/openapi.json"}, Session{BaseURL: server.URL}, "test", "")
	var stdout, stderr bytes.Buffer
	cli.Stdout, cli.Stderr = &stdout, &stderr
	if err := RunCLI(cli, []string{"ozon", "--help"}); err != nil {
		t.Fatalf("generate full command tree: %v\nstderr=%s", err, stderr.String())
	}
	if strings.Contains(stderr.String(), "skipping ") {
		t.Fatalf("Restish skipped operations:\n%s", stderr.String())
	}
	for _, group := range []string{"product-api", "analytics-api", "finance-api", "warehouse-api"} {
		if !strings.Contains(stdout.String(), group) {
			t.Errorf("root help is missing %q", group)
		}
	}
}
