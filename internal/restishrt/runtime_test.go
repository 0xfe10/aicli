package restishrt_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/0xfe10/aicli/internal/pingcode"
	"github.com/0xfe10/aicli/internal/restishrt"
)

func TestRawSingleJSONAndNoOpenAPIDiscovery(t *testing.T) {
	var sawOpenAPI bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "openapi") || strings.Contains(r.URL.Path, "swagger") {
			sawOpenAPI = true
		}
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/project/projects":
			_ = json.NewEncoder(w).Encode(map[string]any{"total": 1, "values": []any{}})
		case r.Method == http.MethodPost && r.URL.Path == "/v1/project/work_items":
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "wi1"})
		case r.Method == http.MethodPatch:
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]any{"message": "bad"})
		case r.Method == http.MethodDelete:
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = io.WriteString(w, "boom")
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	run := func(args []string) (restishrt.Result, error) {
		rt := &restishrt.Runtime{
			APIBaseURL: srv.URL,
			Auth:       func(context.Context) (string, error) { return "Bearer test-token-secret", nil },
		}
		return rt.Run(context.Background(), args)
	}

	got, err := run([]string{"GET", "/v1/project/projects"})
	if err != nil || got.Data == nil {
		t.Fatalf("GET failed: err=%v got=%#v", err, got)
	}
	got, err = run([]string{"POST", "/v1/project/work_items", "--body", `{"title":"x"}`})
	if err != nil || got.Data == nil {
		t.Fatalf("POST failed: err=%v got=%#v", err, got)
	}
	_, err = run([]string{"PATCH", "/v1/project/work_items/1", "--body", `{"title":"y"}`})
	if err == nil {
		t.Fatal("PATCH 400 should fail")
	}
	_, err = run([]string{"DELETE", "/v1/project/work_items/1"})
	var coded interface{ ErrorCode() string }
	if err == nil || !errors.As(err, &coded) || coded.ErrorCode() != "UPSTREAM_ERROR" {
		t.Fatalf("DELETE 500: err=%v", err)
	}
	if sawOpenAPI {
		t.Fatal("raw must not probe openapi/swagger")
	}
}

func TestRawTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(50 * time.Millisecond)
		w.WriteHeader(200)
	}))
	defer srv.Close()
	rt := &restishrt.Runtime{
		APIBaseURL: srv.URL,
		HTTP:       &http.Client{Timeout: time.Millisecond},
	}
	_, err := rt.Run(context.Background(), []string{"GET", "/slow"})
	if err == nil {
		t.Fatal("expected timeout")
	}
}

func TestRawRejectsAbsoluteURL(t *testing.T) {
	rt := &restishrt.Runtime{APIBaseURL: "https://open.pingcode.com"}
	_, err := rt.Run(context.Background(), []string{"GET", "https://evil.example/v1"})
	if err == nil {
		t.Fatal("expected reject")
	}
}

func TestCommandLayerOwnsRawJSONOnceOnError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"message":"bad"}`, http.StatusBadRequest)
	}))
	defer srv.Close()

	rt := &restishrt.Runtime{APIBaseURL: srv.URL}
	var stdout bytes.Buffer
	result := pingcode.Execute(context.Background(), []string{"raw", "GET", "/bad"}, pingcode.RuntimeDependencies{
		Stdout: &stdout,
		Raw: func(ctx context.Context, args []string) (pingcode.RawResult, error) {
			rawResult, err := rt.Run(ctx, args)
			return pingcode.RawResult{Data: rawResult.Data, Meta: rawResult.Meta}, err
		},
	})
	if result.ExitCode == 0 {
		t.Fatal("expected non-zero exit")
	}
	dec := json.NewDecoder(bytes.NewReader(stdout.Bytes()))
	var doc map[string]any
	if err := dec.Decode(&doc); err != nil {
		t.Fatalf("decode: %v output=%s", err, stdout.String())
	}
	var extra json.RawMessage
	if err := dec.Decode(&extra); err != io.EOF {
		t.Fatalf("expected exactly one JSON document: %s", stdout.String())
	}
	if doc["ok"] != false {
		t.Fatalf("unexpected doc: %#v", doc)
	}
	data, _ := doc["data"].(map[string]any)
	if data == nil || data["status"] != float64(400) {
		t.Fatalf("expected HTTP data on failure: %#v", doc)
	}
	meta, _ := doc["meta"].(map[string]any)
	if meta["transport"] != "controlled-net-http" {
		t.Fatalf("unexpected transport metadata: %#v", meta)
	}
}

func TestWorkItemCreateHelpIsCommandSpecific(t *testing.T) {
	var stdout bytes.Buffer
	result := pingcode.Execute(context.Background(), []string{"work-item", "create", "--help"}, pingcode.RuntimeDependencies{
		Stdout: &stdout,
	})
	if result.ExitCode != 0 {
		t.Fatalf("exit=%d", result.ExitCode)
	}
	out := stdout.String()
	if !strings.Contains(out, "pingcode work-item create") || !strings.Contains(out, "stdin JSON fields") {
		t.Fatalf("expected create help, got: %s", out)
	}
	if strings.Contains(out, "pingcode auth status|login") {
		t.Fatalf("root help leaked into create --help: %s", out)
	}
}
