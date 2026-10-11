package hastrt

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/0xfe10/aicli/internal/authflow"
)

func TestPublishedSpecAndHTTP(t *testing.T) {
	body, err := os.ReadFile("testdata/swagger.json")
	if err != nil {
		t.Fatal(err)
	}
	fixed, err := fixSpec(body)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(fixed, []byte("#/definitions/")) {
		t.Fatal("unconverted extension references")
	}
	var spec map[string]any
	if err := json.Unmarshal(fixed, &spec); err != nil {
		t.Fatal(err)
	}
	paths := spec["paths"].(map[string]any)
	if _, ok := spec["servers"]; ok {
		t.Fatal("upstream server retained")
	}
	for path, rawItem := range paths {
		item := rawItem.(map[string]any)
		if op, ok := item["get"].(map[string]any); ok {
			if _, exists := op["requestBody"]; exists {
				t.Fatalf("GET body: %s", path)
			}
		}
	}
	mux := http.NewServeMux()
	server := httptest.NewServer(mux)
	defer server.Close()
	mux.HandleFunc("/spec.json", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			t.Error("discovery received API key")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
	})
	var reads, writes, deletes, redirects atomic.Int32
	var promptStatus atomic.Int32
	promptStatus.Store(202)
	checkAuth := func(r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-hast-key" {
			t.Error("missing bearer key")
		}
	}
	mux.HandleFunc("/api/public/v1/sessions", func(w http.ResponseWriter, r *http.Request) {
		checkAuth(r)
		reads.Add(1)
		if r.URL.Query().Get("limit") != "2" {
			t.Error("missing query parameter")
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"sessions":[],"next_cursor":"cursor-2"}`)
	})
	mux.HandleFunc("/api/public/v1/connectors/accounts/account", func(w http.ResponseWriter, r *http.Request) {
		checkAuth(r)
		data, _ := io.ReadAll(r.Body)
		if len(data) != 0 {
			t.Error("GET sent mutation body")
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"account":{"id":"account"}}`)
	})
	mux.HandleFunc("/api/public/v1/secrets", func(w http.ResponseWriter, r *http.Request) {
		checkAuth(r)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"secrets":[]}`)
	})
	mux.HandleFunc("/api/public/v1/prompts", func(w http.ResponseWriter, r *http.Request) {
		checkAuth(r)
		writes.Add(1)
		var input map[string]any
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			t.Error(err)
		}
		if input["prompt"] != "test prompt" || input["request_id"] != "request-1" {
			t.Error("wrong prompt body")
		}
		code := int(promptStatus.Load())
		if code == 0 {
			conn, _, err := w.(http.Hijacker).Hijack()
			if err != nil {
				t.Error(err)
				return
			}
			_ = conn.Close()
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Retry-After", "0")
		if code == 307 || code == 308 {
			w.Header().Set("Location", server.URL+"/redirect")
		}
		w.WriteHeader(code)
		fmt.Fprint(w, `{"session_id":"session","request_id":"request-1","status":"queued"}`)
	})
	mux.HandleFunc("/redirect", func(w http.ResponseWriter, r *http.Request) { redirects.Add(1); w.WriteHeader(202) })
	mux.HandleFunc("/api/public/v1/sessions/session", func(w http.ResponseWriter, r *http.Request) {
		checkAuth(r)
		if r.Method != "DELETE" {
			t.Error("wrong delete method")
		}
		deletes.Add(1)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{}`)
	})
	mux.HandleFunc("/api/public/v1/sessions/session/events/stream", func(w http.ResponseWriter, r *http.Request) {
		checkAuth(r)
		if r.URL.Query().Get("after") != "7" || r.Header.Get("Last-Event-ID") != "7" {
			t.Error("missing replay cursor")
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "id: 8\nevent: message.completed\ndata: {\"type\":\"message.completed\",\"sequence\":8}\n\nid: 9\nevent: run.completed\ndata: {\"type\":\"run.completed\",\"sequence\":9}\n\n")
	})
	mux.HandleFunc("/api/public/v1/files", func(w http.ResponseWriter, r *http.Request) {
		checkAuth(r)
		f, _, err := r.FormFile("file")
		if err != nil {
			t.Error(err)
			w.WriteHeader(400)
			return
		}
		defer f.Close()
		data, _ := io.ReadAll(f)
		if string(data) != "upload-content" {
			t.Error("upload content lost")
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(201)
		fmt.Fprint(w, `{"file":{"file_id":"file-1"}}`)
	})
	mux.HandleFunc("/api/public/v1/files/note.txt", func(w http.ResponseWriter, r *http.Request) {
		checkAuth(r)
		if r.URL.Query().Get("file_id") != "file-1" {
			t.Error("missing file ID")
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = w.Write([]byte{0, 1, 2, 255})
	})
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("RSH_CACHE_DIR", t.TempDir())
	t.Setenv("HAST_WRITE_MODE", "")
	selection := ContextManager().DefaultSelection()
	cfg := Config{BaseURL: server.URL, SpecURL: server.URL + "/spec.json"}
	session := Session{BaseURL: server.URL, HasCredentials: true, Credentials: Credentials{APIKey: "test-hast-key"}}
	run := func(args ...string) (string, error) {
		cli := NewCLIWithContext(cfg, session, "test", "test", selection)
		var output bytes.Buffer
		cli.Stdout, cli.Stderr = &output, &output
		err := RunCLIWithContext(cli, append([]string{"hast"}, args...), selection)
		return output.String(), err
	}
	output, err := run("public-task-agents", "--help")
	if err != nil {
		t.Fatal(err)
	}
	commands := regexp.MustCompile(`(?m)^  (?:get|post|put|patch|delete)-\S+`).FindAllString(output, -1)
	if len(commands) != 39 {
		t.Fatalf("expected 39 commands, got %d: %s", len(commands), output)
	}
	for i := 0; i < 2; i++ {
		output, err := run("public-task-agents", "get-api-public-v1-sessions", "--limit", "2", "-o", "json", "-v")
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(output, "test-hast-key") {
			t.Fatal("verbose key leak")
		}
	}
	if reads.Load() != 2 {
		t.Fatal("authenticated response cached")
	}
	for _, command := range []string{"get-api-public-v1-secrets", "get-api-public-v1-connectors-accounts-id"} {
		args := []string{"public-task-agents", command}
		if strings.HasSuffix(command, "-id") {
			args = append(args, "account")
		}
		if _, err := run(args...); err != nil {
			t.Fatal(err)
		}
	}
	promptArgs := []string{"public-task-agents", "post-api-public-v1-prompts", "prompt: test prompt, request_id: request-1", "-o", "json"}
	if _, err := run(promptArgs...); err == nil {
		t.Fatal("readonly prompt allowed")
	}
	if writes.Load() != 0 {
		t.Fatal("blocked prompt reached server")
	}
	t.Setenv("HAST_WRITE_MODE", "write")
	t.Setenv("RSH_RETRY_UNSAFE", "1")
	t.Setenv("RSH_RETRY", "3")
	for _, code := range []int{202, 401, 409, 429, 503, 307, 308, 0} {
		promptStatus.Store(int32(code))
		before := writes.Load()
		args := append(append([]string{}, promptArgs...), "--rsh-retry-unsafe", "--rsh-retry", "3")
		_, err := run(args...)
		if code == 202 && err != nil {
			t.Fatal(err)
		}
		if code != 202 && err == nil {
			t.Fatalf("status %d did not fail", code)
		}
		if writes.Load() != before+1 {
			t.Fatal("prompt replayed")
		}
	}
	if redirects.Load() != 0 {
		t.Fatal("redirect replayed")
	}
	if _, err := run("public-task-agents", "delete-api-public-v1-sessions-id", "session"); err == nil {
		t.Fatal("write mode allowed delete")
	}
	if deletes.Load() != 0 {
		t.Fatal("blocked delete reached server")
	}
	t.Setenv("HAST_WRITE_MODE", "destructive")
	if _, err := run("public-task-agents", "delete-api-public-v1-sessions-id", "session"); err != nil {
		t.Fatal(err)
	}
	if deletes.Load() != 1 {
		t.Fatal("delete not executed")
	}
	var bodylessWrites atomic.Int32
	var bodylessRedirectStatus atomic.Int32
	for _, path := range []string{"/api/public/v1/sessions/bodyless/cancel", "/api/public/v1/sessions/bodyless/interrupt", "/api/public/v1/sessions/bodyless"} {
		mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
			checkAuth(r)
			bodylessWrites.Add(1)
			w.Header().Set("Location", server.URL+"/redirect")
			w.WriteHeader(int(bodylessRedirectStatus.Load()))
		})
	}
	for _, status := range []int32{307, 308} {
		bodylessRedirectStatus.Store(status)
		for _, command := range []string{"post-api-public-v1-sessions-id-cancel", "post-api-public-v1-sessions-id-interrupt", "delete-api-public-v1-sessions-id"} {
			before := bodylessWrites.Load()
			if _, err := run("public-task-agents", command, "bodyless"); err == nil {
				t.Fatal("bodyless redirect did not fail")
			}
			if bodylessWrites.Load() != before+1 {
				t.Fatal("bodyless write replayed")
			}
		}
	}
	if redirects.Load() != 0 {
		t.Fatal("bodyless write replayed through redirect")
	}
	file := filepath.Join(t.TempDir(), "upload.txt")
	if err := os.WriteFile(file, []byte("upload-content"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := run("public-task-agents", "post-api-public-v1-files", "file: @"+file); err != nil {
		t.Fatal(err)
	}
	output, err = run("public-task-agents", "get-api-public-v1-files-relativepath", "note.txt", "file-1")
	if err != nil || output != string([]byte{0, 1, 2, 255}) {
		t.Fatalf("binary download: %v, %q", err, output)
	}
	output, err = run("public-task-agents", "get-api-public-v1-sessions-id-events-stream", "session", "--after", "7", "-H", "Last-Event-ID: 7", "-o", "ndjson")
	if err != nil || !strings.Contains(output, "run.completed") {
		t.Fatalf("SSE: %v, %s", err, output)
	}
}

func TestQuickstartEnvironment(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("HAST_BASE_URL", "")
	t.Setenv("HAST_API_KEY", "test-key")
	t.Setenv("HAST_SPEC_URL", "")
	for _, base := range []string{"https://backend.hast.so/api/public/v1", "https://backend.hast.so/api/public/v1/", "https://backend.hast.so"} {
		t.Setenv("HAST_API_BASE", base)
		session, cfg, err := LoadSessionWithContext(ContextManager().DefaultSelection())
		if err != nil || cfg.BaseURL != DefaultBaseURL || cfg.SpecURL != DefaultSpecURL || !session.HasCredentials {
			t.Fatalf("%s: %+v, %v", base, cfg, err)
		}
	}
	t.Setenv("HAST_API_BASE", "https://backend.hast.so/wrong")
	if _, _, err := LoadSessionWithContext(ContextManager().DefaultSelection()); err == nil {
		t.Fatal("invalid API prefix allowed")
	}
	t.Setenv("HAST_BASE_URL", "https://override.test")
	session, _, err := LoadSessionWithContext(ContextManager().DefaultSelection())
	if err != nil || session.BaseURL != "https://override.test" || session.BaseURLSource != authflow.SourceEnvironment {
		t.Fatal("explicit origin precedence failed")
	}
}
