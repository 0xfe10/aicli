package restishrt_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

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

	type resp struct {
		OK    bool           `json:"ok"`
		Data  map[string]any `json:"data"`
		Error *struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}

	parse := func(args []string) (resp, error) {
		var stdout, stderr bytes.Buffer
		rt := &restishrt.Runtime{
			APIBaseURL: srv.URL,
			Auth:       func(context.Context) (string, error) { return "Bearer test-token-secret", nil },
			Stdout:     &stdout,
			Stderr:     &stderr,
		}
		err := rt.Run(context.Background(), args)
		var out resp
		dec := json.NewDecoder(bytes.NewReader(stdout.Bytes()))
		if derr := dec.Decode(&out); derr != nil {
			t.Fatalf("decode: %v stdout=%s", derr, stdout.String())
		}
		var extra json.RawMessage
		if derr := dec.Decode(&extra); derr != io.EOF {
			t.Fatalf("stdout must be exactly one JSON document, got trailing: %s", stdout.String())
		}
		if strings.Contains(stdout.String(), "test-token-secret") || strings.Contains(stderr.String(), "test-token-secret") {
			t.Fatalf("token leaked: stdout=%s stderr=%s", stdout.String(), stderr.String())
		}
		return out, err
	}

	got, err := parse([]string{"GET", "/v1/project/projects"})
	if err != nil || !got.OK {
		t.Fatalf("GET failed: err=%v got=%#v", err, got)
	}
	got, err = parse([]string{"POST", "/v1/project/work_items", "--body", `{"title":"x"}`})
	if err != nil || !got.OK {
		t.Fatalf("POST failed: err=%v got=%#v", err, got)
	}
	got, err = parse([]string{"PATCH", "/v1/project/work_items/1", "--body", `{"title":"y"}`})
	if err == nil || got.OK {
		t.Fatalf("PATCH 400 should fail: %#v", got)
	}
	got, err = parse([]string{"DELETE", "/v1/project/work_items/1"})
	if err == nil || got.OK || got.Error == nil || got.Error.Code != "UPSTREAM_ERROR" {
		t.Fatalf("DELETE 500: %#v err=%v", got, err)
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
	var stdout bytes.Buffer
	rt := &restishrt.Runtime{
		APIBaseURL: srv.URL,
		Stdout:     &stdout,
		HTTP:       &http.Client{Timeout: time.Millisecond},
	}
	err := rt.Run(context.Background(), []string{"GET", "/slow"})
	if err == nil {
		t.Fatal("expected timeout")
	}
	var doc map[string]any
	if jerr := json.Unmarshal(stdout.Bytes(), &doc); jerr != nil {
		t.Fatal(jerr)
	}
	if doc["ok"] != false {
		t.Fatalf("doc=%#v", doc)
	}
}

func TestRawRejectsAbsoluteURL(t *testing.T) {
	var stdout bytes.Buffer
	rt := &restishrt.Runtime{APIBaseURL: "https://open.pingcode.com", Stdout: &stdout}
	err := rt.Run(context.Background(), []string{"GET", "https://evil.example/v1"})
	if err == nil {
		t.Fatal("expected reject")
	}
}
