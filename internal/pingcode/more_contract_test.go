package pingcode_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/0xfe10/aicli/internal/cli"
	"github.com/0xfe10/aicli/internal/pingcode"
)

func TestSearchDedupeAndPaginationMeta(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/v1/auth/token":
			_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "t", "expires_in": 3600})
		case r.URL.Path == "/v1/project/projects":
			_ = json.NewEncoder(w).Encode(page([]any{map[string]any{"id": "p1", "name": "Demo", "identifier": "DEMO"}}))
		case r.URL.Path == "/v1/project/work_item/types":
			_ = json.NewEncoder(w).Encode(page([]any{
				map[string]any{"id": "bug", "name": "缺陷"},
				map[string]any{"id": "req", "name": "需求"},
			}))
		case r.URL.Path == "/v1/project/work_item/states":
			_ = json.NewEncoder(w).Encode(page([]any{map[string]any{"id": "s1", "name": "新提交"}}))
		case r.URL.Path == "/v1/project/work_item/priorities":
			_ = json.NewEncoder(w).Encode(page([]any{}))
		case strings.HasSuffix(r.URL.Path, "/members"):
			_ = json.NewEncoder(w).Encode(page([]any{}))
		case r.URL.Path == "/v1/project/work_items":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"page_size": 1, "page_index": 0, "total": 2,
				"values": []any{map[string]any{"id": "same", "identifier": "DEMO-1", "title": "Shared", "state": map[string]any{"name": "新提交"}}},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	dir := t.TempDir()
	t.Setenv("PINGCODE_API_BASE_URL", srv.URL)
	t.Setenv("PINGCODE_BASE_URL", "https://example.pingcode.com")
	t.Setenv("PINGCODE_CLIENT_ID", "cid")
	t.Setenv("PINGCODE_CLIENT_SECRET", "csecret")
	t.Setenv("PINGCODE_AUTH_TOKEN_PATH", filepath.Join(dir, "auth.json"))
	t.Setenv("PINGCODE_PROJECT_IDENTIFIER", "DEMO")
	t.Setenv("PINGCODE_BUG_TYPE_ID", "bug")
	t.Setenv("PINGCODE_REQUIREMENT_TYPE_ID", "req")

	var stdout bytes.Buffer
	result := pingcode.Execute(context.Background(), []string{"work-item", "search", "--page-size", "1"}, pingcode.RuntimeDependencies{Stdout: &stdout})
	if result.ExitCode != cli.ExitOK {
		t.Fatalf("exit=%d out=%s", result.ExitCode, stdout.String())
	}
	if !strings.Contains(stdout.String(), `"truncated": true`) && !strings.Contains(stdout.String(), `"truncated":true`) {
		t.Fatalf("expected truncated: %s", stdout.String())
	}
	// same id across kinds should appear once in values.
	count := strings.Count(stdout.String(), `"id": "same"`)
	if count != 1 {
		t.Fatalf("expected deduped once, got %d in %s", count, stdout.String())
	}
}

func TestRateLimitRetriesReadsOnly(t *testing.T) {
	var gets atomic.Int32
	var posts atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			gets.Add(1)
		case http.MethodPost:
			posts.Add(1)
		}
		if r.URL.Path == "/v1/auth/token" {
			_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "t", "expires_in": 3600})
			return
		}
		if r.Method == http.MethodGet && gets.Load() <= 2 {
			w.Header().Set("x-pc-retry-after", "0")
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"message":"slow down"}`))
			return
		}
		if r.URL.Path == "/v1/project/projects" {
			_ = json.NewEncoder(w).Encode(page([]any{map[string]any{"id": "p1", "name": "Demo", "identifier": "DEMO"}}))
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	dir := t.TempDir()
	cfg := pingcode.Config{
		APIBaseURL:        srv.URL,
		BaseURL:           "https://example.pingcode.com",
		ClientID:          "cid",
		ClientSecret:      "csecret",
		AuthScheme:        "Bearer",
		TimeoutMS:         5000,
		ProjectIdentifier: "DEMO",
		AuthTokenPath:     filepath.Join(dir, "auth.json"),
	}
	client := pingcode.NewClient(cfg, pingcode.NewAuthStore(cfg.AuthTokenPath))
	page, err := client.ListProjects(context.Background(), "DEMO", 0, 30)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Values) != 1 {
		t.Fatalf("unexpected page %#v", page)
	}
	if gets.Load() < 3 {
		t.Fatalf("expected retries, gets=%d", gets.Load())
	}
}

func TestCommentApplyOnceNoRetry(t *testing.T) {
	var posts atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/v1/comments" {
			posts.Add(1)
			w.WriteHeader(http.StatusTooManyRequests)
			w.Header().Set("x-pc-retry-after", "0")
			_, _ = w.Write([]byte(`{"message":"rate"}`))
			return
		}
		switch {
		case r.URL.Path == "/v1/auth/token":
			_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "t", "expires_in": 3600})
		case r.URL.Path == "/v1/project/projects":
			_ = json.NewEncoder(w).Encode(page([]any{map[string]any{"id": "p1", "name": "Demo", "identifier": "DEMO"}}))
		case r.URL.Path == "/v1/project/work_item/types":
			_ = json.NewEncoder(w).Encode(page([]any{map[string]any{"id": "bug", "name": "缺陷"}}))
		case r.URL.Path == "/v1/project/work_item/states":
			_ = json.NewEncoder(w).Encode(page([]any{map[string]any{"id": "s1", "name": "新提交"}}))
		case r.URL.Path == "/v1/project/work_item/priorities":
			_ = json.NewEncoder(w).Encode(page([]any{}))
		case strings.HasSuffix(r.URL.Path, "/members"):
			_ = json.NewEncoder(w).Encode(page([]any{}))
		case r.URL.Path == "/v1/project/work_items":
			_ = json.NewEncoder(w).Encode(page([]any{map[string]any{"id": "wi1", "identifier": "DEMO-1", "title": "Bug", "state": map[string]any{"id": "s1", "name": "新提交"}}}))
		case strings.HasPrefix(r.URL.Path, "/v1/project/work_items/"):
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "wi1", "identifier": "DEMO-1", "title": "Bug", "state": map[string]any{"id": "s1", "name": "新提交"}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	dir := t.TempDir()
	t.Setenv("PINGCODE_API_BASE_URL", srv.URL)
	t.Setenv("PINGCODE_BASE_URL", "https://example.pingcode.com")
	t.Setenv("PINGCODE_CLIENT_ID", "cid")
	t.Setenv("PINGCODE_CLIENT_SECRET", "csecret")
	t.Setenv("PINGCODE_AUTH_TOKEN_PATH", filepath.Join(dir, "auth.json"))
	t.Setenv("PINGCODE_PROJECT_IDENTIFIER", "DEMO")

	var stdout bytes.Buffer
	result := pingcode.Execute(context.Background(), []string{"work-item", "comment", "--input", "-", "--apply"}, pingcode.RuntimeDependencies{
		Stdout: &stdout,
		Stdin:  strings.NewReader(`{"kind":"bug","identifier":"DEMO-1","content":"hello"}`),
	})
	if result.ExitCode == cli.ExitOK {
		t.Fatalf("expected failure, out=%s", stdout.String())
	}
	if posts.Load() != 1 {
		t.Fatalf("comment must not retry, posts=%d", posts.Load())
	}
}

func TestAuthLoginAndComplete(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/v1/auth/token" && r.URL.Query().Get("grant_type") == "authorization_code":
			if r.URL.Query().Get("code") != "from-callback" {
				w.WriteHeader(400)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"access_token": "user-access", "refresh_token": "user-refresh", "expires_in": 3600,
			})
		case r.URL.Path == "/v1/myself":
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "u1", "display_name": "Ada"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	dir := t.TempDir()
	t.Setenv("PINGCODE_API_BASE_URL", srv.URL)
	t.Setenv("PINGCODE_BASE_URL", "https://tenant.pingcode.com")
	t.Setenv("PINGCODE_CLIENT_ID", "cid")
	t.Setenv("PINGCODE_CLIENT_SECRET", "csecret")
	t.Setenv("PINGCODE_AUTH_TOKEN_PATH", filepath.Join(dir, "auth.json"))

	var stdout bytes.Buffer
	result := pingcode.Execute(context.Background(), []string{"auth", "login"}, pingcode.RuntimeDependencies{Stdout: &stdout})
	if result.ExitCode != cli.ExitOK {
		t.Fatalf("login exit=%d out=%s", result.ExitCode, stdout.String())
	}
	var login cli.Response
	_ = json.Unmarshal(stdout.Bytes(), &login)
	data := login.Data.(map[string]any)
	state := data["state"].(string)
	if state == "" || !strings.Contains(data["url"].(string), "client_id=cid") {
		t.Fatalf("bad login data %#v", data)
	}

	stdout.Reset()
	callback := "https://tenant.pingcode.com/callback?code=from-callback&state=" + state
	result = pingcode.Execute(context.Background(), []string{"auth", "complete", "--callback-url-stdin"}, pingcode.RuntimeDependencies{
		Stdout: &stdout,
		Stdin:  strings.NewReader(callback),
	})
	if result.ExitCode != cli.ExitOK {
		t.Fatalf("complete exit=%d out=%s", result.ExitCode, stdout.String())
	}
	if strings.Contains(stdout.String(), "from-callback") || strings.Contains(stdout.String(), "user-access") {
		t.Fatalf("secrets leaked: %s", stdout.String())
	}
	store := pingcode.NewAuthStore(filepath.Join(dir, "auth.json"))
	got, err := store.Get()
	if err != nil {
		t.Fatal(err)
	}
	if got.AccessToken != "user-access" {
		t.Fatalf("token not saved: %#v", got)
	}
}
