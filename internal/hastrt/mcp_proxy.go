package hastrt

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"time"
)

// MCPProxy owns authentication and write policy for every runtime RPC request.
type MCPProxy struct {
	Endpoint                string
	server                  *httptest.Server
	target, key, mode, path string
	client                  *http.Client
	mu                      sync.Mutex
	failure                 error
}

func NewMCPProxy(session Session, mode string) (*MCPProxy, error) {
	if err := validateOrigin(session.BaseURL); err != nil {
		return nil, err
	}
	if err := RejectPlaceholderBaseURL(session.BaseURL); err != nil {
		return nil, err
	}
	if session.Credentials.APIKey == "" {
		return nil, fmt.Errorf("Hast API key is required")
	}
	token := make([]byte, 32)
	if _, err := rand.Read(token); err != nil {
		return nil, err
	}
	p := &MCPProxy{target: strings.TrimRight(session.BaseURL, "/") + "/api/public/v1/mcp", key: session.Credentials.APIKey, mode: mode, path: "/" + hex.EncodeToString(token), client: &http.Client{Timeout: 120 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
	if mode != "readonly" && mode != "write" && mode != "destructive" {
		return nil, fmt.Errorf("invalid HAST_WRITE_MODE %q", mode)
	}
	p.server = httptest.NewServer(http.HandlerFunc(p.serveHTTP))
	p.Endpoint = p.server.URL + p.path
	return p, nil
}
func (p *MCPProxy) Close() error { p.server.Close(); p.client.CloseIdleConnections(); return nil }
func (p *MCPProxy) Err() error   { p.mu.Lock(); defer p.mu.Unlock(); return p.failure }
func (p *MCPProxy) fail(err error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.failure == nil {
		p.failure = err
	}
}

func (p *MCPProxy) serveHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != p.path || r.URL.RawQuery != "" {
		http.NotFound(w, r)
		return
	}
	// Runtime requests are POST; no event subscription or session mutation endpoint.
	if r.Method != "POST" {
		http.Error(w, "MCP requires POST", 405)
		return
	}
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "invalid request", 400)
		return
	}
	var rpc struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      json.RawMessage `json:"id"`
		Method  string          `json:"method"`
		Params  struct {
			Name string `json:"name"`
		} `json:"params"`
	}
	if err = json.Unmarshal(raw, &rpc); err != nil || rpc.JSONRPC != "2.0" || rpc.Method == "" {
		http.Error(w, "invalid JSON-RPC object; batch requests are unsupported", 400)
		return
	}
	if err = p.check(rpc.Method, rpc.Params.Name); err != nil {
		p.fail(err)
		w.Header().Set("Content-Type", "application/json")
		id := interface{}(nil)
		if len(rpc.ID) > 0 {
			_ = json.Unmarshal(rpc.ID, &id)
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"jsonrpc": "2.0", "id": id, "error": map[string]interface{}{"code": -32000, "message": err.Error()}})
		return
	}
	upstream, err := http.NewRequestWithContext(r.Context(), "POST", p.target, bytes.NewReader(raw))
	if err != nil {
		http.Error(w, "invalid upstream", 500)
		return
	}
	// Prevent http.Transport from recreating a request after a connection failure.
	upstream.GetBody = nil
	for _, name := range []string{"Content-Type", "Accept", "MCP-Protocol-Version", "Mcp-Session-Id", "Mcp-Method", "Mcp-Name"} {
		if value := r.Header.Get(name); value != "" {
			upstream.Header.Set(name, value)
		}
	}
	upstream.Header.Set("Authorization", "Bearer "+p.key)
	response, err := p.client.Do(upstream)
	if err != nil {
		if rpc.Method == "tools/call" {
			p.fail(fmt.Errorf("MCP call outcome is uncertain: %w", err))
		}
		http.Error(w, "Hast MCP upstream request failed", 502)
		return
	}
	defer response.Body.Close()
	data, err := io.ReadAll(response.Body)
	if err != nil {
		if rpc.Method == "tools/call" {
			p.fail(fmt.Errorf("MCP response incomplete; outcome is uncertain"))
		}
		http.Error(w, "incomplete upstream response", 502)
		return
	}
	if rpc.Method == "tools/call" {
		if response.StatusCode < 200 || response.StatusCode >= 300 {
			p.fail(fmt.Errorf("MCP tool %s returned HTTP %d; inspect current state before retrying", rpc.Params.Name, response.StatusCode))
		}
		if strings.Contains(response.Header.Get("Content-Type"), "text/event-stream") {
			for _, line := range bytes.Split(data, []byte("\n")) {
				if bytes.HasPrefix(line, []byte("data:")) && mcpResultFailed(bytes.TrimSpace(line[5:])) {
					p.fail(fmt.Errorf("MCP tool %s returned an error", rpc.Params.Name))
				}
			}
		} else if mcpResultFailed(data) {
			p.fail(fmt.Errorf("MCP tool %s returned an error", rpc.Params.Name))
		}
	}
	for _, name := range []string{"Content-Type", "Mcp-Session-Id", "MCP-Protocol-Version", "Retry-After"} {
		if value := response.Header.Get(name); value != "" {
			w.Header().Set(name, value)
		}
	}
	// Do not forward Location: the runtime must never repeat a redirected write.
	w.WriteHeader(response.StatusCode)
	_, _ = w.Write(data)
}
func (p *MCPProxy) check(method, name string) error {
	switch method {
	case "initialize", "notifications/initialized", "ping", "server/discover", "tools/list", "resources/list", "resources/templates/list", "resources/read", "prompts/list", "prompts/get":
		return nil
	case "tools/call":
		lower := strings.ToLower(name)
		if strings.HasPrefix(lower, "get_") {
			return nil
		}
		destructive := strings.HasPrefix(lower, "delete_")
		for _, part := range strings.Split(lower, "_") {
			if part == "delete" || part == "purge" || part == "clear" || part == "revoke" {
				destructive = true
			}
		}
		if destructive {
			if p.mode == "destructive" {
				return nil
			}
			return fmt.Errorf("HAST MCP tool %s is blocked by HAST_WRITE_MODE=%s", name, p.mode)
		}
		if strings.HasPrefix(lower, "post_") || strings.HasPrefix(lower, "put_") || strings.HasPrefix(lower, "patch_") {
			if p.mode == "write" || p.mode == "destructive" {
				return nil
			}
			return fmt.Errorf("HAST MCP tool %s is blocked by HAST_WRITE_MODE=%s", name, p.mode)
		}
		return fmt.Errorf("unknown Hast MCP tool %q: cannot determine write safety", name)
	default:
		return fmt.Errorf("unsupported MCP method %q", method)
	}
}
