package pingcodert

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

func TestEmbeddedRestishGeneratedCommandsExecuteRequests(t *testing.T) {
	var tokenCalls atomic.Int32
	mux := http.NewServeMux()
	server := httptest.NewServer(mux)
	defer server.Close()

	spec := `[
      {
        "type":"GET","url":"/v1/pjm/projects?page_size={page_size}","group":"项目","name":"获取项目列表",
        "permission":[{"name":"企业令牌"}],"scopes":[{"name":"pcp:read:pjm:project"}],
        "parameter":{"fields":{"查询参数":[{"type":"Number","optional":false,"field":"page_size"}]}},
        "success":{"examples":[{"type":"json","content":"{\"values\":[]}"}]}
      },
      {
        "type":"POST","url":"/v1/pjm/work_items","group":"工作项","name":"创建工作项",
        "permission":[{"name":"企业令牌"}],"scopes":[{"name":"pcp:write:pjm:workitem"}],
        "parameter":{"fields":{"Parameter":[{"type":"String","optional":false,"field":"title"}]}}
      },
      {
        "type":"GET","url":"/v1/myself","group":"用户","name":"获取当前用户",
        "permission":[{"name":"用户令牌"}]
      }
    ]`
	mux.HandleFunc("/api_data.json", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, spec)
	})
	mux.HandleFunc("/v1/auth/token", func(w http.ResponseWriter, r *http.Request) {
		tokenCalls.Add(1)
		if r.URL.Query().Get("client_id") != "client" || r.URL.Query().Get("client_secret") != "secret" {
			t.Errorf("unexpected token query: %s", r.URL.RawQuery)
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"access_token":"test-token","token_type":"Bearer","expires_in":3600}`)
	})
	mux.HandleFunc("/v1/pjm/projects", func(w http.ResponseWriter, r *http.Request) {
		assertBearer(t, r)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"method": r.Method, "page_size": r.URL.Query().Get("page_size")})
	})
	mux.HandleFunc("/v1/pjm/work_items", func(w http.ResponseWriter, r *http.Request) {
		assertBearer(t, r)
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"method": r.Method, "body": body})
	})
	mux.HandleFunc("/v1/myself", func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer user-token" {
			t.Errorf("user Authorization = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"id": "user-1"})
	})

	stateDir := t.TempDir()
	t.Setenv("RSH_CONFIG_DIR", stateDir+"/config")
	t.Setenv("RSH_CACHE_DIR", stateDir+"/cache")
	t.Setenv("PINGCODE_API_BASE_URL", server.URL)
	t.Setenv("PINGCODE_CLIENT_ID", "client")
	t.Setenv("PINGCODE_CLIENT_SECRET", "secret")

	getOutput := runTestCLI(t, Config{APIBaseURL: server.URL, SpecURL: server.URL + "/api_data.json"}, "", []string{
		"pingcode", "pjm", "get-projects-by-page-size", "25", "-o", "json",
	})
	if !strings.Contains(getOutput, `"method": "GET"`) || !strings.Contains(getOutput, `"page_size": "25"`) {
		t.Fatalf("GET output = %s", getOutput)
	}

	t.Setenv("PINGCODE_WRITE_MODE", "write")
	postOutput := runTestCLI(t, Config{APIBaseURL: server.URL, SpecURL: server.URL + "/api_data.json"}, `{"title":"Restish"}`, []string{
		"pingcode", "pjm", "post-work-items", "-o", "json",
	})
	if !strings.Contains(postOutput, `"method": "POST"`) || !strings.Contains(postOutput, `"title": "Restish"`) {
		t.Fatalf("POST output = %s", postOutput)
	}
	if tokenCalls.Load() != 1 {
		t.Fatalf("token calls = %d, want one cached exchange", tokenCalls.Load())
	}

	t.Setenv("PINGCODE_ACCESS_TOKEN", "user-token")
	userOutput := runTestCLI(t, Config{APIBaseURL: server.URL, SpecURL: server.URL + "/api_data.json"}, "", []string{
		"pingcode", "myself", "get-myself", "-o", "json",
	})
	if !strings.Contains(userOutput, `"id": "user-1"`) {
		t.Fatalf("user-token output = %s", userOutput)
	}
}

func TestEmbeddedRestishBlocksWritesByDefault(t *testing.T) {
	mux := http.NewServeMux()
	server := httptest.NewServer(mux)
	defer server.Close()
	mux.HandleFunc("/api_data.json", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `[{"type":"POST","url":"/v1/pjm/work_items","name":"create","permission":[{"name":"企业令牌"}],"parameter":{"fields":{"Parameter":[{"type":"String","field":"title"}]}}}]`)
	})
	mux.HandleFunc("/v1/pjm/work_items", func(http.ResponseWriter, *http.Request) {
		t.Error("write reached API while readonly")
	})

	stateDir := t.TempDir()
	t.Setenv("RSH_CONFIG_DIR", stateDir+"/config")
	t.Setenv("RSH_CACHE_DIR", stateDir+"/cache")
	t.Setenv("PINGCODE_API_BASE_URL", server.URL)
	t.Setenv("PINGCODE_ACCESS_TOKEN", "token")
	cli := NewCLI(Config{APIBaseURL: server.URL, SpecURL: server.URL + "/api_data.json"}, "test", "")
	cli.Stdin = strings.NewReader(`{"title":"blocked"}`)
	var stdout, stderr bytes.Buffer
	cli.Stdout, cli.Stderr = &stdout, &stderr
	err := cli.Run([]string{"pingcode", "pjm", "post-work-items", "-o", "json"})
	if err == nil || !strings.Contains(err.Error(), "blocked by PINGCODE_WRITE_MODE=readonly") {
		t.Fatalf("error = %v, stdout=%s stderr=%s", err, stdout.String(), stderr.String())
	}
}

func TestEmbeddedRestishDoesNotRetryWriteAfterUnauthorized(t *testing.T) {
	var calls atomic.Int32
	mux := http.NewServeMux()
	server := httptest.NewServer(mux)
	defer server.Close()
	mux.HandleFunc("/api_data.json", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `[{"type":"POST","url":"/v1/pjm/work_items","name":"create","permission":[{"name":"企业令牌"}],"parameter":{"fields":{"Parameter":[{"type":"String","field":"title"}]}}}]`)
	})
	mux.HandleFunc("/v1/pjm/work_items", func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusUnauthorized)
	})

	stateDir := t.TempDir()
	t.Setenv("RSH_CONFIG_DIR", stateDir+"/config")
	t.Setenv("RSH_CACHE_DIR", stateDir+"/cache")
	t.Setenv("PINGCODE_API_BASE_URL", server.URL)
	t.Setenv("PINGCODE_ACCESS_TOKEN", "token")
	t.Setenv("PINGCODE_WRITE_MODE", "write")
	cli := NewCLI(Config{APIBaseURL: server.URL, SpecURL: server.URL + "/api_data.json"}, "test", "")
	cli.Stdin = strings.NewReader(`{"title":"uncertain"}`)
	var stdout, stderr bytes.Buffer
	cli.Stdout, cli.Stderr = &stdout, &stderr
	err := cli.Run([]string{"pingcode", "pjm", "post-work-items", "-o", "json"})
	if err == nil || !strings.Contains(err.Error(), "automatic retry is disabled for writes") {
		t.Fatalf("error = %v, stdout=%s stderr=%s", err, stdout.String(), stderr.String())
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("write requests = %d, want 1", got)
	}
}

func TestEmbeddedRestishUsesConfigFileClientCredentials(t *testing.T) {
	var tokenCalls atomic.Int32
	mux := http.NewServeMux()
	server := httptest.NewServer(mux)
	defer server.Close()
	mux.HandleFunc("/api_data.json", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `[{"type":"GET","url":"/v1/pjm/projects","group":"项目","name":"获取项目列表","permission":[{"name":"企业令牌"}]}]`)
	})
	mux.HandleFunc("/v1/auth/token", func(w http.ResponseWriter, r *http.Request) {
		tokenCalls.Add(1)
		if r.URL.Query().Get("client_id") != "file-client" || r.URL.Query().Get("client_secret") != "file-secret" {
			t.Errorf("unexpected token query: %s", r.URL.RawQuery)
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"access_token":"file-token","token_type":"Bearer","expires_in":3600}`)
	})
	mux.HandleFunc("/v1/pjm/projects", func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer file-token" {
			t.Errorf("Authorization = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"ok":true}`)
	})

	xdg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", xdg)
	t.Setenv("RSH_CONFIG_DIR", filepath.Join(xdg, "aicli", "pingcode"))
	t.Setenv("RSH_CACHE_DIR", filepath.Join(xdg, "cache"))
	t.Setenv("PINGCODE_ACCESS_TOKEN", "")
	t.Setenv("PINGCODE_CLIENT_ID", "")
	t.Setenv("PINGCODE_CLIENT_SECRET", "")
	if err := SaveLogin(ConfigPath(), server.URL, &AuthConfig{
		Mode: AuthModeClient, ClientID: "file-client", ClientSecret: "file-secret",
	}); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	cfg.SpecURL = server.URL + "/api_data.json"
	out := runTestCLI(t, cfg, "", []string{
		"pingcode", "pjm", "get-projects", "-o", "json",
	})
	if !strings.Contains(out, `"ok": true`) {
		t.Fatalf("output = %s", out)
	}
	if tokenCalls.Load() != 1 {
		t.Fatalf("token calls = %d", tokenCalls.Load())
	}
}

func TestEmbeddedRestishEnvOverridesConfigFile(t *testing.T) {
	mux := http.NewServeMux()
	server := httptest.NewServer(mux)
	defer server.Close()
	mux.HandleFunc("/api_data.json", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `[{"type":"GET","url":"/v1/pjm/projects","group":"项目","name":"list","permission":[{"name":"企业令牌"}]}]`)
	})
	mux.HandleFunc("/v1/auth/token", func(http.ResponseWriter, *http.Request) {
		t.Error("token endpoint should not be called for access-token override")
	})
	mux.HandleFunc("/v1/pjm/projects", func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer env-override" {
			t.Errorf("Authorization = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"source":"env"}`)
	})

	xdg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", xdg)
	t.Setenv("RSH_CONFIG_DIR", filepath.Join(xdg, "aicli", "pingcode"))
	t.Setenv("RSH_CACHE_DIR", filepath.Join(xdg, "cache"))
	if err := SaveLogin(ConfigPath(), "https://file.pingcode.test", &AuthConfig{
		Mode: AuthModeClient, ClientID: "file-client", ClientSecret: "file-secret",
	}); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PINGCODE_ACCESS_TOKEN", "env-override")
	t.Setenv("PINGCODE_API_BASE_URL", server.URL)

	out := runTestCLI(t, Config{APIBaseURL: server.URL, SpecURL: server.URL + "/api_data.json"}, "", []string{
		"pingcode", "pjm", "get-projects", "-o", "json",
	})
	if !strings.Contains(out, `"source": "env"`) {
		t.Fatalf("output = %s", out)
	}
	auth, err := LoadAuthConfig(ConfigPath())
	if err != nil || auth.ClientID != "file-client" {
		t.Fatalf("config mutated: %#v err=%v", auth, err)
	}
}

func TestEmbeddedRestishUsesConfigFileAccessToken(t *testing.T) {
	mux := http.NewServeMux()
	server := httptest.NewServer(mux)
	defer server.Close()
	mux.HandleFunc("/api_data.json", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `[{"type":"GET","url":"/v1/pjm/projects","group":"项目","name":"list","permission":[{"name":"企业令牌"}]}]`)
	})
	mux.HandleFunc("/v1/pjm/projects", func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer file-access-token" {
			t.Errorf("Authorization = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"source":"file-token"}`)
	})

	xdg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", xdg)
	t.Setenv("RSH_CONFIG_DIR", filepath.Join(xdg, "aicli", "pingcode"))
	t.Setenv("RSH_CACHE_DIR", filepath.Join(xdg, "cache"))
	t.Setenv("PINGCODE_ACCESS_TOKEN", "")
	t.Setenv("PINGCODE_CLIENT_ID", "")
	t.Setenv("PINGCODE_CLIENT_SECRET", "")
	if err := SaveLogin(ConfigPath(), server.URL, &AuthConfig{Mode: AuthModeToken, AccessToken: "file-access-token"}); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	cfg.SpecURL = server.URL + "/api_data.json"
	out := runTestCLI(t, cfg, "", []string{
		"pingcode", "pjm", "get-projects", "-o", "json",
	})
	if !strings.Contains(out, `"source": "file-token"`) {
		t.Fatalf("output = %s", out)
	}
}

func runTestCLI(t *testing.T, cfg Config, stdin string, args []string) string {
	t.Helper()
	cli := NewCLI(cfg, "test", "")
	cli.Stdin = strings.NewReader(stdin)
	var stdout, stderr bytes.Buffer
	cli.Stdout, cli.Stderr = &stdout, &stderr
	if err := cli.Run(args); err != nil {
		t.Fatalf("Run(%v): %v\nstderr: %s", args, err, stderr.String())
	}
	return stdout.String()
}

func assertBearer(t *testing.T, r *http.Request) {
	t.Helper()
	if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
		t.Errorf("Authorization = %q", got)
	}
}
