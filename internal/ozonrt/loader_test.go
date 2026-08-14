package ozonrt

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

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
