package hastrt

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

func TestMCPArgumentsAndErrorOutput(t *testing.T) {
	for _, flag := range []string{"--url=https://other.test/mcp", "--stdio", "--config", "--profile=other"} {
		if _, _, err := mcpArguments([]string{"hast", flag}); err == nil {
			t.Fatalf("override accepted: %s", flag)
		}
	}
	if !mcpResultFailed([]byte(`{"jsonrpc":"2.0","id":1,"error":{"code":-32602,"message":"invalid parameters"}}`)) {
		t.Fatal("JSON-RPC error treated as success")
	}
	if mcpResultFailed([]byte(`{"body":{"error":"business data"}}`)) {
		t.Fatal("business field treated as protocol error")
	}
	raw := []byte(`{"data":{"result":{"isError":true,"structuredContent":{"status":404,"body":{"error":"missing"}}}}}`)
	if !mcpResultFailed(raw) {
		t.Fatal("tool error treated as success")
	}
	var output bytes.Buffer
	if err := renderMCPOutput(&output, raw, "ndjson"); err != nil {
		t.Fatal(err)
	}
	if strings.Count(output.String(), "\n") != 1 || !json.Valid(bytes.TrimSpace(output.Bytes())) {
		t.Fatal("invalid ndjson")
	}
	output.Reset()
	if err := renderMCPOutput(&output, []byte(`{"data":{"help":"Command help\n"}}`), "human"); err != nil {
		t.Fatal(err)
	}
	if output.String() != "Command help\n" {
		t.Fatal("help was not rendered")
	}
}

func TestMCPRuntimeVersionWithoutCredentials(t *testing.T) {
	var output bytes.Buffer
	if err := runMCP([]string{"hast", "--version"}, ContextManager().DefaultSelection(), "0.10.0", "abc", &output, &output); err != nil {
		t.Fatal(err)
	}
	if output.String() != "hast version 0.10.0 (abc)\n" {
		t.Fatal(output.String())
	}
}

// The pinned release runtime is opt-in; regular CI tests the proxy independently.
func TestNativeMCPRuntime(t *testing.T) {
	runtime := os.Getenv("HAST_TEST_MCP2CLI")
	if runtime == "" {
		t.Skip("set HAST_TEST_MCP2CLI to the pinned runtime")
	}
	t.Setenv("HAST_MCP2CLI", runtime)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	t.Setenv("HAST_API_KEY", "test-hast-mcp-key")
	t.Setenv("HAST_WRITE_MODE", "readonly")
	var writes atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-hast-mcp-key" {
			t.Error("missing upstream key")
		}
		var request map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
			return
		}
		method, _ := request["method"].(string)
		if strings.HasPrefix(method, "notifications/") {
			w.WriteHeader(202)
			return
		}
		result := interface{}(map[string]interface{}{})
		switch method {
		case "server/discover":
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%v,"error":{"code":-32601,"message":"method not found"}}`, request["id"])
			return
		case "initialize":
			result = map[string]interface{}{"protocolVersion": "2025-11-25", "capabilities": map[string]interface{}{"tools": map[string]interface{}{}}, "serverInfo": map[string]interface{}{"name": "mock-hast", "version": "1"}}
		case "tools/list":
			tools := []interface{}{}
			for _, name := range []string{"get_models", "get_webhooks_hook_id", "post_webhooks", "delete_webhooks_hook_id"} {
				tools = append(tools, map[string]interface{}{"name": name, "inputSchema": map[string]interface{}{"type": "object", "properties": map[string]interface{}{"body": map[string]interface{}{"type": "object"}, "path": map[string]interface{}{"type": "object"}}}})
			}
			result = map[string]interface{}{"tools": tools}
		case "tools/call":
			params := request["params"].(map[string]interface{})
			name := params["name"].(string)
			body := map[string]interface{}{"ok": true}
			status := 200
			isError := false
			if name == "get_webhooks_hook_id" {
				status = 404
				isError = true
				body = map[string]interface{}{"error": "not found"}
			}
			if name == "post_webhooks" {
				writes.Add(1)
				args := params["arguments"].(map[string]interface{})
				b, ok := args["body"].(map[string]interface{})
				if !ok || b["title"] != "degov" {
					t.Error("JSON body lost")
				}
			}
			if name == "delete_webhooks_hook_id" {
				writes.Add(1)
			}
			structured := map[string]interface{}{"status": status, "body": body}
			text, _ := json.Marshal(structured)
			result = map[string]interface{}{"isError": isError, "structuredContent": structured, "content": []interface{}{map[string]interface{}{"type": "text", "text": string(text)}}}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"jsonrpc": "2.0", "id": request["id"], "result": result})
	}))
	defer server.Close()
	t.Setenv("HAST_BASE_URL", server.URL)
	selection := ContextManager().DefaultSelection()
	if err := ContextManager().Prepare(selection); err != nil {
		t.Fatal(err)
	}
	run := func(args ...string) error {
		var output bytes.Buffer
		err := runMCP(append([]string{"hast"}, args...), selection, "test", "test", &output, &output)
		if strings.Contains(output.String(), "test-hast-mcp-key") {
			t.Fatal("key leaked")
		}
		if err != nil {
			t.Log(err, output.String())
		}
		return err
	}
	if err := run("get_models", "--json"); err != nil {
		t.Fatal(err)
	}
	if err := run("get_webhooks_hook_id", "--args-json", `{"path":{"hook_id":"missing"}}`); err == nil {
		t.Fatal("404 succeeded")
	}
	file := filepath.Join(t.TempDir(), "agent.json")
	if err := os.WriteFile(file, []byte(`{"body":{"title":"degov"}}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := run("post_webhooks", "--args-file", file); err == nil {
		t.Fatal("readonly write allowed")
	}
	if writes.Load() != 0 {
		t.Fatal("blocked write reached upstream")
	}
	t.Setenv("HAST_WRITE_MODE", "write")
	if err := run("post_webhooks", "--args-file", file, "--json"); err != nil {
		t.Fatal(err)
	}
	if err := run("tool", "call", "delete_webhooks_hook_id", "--args-json", `{"path":{"hook_id":"test"}}`); err == nil {
		t.Fatal("generic delete gate bypass")
	}
	if writes.Load() != 1 {
		t.Fatal("unexpected replay")
	}
	t.Setenv("HAST_WRITE_MODE", "destructive")
	if err := run("tool", "call", "delete_webhooks_hook_id", "--args-json", `{"path":{"hook_id":"test"}}`); err != nil {
		t.Fatal(err)
	}
	if writes.Load() != 2 {
		t.Fatal("delete missing/replayed")
	}
}

func TestMCPRuntimeKeepsCredentialsOutOfChildEnvironment(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	t.Setenv("HAST_API_KEY", "private-test-key")
	t.Setenv("hast_api_key", "alternate-private-key")
	t.Setenv("HAST_BASE_URL", "http://127.0.0.1:12345")
	t.Setenv("HAST_WRITE_MODE", "readonly")
	t.Setenv("MCP2CLI_SERVER__ENDPOINT", "https://wrong.test/mcp")
	script := filepath.Join(t.TempDir(), "runtime")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nif [ -n \"$HAST_API_KEY\" ] || [ -n \"$hast_api_key\" ] || [ -n \"$MCP2CLI_SERVER__ENDPOINT\" ]; then exit 7; fi\nprintf '%s\\n' '{\"data\":{\"result\":{\"structuredContent\":{\"status\":200,\"body\":{}}}}}'\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HAST_MCP2CLI", script)
	selection := ContextManager().DefaultSelection()
	if err := ContextManager().Prepare(selection); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := runMCP([]string{"hast", "get_models"}, selection, "test", "test", &output, &output); err != nil {
		t.Fatal(err)
	}
}
