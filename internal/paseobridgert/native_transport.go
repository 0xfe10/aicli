package paseobridgert

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf16"
)

func readBounded(r io.Reader, limit int) ([]byte, error) {
	raw, err := io.ReadAll(io.LimitReader(r, int64(limit)+1))
	if err != nil || len(raw) > limit {
		return nil, nativeFailure
	}
	return raw, nil
}
func nativeFetchedAt() string { return time.Now().UTC().Format(time.RFC3339Nano) }
func nativeJSONResponse(r *http.Request, status int, value any) (*http.Response, error) {
	raw, err := json.Marshal(value)
	if err != nil || len(raw) > maxEncryptedResponse {
		return nil, nativeFailure
	}
	return &http.Response{StatusCode: status, Status: fmt.Sprintf("%d %s", status, http.StatusText(status)), Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(bytes.NewReader(raw)), ContentLength: int64(len(raw)), Request: r}, nil
}
func nativeSuccess(r *http.Request, status int, data any) (*http.Response, error) {
	return nativeJSONResponse(r, status, map[string]any{"ok": true, "data": data})
}
func (t *encryptedTransport) nativeOne(r *http.Request, host, op string) (*http.Response, error) {
	if r.Method != "GET" && r.Method != "POST" {
		return nil, fmt.Errorf("unsupported native Relay method")
	}
	if (op == "send") != (r.Method == "POST") {
		return nil, fmt.Errorf("unsupported native Relay method for operation")
	}
	if !hostName.MatchString(host) || host == "all" {
		return nil, fmt.Errorf("explicit pinned native Relay host required")
	}
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	agent := ""
	if len(parts) >= 6 {
		agent = parts[5]
		if !nativeAgentID.MatchString(agent) {
			return nil, fmt.Errorf("invalid native agent target")
		}
	}
	ctx, cancel := context.WithTimeout(r.Context(), 150*time.Second)
	streaming := false
	defer func() {
		if !streaming {
			cancel()
		}
	}()
	channel, err := t.nativeConnect(ctx, host, r.Header.Get("Authorization"))
	if err != nil {
		return nil, err
	}
	defer func() {
		if !streaming {
			channel.close()
		}
	}()
	base := map[string]any{"host": host, "fetchedAt": nativeFetchedAt()}
	if op == "status" {
		base["state"] = "connected"
		return nativeSuccess(r, 200, map[string]any{"results": []any{base}, "partial": false})
	}
	if op == "agents" || op == "workspaces" {
		entries, err := channel.directory(op)
		if err != nil {
			return nil, err
		}
		base["entries"] = entries
		return nativeSuccess(r, 200, map[string]any{"results": []any{base}, "partial": false})
	}
	if op == "send" {
		return t.nativeSend(r, channel, host, agent)
	}
	snapshot, err := channel.snapshot(host, agent)
	if err != nil {
		return nil, err
	}
	if strings.HasSuffix(r.URL.Path, "/events") {
		include := r.URL.Query().Get("include")
		if include != "" && include != "state" && include != "activity" {
			return nil, fmt.Errorf("invalid native observation include")
		}
		if cursor := r.Header.Get("Last-Event-ID"); cursor != "" && !nativeCursor.MatchString(cursor) {
			return nil, fmt.Errorf("invalid native event cursor")
		}
		streaming = true
		return channel.stream(r, cancel, host, agent, snapshot, include == "activity"), nil
	}
	if op == "wait" {
		return nativeSuccess(r, 200, snapshot)
	}
	if op == "activity" {
		tail := 20
		if raw := r.URL.Query().Get("tail"); raw != "" {
			tail, err = strconv.Atoi(raw)
			if err != nil || tail < 0 || tail > 200 {
				return nil, fmt.Errorf("invalid native activity tail")
			}
		}
		data, err := channel.activity(host, agent, tail)
		if err != nil {
			return nil, err
		}
		return nativeSuccess(r, 200, data)
	}
	return nil, fmt.Errorf("unsupported native Relay operation")
}

func (t *encryptedTransport) nativeSend(r *http.Request, channel *nativeChannel, host, agent string) (*http.Response, error) {
	if r.Body == nil {
		return nil, fmt.Errorf("native send body is required")
	}
	raw, err := readBounded(r.Body, 262144)
	if r.Body != nil {
		r.Body.Close()
	}
	if err != nil {
		return nil, err
	}
	var request struct {
		Prompt    string `json:"prompt"`
		Interrupt bool   `json:"interrupt"`
	}
	if strictJSON(raw, &request) != nil || strings.TrimSpace(request.Prompt) == "" || len(utf16.Encode([]rune(request.Prompt))) > 65536 {
		return nil, fmt.Errorf("invalid native send body")
	}
	messageID := nativeUUID()
	if messageID == "" {
		return nil, nativeFailure
	}
	guard, err := nativePrepareGuard(t.keys.native.StateDir, host, agent, messageID)
	if err != nil {
		return nil, err
	}
	submitted := false
	defer func() {
		if !submitted {
			_ = nativeClearGuard(guard, host, agent, messageID)
		}
	}()
	snapshot, err := channel.snapshot(host, agent)
	if err != nil {
		return nil, err
	}
	current := objectField(snapshot, "agent")
	if stringField(current, "status") != "idle" && !request.Interrupt {
		return nativeJSONResponse(r, 409, map[string]any{"ok": false, "error": map[string]string{"code": "BUSY", "message": "Agent is not idle; explicit interrupt required"}})
	}
	behavior := "steer"
	if request.Interrupt {
		behavior = "interrupt"
	}
	submitted = true
	result, err := channel.rpc("send_agent_message_request", "send_agent_message_response", map[string]any{"agentId": agent, "text": request.Prompt, "messageId": messageID, "activeTurnBehavior": behavior})
	accepted, _ := result["accepted"].(bool)
	// An RPC rejection, timeout, malformed receipt, or lost durable clear remains
	// ambiguous. Preserve the guard across process restarts and never resend.
	if err != nil || !accepted || stringField(result, "agentId") != agent || nativeClearGuard(guard, host, agent, messageID) != nil {
		return nativeSuccess(r, 200, map[string]any{"host": host, "agentId": agent, "requestId": messageID, "status": "unknown", "retrySafe": false})
	}
	return nativeSuccess(r, 202, map[string]any{"host": host, "agentId": agent, "requestId": messageID, "status": "accepted"})
}
func (c *nativeChannel) directory(op string) ([]any, error) {
	entries := []any{}
	cursor := ""
	seen := map[string]bool{}
	for page := 0; page < 1000; page++ {
		pageFields := map[string]any{"limit": 200}
		if cursor != "" {
			pageFields["cursor"] = cursor
		}
		payload, err := c.rpc("fetch_"+op+"_request", "fetch_"+op+"_response", map[string]any{"page": pageFields})
		if err != nil {
			return nil, err
		}
		batch, ok := payload["entries"].([]any)
		if !ok {
			return nil, nativeFailure
		}
		for _, value := range batch {
			entry, ok := value.(map[string]any)
			if !ok {
				return nil, nativeFailure
			}
			if op == "agents" {
				entry = objectField(entry, "agent")
				if stringField(entry, "id") == "" {
					return nil, nativeFailure
				}
			} else {
				if stringField(entry, "id") == "" {
					return nil, nativeFailure
				}
				normalizeNativeWorkspace(entry)
			}
			entries = append(entries, entry)
		}
		if len(entries) > 10000 {
			return nil, nativeFailure
		}
		pageInfo := objectField(payload, "pageInfo")
		if pageInfo == nil {
			return nil, nativeFailure
		}
		cursor = stringField(pageInfo, "nextCursor")
		if cursor == "" {
			return entries, nil
		}
		if seen[cursor] {
			return nil, nativeFailure
		}
		seen[cursor] = true
	}
	return nil, nativeFailure
}
func normalizeNativeWorkspace(workspace map[string]any) {
	if stringField(workspace, "workspaceDirectory") == "" {
		if cwd := stringField(workspace, "projectRootPath"); cwd != "" {
			workspace["workspaceDirectory"] = cwd
		}
	}
	if _, ok := workspace["archivingAt"]; !ok {
		workspace["archivingAt"] = nil
	}
	if _, ok := workspace["scripts"]; !ok {
		workspace["scripts"] = []any{}
	}
	if _, ok := workspace["statusEnteredAt"]; !ok {
		workspace["statusEnteredAt"] = nil
	}
}
func (c *nativeChannel) snapshot(host, agent string) (map[string]any, error) {
	payload, err := c.rpc("fetch_agent_request", "fetch_agent_response", map[string]any{"agentId": agent})
	if err != nil {
		return nil, err
	}
	value := objectField(payload, "agent")
	if stringField(value, "id") != agent || value["archivedAt"] != nil {
		return nil, nativeFailure
	}
	snapshot := map[string]any{"host": host, "agentId": agent, "fetchedAt": nativeFetchedAt(), "agent": value}
	status := stringField(value, "status")
	permissions, _ := value["pendingPermissions"].([]any)
	if status == "idle" || status == "error" || len(permissions) > 0 {
		result, e := c.rpc("wait_for_finish_request", "wait_for_finish_response", map[string]any{"agentId": agent, "timeoutMs": 1000})
		if e != nil {
			return nil, e
		}
		final := objectField(result, "final")
		if final != nil && stringField(final, "id") != agent {
			return nil, nativeFailure
		}
		resultStatus := stringField(result, "status")
		if resultStatus != "idle" && resultStatus != "error" && resultStatus != "permission" && resultStatus != "timeout" {
			return nil, nativeFailure
		}
		if resultStatus != "timeout" {
			delete(result, "requestId")
			result["lastMessage"] = nil
			if result["error"] != nil && result["error"] != "" {
				result["error"] = "Agent reported an error"
			}
			snapshot["result"] = result
		}
	}
	return snapshot, nil
}
func (c *nativeChannel) activity(host, agent string, tail int) (map[string]any, error) {
	if tail == 0 {
		return map[string]any{"host": host, "agentId": agent, "entries": []any{}}, nil
	}
	payload, err := c.rpc("fetch_agent_timeline_request", "fetch_agent_timeline_response", map[string]any{"agentId": agent, "direction": "before", "limit": tail})
	if err != nil {
		return nil, err
	}
	if stringField(payload, "agentId") != agent {
		return nil, nativeFailure
	}
	if value := objectField(payload, "agent"); value != nil && stringField(value, "id") != agent {
		return nil, nativeFailure
	}
	return map[string]any{"host": host, "agentId": agent, "fetchedAt": nativeFetchedAt(), "timeline": payload}, nil
}
