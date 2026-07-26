package pingcode_test

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
	"sync/atomic"
	"testing"
	"time"

	"github.com/0xfe10/aicli/internal/cli"
	"github.com/0xfe10/aicli/internal/pingcode"
)

func TestVersionJSONContract(t *testing.T) {
	var stdout bytes.Buffer
	result := pingcode.Execute(context.Background(), []string{"version"}, pingcode.RuntimeDependencies{
		Stdout: &stdout,
		Version: pingcode.VersionInfo{
			CLI: "0.1.0", Commit: "abc", Go: "go1.25.3", Restish: "2.3.0",
		},
	})
	if result.ExitCode != cli.ExitOK {
		t.Fatalf("exit=%d stdout=%s", result.ExitCode, stdout.String())
	}
	var resp cli.Response
	if err := json.Unmarshal(stdout.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if !resp.OK || resp.Error != nil {
		t.Fatalf("unexpected response: %+v", resp)
	}
	data, _ := resp.Data.(map[string]any)
	if data["cli"] != "0.1.0" || data["restish"] != "2.3.0" {
		t.Fatalf("unexpected data: %#v", data)
	}
	if strings.Contains(stdout.String(), "PINGCODE_") {
		t.Fatal("version output leaked env info")
	}
}

func TestUnknownCommandExitCode(t *testing.T) {
	var stdout bytes.Buffer
	result := pingcode.Execute(context.Background(), []string{"nope"}, pingcode.RuntimeDependencies{Stdout: &stdout})
	if result.ExitCode != cli.ExitUsage {
		t.Fatalf("exit=%d", result.ExitCode)
	}
}

func TestAuthStorePermissionsAndAtomicWrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "auth.json")
	store := pingcode.NewAuthStore(path)
	if _, err := store.Save(pingcode.StoredTokens{
		AccessToken:  "secret-token",
		RefreshToken: "secret-refresh",
		TokenType:    "Bearer",
		ExpiresAt:    time.Now().Add(time.Hour).UnixMilli(),
	}); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("file mode=%04o", info.Mode().Perm())
	}
	dirInfo, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if dirInfo.Mode().Perm()&0o077 != 0 {
		t.Fatalf("dir mode too open: %04o", dirInfo.Mode().Perm())
	}
	got, err := store.Get()
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || got.AccessToken != "secret-token" {
		t.Fatalf("roundtrip failed: %#v", got)
	}
	if err := store.Clear(); err != nil {
		t.Fatal(err)
	}
	if store.HasToken() {
		t.Fatal("expected cleared")
	}
}

func TestClientCredentialsAndUserTokenPriority(t *testing.T) {
	var tokenHits atomic.Int32
	var sawAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/v1/auth/token"):
			tokenHits.Add(1)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"access_token": "app-token",
				"token_type":   "Bearer",
				"expires_in":   3600,
			})
		case r.URL.Path == "/v1/myself":
			sawAuth = r.Header.Get("Authorization")
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "u1", "display_name": "Ada"})
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
	t.Setenv("PINGCODE_ACCESS_TOKEN", "")

	cfg, err := pingcode.LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	store := pingcode.NewAuthStore(cfg.AuthTokenPath)
	client := pingcode.NewClient(cfg, store)
	user, err := client.GetCurrentUser(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if user.DisplayName != "Ada" || sawAuth != "Bearer app-token" || tokenHits.Load() != 1 {
		t.Fatalf("user=%#v auth=%q hits=%d", user, sawAuth, tokenHits.Load())
	}

	if _, err := store.Save(pingcode.StoredTokens{
		AccessToken: "user-token",
		TokenType:   "Bearer",
		ExpiresAt:   time.Now().Add(time.Hour).UnixMilli(),
	}); err != nil {
		t.Fatal(err)
	}
	user, err = client.GetCurrentUser(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if sawAuth != "Bearer user-token" || user.ID != "u1" {
		t.Fatalf("expected user token priority, auth=%q", sawAuth)
	}
}

func TestRefreshBeforeExpiryAnd401RetryOnce(t *testing.T) {
	var authTokenCalls atomic.Int32
	var myselfCalls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/v1/auth/token" && r.URL.Query().Get("grant_type") == "refresh_token":
			authTokenCalls.Add(1)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"access_token":  "refreshed-token",
				"refresh_token": "keep-refresh",
				"token_type":    "Bearer",
				"expires_in":    3600,
			})
		case r.URL.Path == "/v1/myself":
			n := myselfCalls.Add(1)
			authz := r.Header.Get("Authorization")
			if n == 1 && authz == "Bearer stale-token" {
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = io.WriteString(w, `{"message":"expired"}`)
				return
			}
			if authz != "Bearer refreshed-token" {
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = io.WriteString(w, `{"message":"bad"}`)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "u1", "display_name": "Ada"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	dir := t.TempDir()
	store := pingcode.NewAuthStore(filepath.Join(dir, "auth.json"))
	_, _ = store.Save(pingcode.StoredTokens{
		AccessToken:  "stale-token",
		RefreshToken: "refresh-me",
		TokenType:    "Bearer",
		ExpiresAt:    time.Now().Add(30 * time.Second).UnixMilli(), // within 60s skew
	})
	cfg := pingcode.Config{
		APIBaseURL: srv.URL,
		BaseURL:    "https://example.pingcode.com",
		AuthScheme: "Bearer",
		TimeoutMS:  5000,
	}
	client := pingcode.NewClient(cfg, store)
	user, err := client.GetCurrentUser(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if user.DisplayName != "Ada" {
		t.Fatalf("unexpected user %#v", user)
	}
	if authTokenCalls.Load() < 1 {
		t.Fatal("expected refresh")
	}
	got, err := store.Get()
	if err != nil {
		t.Fatal(err)
	}
	if got.AccessToken != "refreshed-token" {
		t.Fatalf("store not updated: %#v", got)
	}
}

func TestWriteCommandsDefaultZeroWrites(t *testing.T) {
	var writes atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost || r.Method == http.MethodPatch || r.Method == http.MethodDelete {
			writes.Add(1)
		}
		switch {
		case r.URL.Path == "/v1/auth/token":
			_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "t", "expires_in": 3600})
		case r.URL.Path == "/v1/project/projects":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"page_size": 30, "page_index": 0, "total": 1,
				"values": []any{map[string]any{"id": "p1", "name": "Demo", "identifier": "DEMO"}},
			})
		case r.URL.Path == "/v1/project/work_item/types":
			_ = json.NewEncoder(w).Encode(page([]any{map[string]any{"id": "bug", "name": "缺陷"}}))
		case r.URL.Path == "/v1/project/work_item/states":
			_ = json.NewEncoder(w).Encode(page([]any{map[string]any{"id": "s1", "name": "新提交"}, map[string]any{"id": "s2", "name": "已修复"}}))
		case r.URL.Path == "/v1/project/work_item/priorities":
			_ = json.NewEncoder(w).Encode(page([]any{map[string]any{"id": "pr1", "name": "高"}}))
		case strings.HasSuffix(r.URL.Path, "/members"):
			_ = json.NewEncoder(w).Encode(page([]any{map[string]any{"id": "m1", "user": map[string]any{"id": "u1", "display_name": "Ada"}}}))
		case r.URL.Path == "/v1/project/work_item_state_plans":
			_ = json.NewEncoder(w).Encode(page([]any{}))
		case r.URL.Path == "/v1/project/work_items" && r.Method == http.MethodGet:
			if id := r.URL.Query().Get("identifier"); id != "" {
				_ = json.NewEncoder(w).Encode(page([]any{map[string]any{
					"id": "wi1", "identifier": id, "title": "Bug", "state": map[string]any{"id": "s1", "name": "新提交"},
				}}))
				return
			}
			_ = json.NewEncoder(w).Encode(page([]any{}))
		case strings.HasPrefix(r.URL.Path, "/v1/project/work_items/"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id": "wi1", "identifier": "DEMO-1", "title": "Bug", "state": map[string]any{"id": "s1", "name": "新提交"},
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

	cases := []struct {
		name string
		args []string
		body string
	}{
		{"create", []string{"work-item", "create", "--input", "-"}, `{"kind":"bug","title":"t"}`},
		{"update", []string{"work-item", "update", "--input", "-"}, `{"kind":"bug","identifier":"DEMO-1","title":"new"}`},
		{"transition", []string{"work-item", "transition", "--input", "-"}, `{"kind":"bug","identifier":"DEMO-1","statusName":"已修复","expectedCurrentState":"新提交"}`},
		{"comment", []string{"work-item", "comment", "--input", "-"}, `{"kind":"bug","identifier":"DEMO-1","content":"hi"}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			writes.Store(0)
			var stdout bytes.Buffer
			result := pingcode.Execute(context.Background(), tc.args, pingcode.RuntimeDependencies{
				Stdout: &stdout,
				Stdin:  strings.NewReader(tc.body),
			})
			if result.ExitCode != cli.ExitOK {
				t.Fatalf("exit=%d out=%s", result.ExitCode, stdout.String())
			}
			if writes.Load() != 0 {
				t.Fatalf("expected zero writes, got %d", writes.Load())
			}
			if !strings.Contains(stdout.String(), `"dryRun": true`) && !strings.Contains(stdout.String(), `"dryRun":true`) {
				t.Fatalf("expected dryRun true: %s", stdout.String())
			}
		})
	}
}

func TestExpectedStateMismatchAndNoChange(t *testing.T) {
	var patches atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPatch {
			patches.Add(1)
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
			_ = json.NewEncoder(w).Encode(page([]any{map[string]any{"id": "pr1", "name": "高"}}))
		case strings.HasSuffix(r.URL.Path, "/members"):
			_ = json.NewEncoder(w).Encode(page([]any{}))
		case r.URL.Path == "/v1/project/work_item_state_plans":
			_ = json.NewEncoder(w).Encode(page([]any{}))
		case r.URL.Path == "/v1/project/work_items":
			_ = json.NewEncoder(w).Encode(page([]any{map[string]any{
				"id": "wi1", "identifier": "DEMO-1", "title": "Bug", "state": map[string]any{"id": "s1", "name": "新提交"},
			}}))
		case strings.HasPrefix(r.URL.Path, "/v1/project/work_items/"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id": "wi1", "identifier": "DEMO-1", "title": "Bug", "state": map[string]any{"id": "s1", "name": "新提交"},
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

	var stdout bytes.Buffer
	result := pingcode.Execute(context.Background(), []string{"work-item", "update", "--input", "-", "--apply"}, pingcode.RuntimeDependencies{
		Stdout: &stdout,
		Stdin:  strings.NewReader(`{"kind":"bug","identifier":"DEMO-1","expectedCurrentState":"处理中","title":"x"}`),
	})
	if result.ExitCode != cli.ExitConflict {
		t.Fatalf("exit=%d out=%s", result.ExitCode, stdout.String())
	}
	if !strings.Contains(stdout.String(), "EXPECTED_STATE_MISMATCH") {
		t.Fatalf("expected mismatch: %s", stdout.String())
	}

	stdout.Reset()
	patches.Store(0)
	result = pingcode.Execute(context.Background(), []string{"work-item", "update", "--input", "-", "--apply"}, pingcode.RuntimeDependencies{
		Stdout: &stdout,
		Stdin:  strings.NewReader(`{"kind":"bug","identifier":"DEMO-1","title":"Bug"}`),
	})
	if result.ExitCode != cli.ExitOK {
		t.Fatalf("exit=%d out=%s", result.ExitCode, stdout.String())
	}
	if patches.Load() != 0 {
		t.Fatalf("no-change should not PATCH, got %d", patches.Load())
	}
	if !strings.Contains(stdout.String(), `"noChange"`) {
		t.Fatalf("expected noChange: %s", stdout.String())
	}
}

func TestReadonlyBlocksApply(t *testing.T) {
	var writes atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writes.Add(1)
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
	t.Setenv("PINGCODE_READONLY", "true")

	var stdout bytes.Buffer
	result := pingcode.Execute(context.Background(), []string{"work-item", "create", "--input", "-", "--apply"}, pingcode.RuntimeDependencies{
		Stdout: &stdout,
		Stdin:  strings.NewReader(`{"kind":"bug","title":"x"}`),
	})
	if result.ExitCode != cli.ExitForbidden {
		t.Fatalf("exit=%d out=%s", result.ExitCode, stdout.String())
	}
	if writes.Load() != 0 {
		t.Fatalf("readonly wrote %d", writes.Load())
	}
	if !strings.Contains(stdout.String(), "READONLY") {
		t.Fatalf("expected READONLY: %s", stdout.String())
	}
}

func TestRedactSecrets(t *testing.T) {
	in := `access_token=abc123&refresh_token=def&client_secret=zzz Authorization: Bearer tok.en-1 code=authcode`
	out := pingcode.Redact(in)
	for _, secret := range []string{"abc123", "def", "zzz", "tok.en-1", "authcode"} {
		if strings.Contains(out, secret) {
			t.Fatalf("secret leaked in %q", out)
		}
	}
}

func TestRejectApplyInJSONBody(t *testing.T) {
	var stdout bytes.Buffer
	result := pingcode.Execute(context.Background(), []string{"work-item", "create", "--input", "-"}, pingcode.RuntimeDependencies{
		Stdout: &stdout,
		Stdin:  strings.NewReader(`{"title":"x","apply":true}`),
	})
	if result.ExitCode != cli.ExitUsage {
		t.Fatalf("exit=%d out=%s", result.ExitCode, stdout.String())
	}
	if !strings.Contains(stdout.String(), "INVALID_ARGUMENT") {
		t.Fatalf("expected INVALID_ARGUMENT: %s", stdout.String())
	}
}

func TestAmbiguousName(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/v1/auth/token":
			_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "t", "expires_in": 3600})
		case r.URL.Path == "/v1/project/projects":
			_ = json.NewEncoder(w).Encode(page([]any{map[string]any{"id": "p1", "name": "Demo", "identifier": "DEMO"}}))
		case r.URL.Path == "/v1/project/work_item/types":
			_ = json.NewEncoder(w).Encode(page([]any{map[string]any{"id": "bug", "name": "缺陷"}}))
		case r.URL.Path == "/v1/project/work_item/states":
			_ = json.NewEncoder(w).Encode(page([]any{
				map[string]any{"id": "s1", "name": "进行中"},
				map[string]any{"id": "s2", "name": "进行中"},
			}))
		case r.URL.Path == "/v1/project/work_item/priorities":
			_ = json.NewEncoder(w).Encode(page([]any{}))
		case strings.HasSuffix(r.URL.Path, "/members"):
			_ = json.NewEncoder(w).Encode(page([]any{}))
		case r.URL.Path == "/v1/project/work_items":
			_ = json.NewEncoder(w).Encode(page([]any{map[string]any{"id": "wi1", "identifier": "DEMO-1", "title": "Bug", "state": map[string]any{"id": "s1", "name": "新提交"}}}))
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
	result := pingcode.Execute(context.Background(), []string{"work-item", "transition", "--input", "-"}, pingcode.RuntimeDependencies{
		Stdout: &stdout,
		Stdin:  strings.NewReader(`{"kind":"bug","identifier":"DEMO-1","statusName":"进行中"}`),
	})
	if result.ExitCode != cli.ExitConflict {
		t.Fatalf("exit=%d out=%s", result.ExitCode, stdout.String())
	}
	if !strings.Contains(stdout.String(), "AMBIGUOUS_NAME") {
		t.Fatalf("expected AMBIGUOUS_NAME: %s", stdout.String())
	}
}

func page(values []any) map[string]any {
	return map[string]any{"page_size": 30, "page_index": 0, "total": len(values), "values": values}
}
