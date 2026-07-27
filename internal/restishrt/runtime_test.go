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

	"github.com/0xfe10/aicli/internal/cli"
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
		return rt.Run(context.Background(), args, nil)
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
	_, err := rt.Run(context.Background(), []string{"GET", "/slow"}, nil)
	if err == nil {
		t.Fatal("expected timeout")
	}
}

func TestRawRejectsAbsoluteURL(t *testing.T) {
	rt := &restishrt.Runtime{APIBaseURL: "https://open.pingcode.com"}
	_, err := rt.Run(context.Background(), []string{"GET", "https://evil.example/v1"}, nil)
	if err == nil {
		t.Fatal("expected reject")
	}
}

func TestRawBodyStdin(t *testing.T) {
	var gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		gotBody = string(raw)
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
	}))
	defer srv.Close()
	rt := &restishrt.Runtime{APIBaseURL: srv.URL}
	stdin := strings.NewReader(`{"clientSecret":"should-not-be-in-argv","title":"x"}`)
	_, err := rt.Run(context.Background(), []string{"POST", "/v1/x", "--body-stdin"}, stdin)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(gotBody, "should-not-be-in-argv") {
		t.Fatalf("body not forwarded: %s", gotBody)
	}
}

func TestRawRejectsCrossPortRedirectWithAuth(t *testing.T) {
	var sawAuthOnB bool
	b := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			sawAuthOnB = true
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
	}))
	defer b.Close()

	a := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, b.URL+"/final", http.StatusFound)
	}))
	defer a.Close()

	rt := &restishrt.Runtime{
		APIBaseURL: a.URL,
		Auth:       func(context.Context) (string, error) { return "Bearer redirect-token-secret", nil },
	}
	_, err := rt.Run(context.Background(), []string{"GET", "/start"}, nil)
	if err == nil {
		t.Fatal("expected cross-port redirect refusal")
	}
	if sawAuthOnB {
		t.Fatal("Authorization must not be forwarded across ports")
	}
	if !strings.Contains(err.Error(), "跨") && !strings.Contains(strings.ToLower(err.Error()), "redirect") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRawResponseTruncationMetadata(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Force truncation by returning more than the test would typically send;
		// the runtime caps at 8MiB. Use a smaller override path via oversized body
		// isn't practical here — instead verify meta key exists and is false for small bodies.
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "payload": strings.Repeat("x", 100)})
	}))
	defer srv.Close()
	rt := &restishrt.Runtime{APIBaseURL: srv.URL}
	got, err := rt.Run(context.Background(), []string{"GET", "/small"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	meta := got.Meta.(map[string]any)
	if meta["response_truncated"] != false {
		t.Fatalf("meta=%#v", meta)
	}
	if meta["response_bytes_cap"] == nil {
		t.Fatalf("missing response_bytes_cap: %#v", meta)
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
		Raw: func(ctx context.Context, args []string, stdin io.Reader) (pingcode.RawResult, error) {
			rawResult, err := rt.Run(ctx, args, stdin)
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

func TestRawExecuteRedactsCredentialCorpusEndToEnd(t *testing.T) {
	const (
		bodyToken      = "body-access-token-secret"
		camelSecret    = "camel-client-secret-value"
		authCode       = "authorization-code-secret"
		refreshCamel   = "refreshToken-secret-value"
		bareToken      = "bare-token-secret"
		authEcho       = "Bearer request-auth-secret"
		cookieValue    = "session=cookie-secret-value; Path=/"
		queryInMessage = "token=query-token-secret"
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Set-Cookie", cookieValue)
		w.Header().Set("X-Echo-Authorization", authEcho)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token":      bodyToken,
			"accessToken":       bodyToken,
			"clientSecret":      camelSecret,
			"authorizationCode": authCode,
			"refresh-token":     refreshCamel,
			"token":             bareToken,
			"Authorization":     authEcho,
			"code":              "100009",
			"message":           "failed " + queryInMessage,
			"title":             "safe-title",
			"nested": map[string]any{
				"client_id": "client-id-secret",
				"items":     []any{map[string]any{"secret": "nested-secret-value"}},
			},
		})
	}))
	defer srv.Close()

	rt := &restishrt.Runtime{APIBaseURL: srv.URL}
	var stdout, stderr bytes.Buffer
	result := pingcode.Execute(context.Background(), []string{"raw", "GET", "/secret"}, pingcode.RuntimeDependencies{
		Stdout: &stdout,
		Stderr: &stderr,
		Raw: func(ctx context.Context, args []string, stdin io.Reader) (pingcode.RawResult, error) {
			rawResult, err := rt.Run(ctx, args, stdin)
			return pingcode.RawResult{Data: rawResult.Data, Meta: rawResult.Meta}, err
		},
	})
	if result.ExitCode != 0 {
		t.Fatalf("exit=%d stdout=%s", result.ExitCode, stdout.String())
	}
	combined := stdout.String() + stderr.String()
	for _, secret := range []string{
		bodyToken, camelSecret, authCode, refreshCamel, bareToken,
		"request-auth-secret", "cookie-secret-value", "query-token-secret",
		"client-id-secret", "nested-secret-value",
	} {
		if strings.Contains(combined, secret) {
			t.Fatalf("secret %q leaked: %s", secret, combined)
		}
	}
	var doc map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &doc); err != nil {
		t.Fatal(err)
	}
	data := doc["data"].(map[string]any)
	body := data["body"].(map[string]any)
	if body["access_token"] != "***" || body["accessToken"] != "***" || body["clientSecret"] != "***" {
		t.Fatalf("keys not redacted: %#v", body)
	}
	if body["code"] != "100009" {
		t.Fatalf("business code over-redacted: %#v", body["code"])
	}
	if body["title"] != "safe-title" {
		t.Fatalf("title=%#v", body["title"])
	}
	headers := data["headers"].(map[string]any)
	if headers["Set-Cookie"] != "***" {
		t.Fatalf("Set-Cookie=%#v", headers["Set-Cookie"])
	}
}

func TestRawStatusCodeExitMapping(t *testing.T) {
	cases := []struct {
		status   int
		code     string
		exitCode int
		hang     bool
	}{
		{http.StatusBadRequest, "UPSTREAM_ERROR", cli.ExitUpstream, false},
		{http.StatusUnauthorized, "AUTH_EXPIRED", cli.ExitAuth, false},
		{http.StatusForbidden, "FORBIDDEN", cli.ExitForbidden, false},
		{http.StatusNotFound, "NOT_FOUND", cli.ExitNotFound, false},
		{http.StatusTooManyRequests, "RATE_LIMITED", cli.ExitRateLimited, false},
		{0, "UPSTREAM_TIMEOUT", cli.ExitUpstreamTimeout, true},
	}
	for _, tc := range cases {
		t.Run(tc.code, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if tc.hang {
					time.Sleep(50 * time.Millisecond)
					w.WriteHeader(http.StatusOK)
					return
				}
				w.WriteHeader(tc.status)
				_, _ = io.WriteString(w, `{"message":"x"}`)
			}))
			defer srv.Close()

			rt := &restishrt.Runtime{
				APIBaseURL: srv.URL,
				HTTP:       &http.Client{Timeout: time.Millisecond},
			}
			if !tc.hang {
				rt.HTTP = srv.Client()
			}
			var stdout bytes.Buffer
			result := pingcode.Execute(context.Background(), []string{"raw", "GET", "/x"}, pingcode.RuntimeDependencies{
				Stdout: &stdout,
				Raw: func(ctx context.Context, args []string, stdin io.Reader) (pingcode.RawResult, error) {
					rawResult, err := rt.Run(ctx, args, stdin)
					return pingcode.RawResult{Data: rawResult.Data, Meta: rawResult.Meta}, err
				},
			})
			if result.ExitCode != tc.exitCode {
				t.Fatalf("exit=%d want=%d stdout=%s", result.ExitCode, tc.exitCode, stdout.String())
			}
			var doc map[string]any
			dec := json.NewDecoder(bytes.NewReader(stdout.Bytes()))
			if err := dec.Decode(&doc); err != nil {
				t.Fatalf("decode: %v", err)
			}
			var extra json.RawMessage
			if err := dec.Decode(&extra); err != io.EOF {
				t.Fatalf("expected one JSON document: %s", stdout.String())
			}
			errBody, _ := doc["error"].(map[string]any)
			if errBody["code"] != tc.code {
				t.Fatalf("code=%v want=%s doc=%#v", errBody["code"], tc.code, doc)
			}
		})
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

func TestWorkItemCreateHelpIgnoresInvalidConfig(t *testing.T) {
	t.Setenv("PINGCODE_API_BASE_URL", "not-a-url")
	t.Setenv("PINGCODE_BASE_URL", "also-bad")
	t.Setenv("PINGCODE_CLIENT_ID", "")
	t.Setenv("PINGCODE_CLIENT_SECRET", "")
	var stdout bytes.Buffer
	result := pingcode.Execute(context.Background(), []string{"work-item", "create", "--help"}, pingcode.RuntimeDependencies{
		Stdout: &stdout,
	})
	if result.ExitCode != 0 {
		t.Fatalf("exit=%d stdout=%s", result.ExitCode, stdout.String())
	}
	if !strings.Contains(stdout.String(), "stdin JSON fields") {
		t.Fatalf("expected help text, got %s", stdout.String())
	}
	if strings.Contains(stdout.String(), "CONFIG_MISSING") {
		t.Fatalf("help depended on config: %s", stdout.String())
	}
}
