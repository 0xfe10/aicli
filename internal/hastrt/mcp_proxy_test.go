package hastrt

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestMCPProxyPolicyAndOrigin(t *testing.T) {
	var hits atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if r.URL.Path != "/api/public/v1/mcp" || r.Header.Get("Authorization") != "Bearer private-test-key" {
			t.Error("incorrect credential origin")
		}
		if r.Header.Get("Mcp-Session-Id") != "mock-session" {
			t.Error("session header dropped")
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"jsonrpc":"2.0","id":1,"result":{"structuredContent":{"status":200,"body":{}}}}`)
	}))
	defer upstream.Close()
	for _, mode := range []string{"readonly", "write", "destructive"} {
		p, err := NewMCPProxy(Session{BaseURL: upstream.URL, Credentials: Credentials{APIKey: "private-test-key"}}, mode)
		if err != nil {
			t.Fatal(err)
		}
		for _, tc := range []struct {
			name    string
			allowed bool
		}{{"get_models", true}, {"post_webhooks", mode != "readonly"}, {"delete_webhooks_hook_id", mode == "destructive"}, {"invoke_unknown", false}} {
			before := hits.Load()
			raw, _ := json.Marshal(map[string]interface{}{"jsonrpc": "2.0", "id": 1, "method": "tools/call", "params": map[string]interface{}{"name": tc.name}})
			request, _ := http.NewRequest("POST", p.Endpoint, bytes.NewReader(raw))
			request.Header.Set("Mcp-Session-Id", "mock-session")
			response, err := http.DefaultClient.Do(request)
			if err != nil {
				t.Fatal(err)
			}
			io.Copy(io.Discard, response.Body)
			response.Body.Close()
			if (hits.Load() > before) != tc.allowed {
				t.Fatalf("%s %s gate failed", mode, tc.name)
			}
		}
		for _, raw := range []string{`[{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"post_webhooks"}}]`, `{"bad":true}`} {
			before := hits.Load()
			r, err := http.Post(p.Endpoint, "application/json", strings.NewReader(raw))
			if err != nil {
				t.Fatal(err)
			}
			r.Body.Close()
			if r.StatusCode != 400 || hits.Load() != before {
				t.Fatal("invalid/batch forwarded")
			}
		}
		root := p.Endpoint[:strings.LastIndex(p.Endpoint, "/")]
		response, err := http.Post(root+"/other", "application/json", strings.NewReader(`{}`))
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		if response.StatusCode != 404 {
			t.Fatal("unguarded proxy path")
		}
		p.Close()
	}
}

func TestMCPProxyNoWriteRedirect(t *testing.T) {
	var calls atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1); w.WriteHeader(200) }))
	defer target.Close()
	for _, status := range []int{307, 308} {
		upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Location", target.URL)
			w.WriteHeader(status)
		}))
		p, err := NewMCPProxy(Session{BaseURL: upstream.URL, Credentials: Credentials{APIKey: "test"}}, "write")
		if err != nil {
			t.Fatal(err)
		}
		response, err := http.Post(p.Endpoint, "application/json", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"post_webhooks"}}`))
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		if calls.Load() != 0 || p.Err() == nil {
			t.Fatal("redirect replayed or treated as success")
		}
		p.Close()
		upstream.Close()
	}
}

func TestMCPProxyProtocolErrors(t *testing.T) {
	for _, stream := range []bool{false, true} {
		upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			data := `{"jsonrpc":"2.0","id":1,"error":{"code":-32602,"message":"invalid parameters"}}`
			if stream {
				w.Header().Set("Content-Type", "text/event-stream")
				fmt.Fprint(w, "event: message\ndata: "+data+"\n\n")
			} else {
				w.Header().Set("Content-Type", "application/json")
				fmt.Fprint(w, data)
			}
		}))
		p, err := NewMCPProxy(Session{BaseURL: upstream.URL, Credentials: Credentials{APIKey: "test"}}, "write")
		if err != nil {
			t.Fatal(err)
		}
		response, err := http.Post(p.Endpoint, "application/json", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"post_webhooks"}}`))
		if err != nil {
			t.Fatal(err)
		}
		io.Copy(io.Discard, response.Body)
		response.Body.Close()
		if p.Err() == nil {
			t.Fatal("JSON-RPC error treated as success")
		}
		p.Close()
		upstream.Close()
	}
}
