package fnsrt

import (
	"bytes"
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
	t.Setenv("RSH_CONFIG_DIR", stateDir+"/config")
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
	t.Setenv("RSH_CONFIG_DIR", stateDir+"/config")
	t.Setenv("RSH_CACHE_DIR", stateDir+"/cache")
	t.Setenv("FNS_ACCESS_TOKEN", "token")
	t.Setenv("FNS_WRITE_MODE", "")

	cli := NewCLI(Config{BaseURL: server.URL, SpecURL: server.URL + "/openapi.yaml", Client: "aicli", Version: "test"}, "test", "")
	cli.Stdin = strings.NewReader(`{"vault":"genesis","path":"a.md","content":"x"}`)
	var stdout, stderr bytes.Buffer
	cli.Stdout, cli.Stderr = &stdout, &stderr
	err = cli.Run([]string{"fns", "note", "post-api-note", "-o", "json"})
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
	t.Setenv("RSH_CONFIG_DIR", stateDir+"/config")
	t.Setenv("RSH_CACHE_DIR", stateDir+"/cache")
	t.Setenv("FNS_ACCESS_TOKEN", "token")
	t.Setenv("FNS_WRITE_MODE", "write")

	cli := NewCLI(Config{BaseURL: server.URL, SpecURL: server.URL + "/openapi.yaml", Client: "aicli", Version: "test"}, "test", "")
	cli.Stdin = strings.NewReader(`{"vault":"genesis","path":"a.md","content":"x"}`)
	var stdout, stderr bytes.Buffer
	cli.Stdout, cli.Stderr = &stdout, &stderr
	err = cli.Run([]string{"fns", "note", "post-api-note", "-o", "json"})
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
	t.Setenv("RSH_CONFIG_DIR", stateDir+"/config")
	t.Setenv("RSH_CACHE_DIR", stateDir+"/cache")
	t.Setenv("FNS_ACCESS_TOKEN", "token")
	t.Setenv("FNS_WRITE_MODE", "readonly")

	cli := NewCLI(Config{BaseURL: DefaultBaseURL, SpecURL: server.URL + "/openapi.yaml", Client: "aicli", Version: "test"}, "test", "")
	var stdout, stderr bytes.Buffer
	cli.Stdout, cli.Stderr = &stdout, &stderr
	err = cli.Run([]string{"fns", "note", "get-api-note", "Notes/test.md", "genesis", "-o", "json"})
	if err == nil || !strings.Contains(err.Error(), "FNS Base URL is not configured") {
		t.Fatalf("error = %v stdout=%s stderr=%s", err, stdout.String(), stderr.String())
	}
}

func runTestCLI(t *testing.T, cfg Config, stdin string, args []string) string {
	t.Helper()
	cli := NewCLI(cfg, cfg.Version, "")
	cli.Stdin = strings.NewReader(stdin)
	var stdout, stderr bytes.Buffer
	cli.Stdout, cli.Stderr = &stdout, &stderr
	if err := cli.Run(args); err != nil {
		t.Fatalf("Run(%v): %v\nstderr: %s", args, err, stderr.String())
	}
	return stdout.String()
}
