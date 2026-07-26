package pingcode_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/0xfe10/aicli/internal/cli"
	"github.com/0xfe10/aicli/internal/pingcode"
)

func TestResolveProjectRequiresExactMatch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/v1/auth/token":
			_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "t", "expires_in": 3600})
		case r.URL.Path == "/v1/project/projects":
			_ = json.NewEncoder(w).Encode(page([]any{
				map[string]any{"id": "1", "name": "Old", "identifier": "mobile-app-old"},
				map[string]any{"id": "2", "name": "Platform", "identifier": "mobile-platform"},
			}))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	cfg := pingcode.Config{
		APIBaseURL:    srv.URL,
		BaseURL:       "https://example.pingcode.com",
		ClientID:      "cid",
		ClientSecret:  "csecret",
		AuthScheme:    "Bearer",
		TimeoutMS:     5000,
		AuthTokenPath: filepath.Join(t.TempDir(), "auth.json"),
	}
	client := pingcode.NewClient(cfg, pingcode.NewAuthStore(cfg.AuthTokenPath))
	_, err := client.ResolveProject(context.Background(), "mobile-app", "")
	if err == nil {
		t.Fatal("expected NOT_FOUND")
	}
	pe := pingcode.Classify(err)
	if pe.Code != pingcode.CodeNotFound {
		t.Fatalf("code=%s msg=%s", pe.Code, pe.Message)
	}
	if !strings.Contains(pe.Message, "mobile-app-old") {
		t.Fatalf("expected candidates in message: %s", pe.Message)
	}

	// Exact identifier match still works when present.
	srv2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/auth/token" {
			_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "t", "expires_in": 3600})
			return
		}
		_ = json.NewEncoder(w).Encode(page([]any{
			map[string]any{"id": "p1", "name": "Demo", "identifier": "DEMO"},
			map[string]any{"id": "p2", "name": "Other", "identifier": "DEMO-OLD"},
		}))
	}))
	defer srv2.Close()
	cfg.APIBaseURL = srv2.URL
	client = pingcode.NewClient(cfg, pingcode.NewAuthStore(cfg.AuthTokenPath))
	got, err := client.ResolveProject(context.Background(), "DEMO", "")
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != "p1" {
		t.Fatalf("got %#v", got)
	}
}

func TestUpdateRegetsBeforePatch(t *testing.T) {
	var patches atomic.Int32
	var gets atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/v1/auth/token":
			_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "t", "expires_in": 3600})
		case r.URL.Path == "/v1/project/projects":
			_ = json.NewEncoder(w).Encode(page([]any{map[string]any{"id": "p1", "name": "Demo", "identifier": "DEMO"}}))
		case r.URL.Path == "/v1/project/work_item/types":
			_ = json.NewEncoder(w).Encode(page([]any{map[string]any{"id": "bug", "name": "缺陷"}}))
		case r.URL.Path == "/v1/project/work_item/states":
			_ = json.NewEncoder(w).Encode(page([]any{map[string]any{"id": "s1", "name": "处理中"}, map[string]any{"id": "s2", "name": "已完成"}}))
		case r.URL.Path == "/v1/project/work_item/priorities":
			_ = json.NewEncoder(w).Encode(page([]any{}))
		case strings.HasSuffix(r.URL.Path, "/members"):
			_ = json.NewEncoder(w).Encode(page([]any{}))
		case r.URL.Path == "/v1/project/work_item_state_plans":
			_ = json.NewEncoder(w).Encode(page([]any{}))
		case r.URL.Path == "/v1/project/work_items" && r.Method == http.MethodGet:
			_ = json.NewEncoder(w).Encode(page([]any{map[string]any{
				"id": "wi1", "identifier": "DEMO-1", "title": "Bug",
				"state": map[string]any{"id": "s1", "name": "处理中"},
			}}))
		case strings.HasPrefix(r.URL.Path, "/v1/project/work_items/") && r.Method == http.MethodGet:
			n := gets.Add(1)
			state := map[string]any{"id": "s1", "name": "处理中"}
			if n >= 2 {
				state = map[string]any{"id": "s2", "name": "已完成"}
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id": "wi1", "identifier": "DEMO-1", "title": "Bug", "state": state,
			})
		case r.Method == http.MethodPatch:
			patches.Add(1)
			w.WriteHeader(200)
			_, _ = io.WriteString(w, `{}`)
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
		Stdin:  strings.NewReader(`{"kind":"bug","identifier":"DEMO-1","expectedCurrentState":"处理中","title":"new"}`),
	})
	if result.ExitCode != cli.ExitConflict {
		t.Fatalf("exit=%d out=%s", result.ExitCode, stdout.String())
	}
	if patches.Load() != 0 {
		t.Fatalf("PATCH must not fire after state drift, patches=%d", patches.Load())
	}
	if !strings.Contains(stdout.String(), "EXPECTED_STATE_MISMATCH") {
		t.Fatalf("expected mismatch: %s", stdout.String())
	}
}

func TestUpdateClearDescriptionSendsEmptyString(t *testing.T) {
	var patchBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
			_ = json.NewEncoder(w).Encode(page([]any{
				map[string]any{"id": "m1", "user": map[string]any{"id": "u1", "display_name": "Ada"}},
			}))
		case r.URL.Path == "/v1/project/work_item_state_plans":
			_ = json.NewEncoder(w).Encode(page([]any{}))
		case r.URL.Path == "/v1/project/work_items" && r.Method == http.MethodGet:
			_ = json.NewEncoder(w).Encode(page([]any{map[string]any{
				"id": "wi1", "identifier": "DEMO-1", "title": "Bug", "description": "keep me",
				"state": map[string]any{"id": "s1", "name": "新提交"},
				"assignee": map[string]any{"id": "u1", "display_name": "Ada"},
				"parent": map[string]any{"id": "parent1"},
				"properties": map[string]any{"a": 1},
			}}))
		case strings.HasPrefix(r.URL.Path, "/v1/project/work_items/") && r.Method == http.MethodGet:
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id": "wi1", "identifier": "DEMO-1", "title": "Bug", "description": "keep me",
				"state": map[string]any{"id": "s1", "name": "新提交"},
				"assignee": map[string]any{"id": "u1", "display_name": "Ada"},
				"parent": map[string]any{"id": "parent1"},
				"properties": map[string]any{"a": 1},
			})
		case r.Method == http.MethodPatch:
			b, _ := io.ReadAll(r.Body)
			patchBody = string(b)
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "wi1", "identifier": "DEMO-1", "title": "Bug"})
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
	body := `{"kind":"bug","identifier":"DEMO-1","description":"","assigneeName":"","parent":"","properties":{}}`
	result := pingcode.Execute(context.Background(), []string{"work-item", "update", "--input", "-", "--apply"}, pingcode.RuntimeDependencies{
		Stdout: &stdout,
		Stdin:  strings.NewReader(body),
	})
	if result.ExitCode != cli.ExitOK {
		t.Fatalf("exit=%d out=%s", result.ExitCode, stdout.String())
	}
	var patch map[string]any
	if err := json.Unmarshal([]byte(patchBody), &patch); err != nil {
		t.Fatalf("patch body=%q err=%v", patchBody, err)
	}
	if _, ok := patch["description"]; !ok {
		t.Fatalf("description must be present: %s", patchBody)
	}
	if patch["description"] != "" {
		t.Fatalf("description want empty string, got %#v", patch["description"])
	}
	if patch["assignee_id"] != nil {
		t.Fatalf("assignee_id should be null, got %#v body=%s", patch["assignee_id"], patchBody)
	}
	if patch["parent_id"] != nil {
		t.Fatalf("parent_id should be null, got %#v", patch["parent_id"])
	}
	props, ok := patch["properties"].(map[string]any)
	if !ok || len(props) != 0 {
		t.Fatalf("properties {} must be sent, got %#v body=%s", patch["properties"], patchBody)
	}
}

func TestUpdateClearAlreadyEmptyIsNoChange(t *testing.T) {
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
			_ = json.NewEncoder(w).Encode(page([]any{}))
		case strings.HasSuffix(r.URL.Path, "/members"):
			_ = json.NewEncoder(w).Encode(page([]any{}))
		case r.URL.Path == "/v1/project/work_items":
			_ = json.NewEncoder(w).Encode(page([]any{map[string]any{
				"id": "wi1", "identifier": "DEMO-1", "title": "Bug", "description": "",
				"state": map[string]any{"id": "s1", "name": "新提交"},
			}}))
		case strings.HasPrefix(r.URL.Path, "/v1/project/work_items/"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id": "wi1", "identifier": "DEMO-1", "title": "Bug", "description": "",
				"state": map[string]any{"id": "s1", "name": "新提交"},
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
		Stdin:  strings.NewReader(`{"kind":"bug","identifier":"DEMO-1","description":""}`),
	})
	if result.ExitCode != cli.ExitOK {
		t.Fatalf("exit=%d out=%s", result.ExitCode, stdout.String())
	}
	if patches.Load() != 0 {
		t.Fatalf("no-change clear must not PATCH, patches=%d out=%s", patches.Load(), stdout.String())
	}
	if !strings.Contains(stdout.String(), `"noChange"`) {
		t.Fatalf("expected noChange: %s", stdout.String())
	}
}

func TestCreateStopsWhenProjectNotFound(t *testing.T) {
	var posts atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			posts.Add(1)
		}
		switch {
		case r.URL.Path == "/v1/auth/token":
			_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "t", "expires_in": 3600})
		case r.URL.Path == "/v1/project/projects":
			_ = json.NewEncoder(w).Encode(page([]any{
				map[string]any{"id": "1", "identifier": "mobile-app-old", "name": "Old"},
			}))
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
	t.Setenv("PINGCODE_PROJECT_IDENTIFIER", "mobile-app")

	var stdout bytes.Buffer
	result := pingcode.Execute(context.Background(), []string{"work-item", "create", "--input", "-", "--apply"}, pingcode.RuntimeDependencies{
		Stdout: &stdout,
		Stdin:  strings.NewReader(`{"kind":"bug","title":"x"}`),
	})
	if result.ExitCode != cli.ExitNotFound {
		t.Fatalf("exit=%d out=%s", result.ExitCode, stdout.String())
	}
	if posts.Load() != 0 {
		t.Fatalf("create must stop before POST, posts=%d", posts.Load())
	}
}
