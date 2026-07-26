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

func TestDecodeStrictJSON(t *testing.T) {
	var in pingcode.CreateInput
	if err := pingcode.DecodeStrictJSON([]byte(`{"kind":"bug","title":"t"}`), &in); err != nil {
		t.Fatal(err)
	}
	if in.Title != "t" || in.Kind != pingcode.KindBug {
		t.Fatalf("unexpected %#v", in)
	}

	cases := []struct {
		name string
		raw  string
	}{
		{"empty", ""},
		{"unknown", `{"kind":"bug","title":"t","expectedCurentState":"x"}`},
		{"two docs", "{\"kind\":\"bug\",\"title\":\"t\"}\n{\"kind\":\"bug\",\"title\":\"u\"}"},
		{"trailing garbage", `{"kind":"bug","title":"t"} nope`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var dest pingcode.CreateInput
			err := pingcode.DecodeStrictJSON([]byte(tc.raw), &dest)
			if err == nil {
				t.Fatal("expected error")
			}
			pe := pingcode.Classify(err)
			if pe.Code != pingcode.CodeInvalidArgument {
				t.Fatalf("code=%s msg=%s", pe.Code, pe.Message)
			}
		})
	}
}

func TestWriteCommandsRejectUnknownFields(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		http.Error(w, "should not be called", 500)
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
		action string
		body   string
	}{
		{"create", `{"kind":"bug","title":"t","typoField":1}`},
		{"update", `{"kind":"bug","identifier":"DEMO-1","expectedCurentState":"新提交"}`},
		{"transition", `{"kind":"bug","identifier":"DEMO-1","statusName":"已修复","extra":true}`},
		{"comment", `{"kind":"bug","identifier":"DEMO-1","content":"hi","oops":1}`},
	}
	for _, tc := range cases {
		t.Run(tc.action, func(t *testing.T) {
			hits.Store(0)
			var stdout bytes.Buffer
			result := pingcode.Execute(context.Background(), []string{"work-item", tc.action, "--input", "-"}, pingcode.RuntimeDependencies{
				Stdout: &stdout,
				Stdin:  strings.NewReader(tc.body),
			})
			if result.ExitCode != cli.ExitUsage {
				t.Fatalf("exit=%d out=%s", result.ExitCode, stdout.String())
			}
			var resp cli.Response
			if err := json.Unmarshal(stdout.Bytes(), &resp); err != nil {
				t.Fatal(err)
			}
			if resp.Error == nil || resp.Error.Code != pingcode.CodeInvalidArgument {
				t.Fatalf("expected INVALID_ARGUMENT, got %#v", resp.Error)
			}
			if hits.Load() != 0 {
				t.Fatalf("HTTP should not be called, hits=%d", hits.Load())
			}
		})
	}
}

func TestWriteCommandsRejectTwoJSONDocuments(t *testing.T) {
	var stdout bytes.Buffer
	body := "{\"kind\":\"bug\",\"title\":\"t\"}\n{\"kind\":\"bug\",\"title\":\"u\"}"
	result := pingcode.Execute(context.Background(), []string{"work-item", "create", "--input", "-"}, pingcode.RuntimeDependencies{
		Stdout: &stdout,
		Stdin:  strings.NewReader(body),
	})
	if result.ExitCode != cli.ExitUsage {
		t.Fatalf("exit=%d out=%s", result.ExitCode, stdout.String())
	}
	if !strings.Contains(stdout.String(), "INVALID_ARGUMENT") {
		t.Fatalf("expected INVALID_ARGUMENT: %s", stdout.String())
	}
}
