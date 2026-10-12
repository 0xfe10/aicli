package paseobridgert

import (
	"bytes"
	"crypto/rand"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"golang.org/x/crypto/nacl/box"
	"golang.org/x/net/websocket"
)

type nativeFixture struct {
	server         *httptest.Server
	transport      *encryptedTransport
	sends          atomic.Int32
	mode           atomic.Value
	directoryCalls atomic.Int32
	passwordSeen   atomic.Bool
}

func newNativeFixture(t *testing.T) *nativeFixture {
	t.Helper()
	public, secret, err := box.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	publicB64 := base64.StdEncoding.EncodeToString(public[:])
	f := &nativeFixture{}
	f.mode.Store("")
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/native/hosts", func(w http.ResponseWriter, r *http.Request) {
		f.directoryCalls.Add(1)
		if r.Header.Get("Authorization") != "Bearer owner" || r.Header.Get("User-Agent") != "paseo-bridge/native-relay" {
			w.WriteHeader(403)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		route := map[string]string{"serverId": "daemon-1"}
		if f.mode.Load().(string) == "directory-public-key" {
			route["daemonPublicKeyB64"] = publicB64
		}
		json.NewEncoder(w).Encode(map[string]any{"ok": true, "data": map[string]any{"hosts": map[string]any{"test": route}}})
	})
	mux.Handle("/api/v1/native/connect/test", websocket.Handler(func(ws *websocket.Conn) {
		defer ws.Close()
		if ws.Request().TLS.NegotiatedProtocol != "http/1.1" {
			t.Error("native WebSocket must negotiate HTTP/1.1")
			return
		}
		if ws.Request().Header.Get("User-Agent") != "paseo-bridge/native-relay" || ws.Request().Header.Get("Authorization") != "Bearer owner" || ws.Request().Header.Get("X-Paseo-Password") != "" || ws.Request().URL.Query().Get("role") != "client" || ws.Request().URL.Query().Get("serverId") != "daemon-1" {
			t.Error("invalid upgrade authentication/routing")
			return
		}
		var first nativeFrame
		if nativeCodec.Receive(ws, &first) != nil {
			return
		}
		var hello struct {
			Type string `json:"type"`
			Key  string `json:"key"`
		}
		if json.Unmarshal(first.data, &hello) != nil || first.binary || hello.Type != "e2ee_hello" {
			t.Error("incorrect handshake")
			return
		}
		raw, err := base64.StdEncoding.DecodeString(hello.Key)
		if err != nil || len(raw) != 32 {
			return
		}
		var peer [32]byte
		copy(peer[:], raw)
		var shared [32]byte
		box.Precompute(&shared, &peer, secret)
		write := func(value any) bool {
			raw, _ := json.Marshal(value)
			var nonce [24]byte
			rand.Read(nonce[:])
			wire := append([]byte(nil), nonce[:]...)
			wire = box.SealAfterPrecomputation(wire, raw, &nonce, &shared)
			return nativeCodec.Send(ws, nativeFrame{data: []byte(base64.StdEncoding.EncodeToString(wire))}) == nil
		}
		read := func() (map[string]any, bool) {
			var frame nativeFrame
			if nativeCodec.Receive(ws, &frame) != nil {
				return nil, false
			}
			if bytes.Contains(frame.data, []byte("SYNTHETIC-PROMPT")) || bytes.Contains(frame.data, []byte("synthetic-password")) {
				t.Error("plaintext application secret on wire")
			}
			wire := frame.data
			if !frame.binary {
				wire, err = base64.StdEncoding.DecodeString(string(frame.data))
				if err != nil {
					return nil, false
				}
			}
			if len(wire) < 40 {
				return nil, false
			}
			var nonce [24]byte
			copy(nonce[:], wire[:24])
			plain, ok := box.OpenAfterPrecomputation(nil, wire[24:], &nonce, &shared)
			if !ok {
				return nil, false
			}
			var value map[string]any
			if json.Unmarshal(plain, &value) != nil {
				return nil, false
			}
			return value, true
		}
		ready := []byte(`{"type":"e2ee_ready","capabilities":{"binaryCiphertext":true}}`)
		if nativeCodec.Send(ws, nativeFrame{data: ready}) != nil {
			return
		}
		helloApp, ok := read()
		if !ok {
			return
		}
		if stringField(helloApp, "type") != "hello" || objectField(helloApp, "capabilities")["all_providers"] != true {
			t.Error("missing encrypted hello or all-provider support")
			return
		}
		if stringField(objectField(helloApp, "auth"), "password") == "synthetic-password" {
			f.passwordSeen.Store(true)
		}
		serverID := "daemon-1"
		if f.mode.Load().(string) == "wrong-server" {
			serverID = "other"
		}
		if !write(map[string]any{"type": "session", "message": map[string]any{"type": "status", "payload": map[string]any{"status": "server_info", "serverId": serverID}}}) {
			return
		}
		for {
			outer, ok := read()
			if !ok {
				return
			}
			request := objectField(outer, "message")
			id := stringField(request, "requestId")
			responseType := ""
			payload := map[string]any{"requestId": id}
			switch stringField(request, "type") {
			case "fetch_agents_request":
				responseType = "fetch_agents_response"
				payload["entries"] = []any{map[string]any{"agent": map[string]any{"id": "agent", "status": "idle", "pendingPermissions": []any{}}}}
				payload["pageInfo"] = map[string]any{"nextCursor": nil, "prevCursor": nil, "hasMore": false}
			case "fetch_workspaces_request":
				responseType = "fetch_workspaces_response"
				payload["entries"] = []any{map[string]any{"id": "workspace", "workspaceDirectory": "/synthetic"}}
				payload["pageInfo"] = map[string]any{"nextCursor": nil, "prevCursor": nil, "hasMore": false}
			case "fetch_agent_request":
				responseType = "fetch_agent_response"
				payload["agent"] = map[string]any{"id": stringField(request, "agentId"), "status": "idle", "pendingPermissions": []any{}}
			case "fetch_agent_timeline_request":
				responseType = "fetch_agent_timeline_response"
				payload["agentId"] = stringField(request, "agentId")
				payload["agent"] = nil
				payload["entries"] = []any{}
				payload["epoch"] = "synthetic"
				payload["gap"] = false
				payload["error"] = nil
			case "wait_for_finish_request":
				responseType = "wait_for_finish_response"
				payload["status"] = "idle"
				payload["final"] = map[string]any{"id": stringField(request, "agentId"), "status": "idle"}
				payload["error"] = nil
				payload["lastMessage"] = "SYNTHETIC-PRIVATE-TEXT"
			case "send_agent_message_request":
				f.sends.Add(1)
				if stringField(request, "text") != "SYNTHETIC-PROMPT" || !nativeID.MatchString(stringField(request, "messageId")) {
					t.Error("missing native durable messageId")
				}
				if f.mode.Load().(string) == "drop-send" {
					return
				}
				responseType = "send_agent_message_response"
				payload["accepted"] = true
				payload["agentId"] = stringField(request, "agentId")
				if f.mode.Load().(string) == "wrong-send-agent" {
					payload["agentId"] = "other"
				}
				if f.mode.Load().(string) == "missing-send-agent" {
					delete(payload, "agentId")
				}
			default:
				t.Errorf("unexpected RPC: %v", request)
				return
			}
			if f.mode.Load().(string) == "wrong-request" {
				payload["requestId"] = "unexpected"
			}
			if f.mode.Load().(string) == "wrong-type" {
				responseType = "unrelated"
			}
			if !write(map[string]any{"type": "session", "message": map[string]any{"type": responseType, "payload": payload}}) {
				return
			}
		}
	}))
	f.server = httptest.NewUnstartedServer(mux)
	f.server.EnableHTTP2 = true
	f.server.TLS = &tls.Config{NextProtos: []string{"h2", "http/1.1"}}
	f.server.StartTLS()
	t.Cleanup(f.server.Close)
	dir := t.TempDir()
	passwordFile := filepath.Join(dir, "password")
	if err = os.WriteFile(passwordFile, []byte("synthetic-password"), 0600); err != nil {
		t.Fatal(err)
	}
	config := nativeConfig{Version: 2, StateDir: filepath.Join(dir, "state"), Hosts: map[string]nativeHost{"test": {ServerID: "daemon-1", PublicKey: publicB64, PasswordFile: passwordFile}}}
	raw, _ := json.Marshal(config)
	configFile := filepath.Join(dir, "native.json")
	if err = os.WriteFile(configFile, raw, 0600); err != nil {
		t.Fatal(err)
	}
	keys, err := loadE2EE(configFile)
	if err != nil {
		t.Fatal(err)
	}
	base := f.server.Client().Transport.(*http.Transport)
	base.ForceAttemptHTTP2 = true
	f.transport = &encryptedTransport{base: base, origin: f.server.URL, keys: keys}
	return f
}
func (f *nativeFixture) request(t *testing.T, path, method, body string) (*http.Response, error) {
	t.Helper()
	r, _ := http.NewRequest(method, f.server.URL+path, strings.NewReader(body))
	r.Header.Set("Authorization", "Bearer owner")
	return f.transport.RoundTrip(r)
}
func TestNativeOperations(t *testing.T) {
	f := newNativeFixture(t)
	for _, path := range []string{"/api/v1/hosts/test", "/api/v1/hosts", "/api/v1/agents?host=test", "/api/v1/workspaces?host=all", "/api/v1/hosts/test/agents/agent", "/api/v1/hosts/test/agents/agent/activity?tail=0", "/api/v1/hosts/test/agents/agent/activity?tail=20"} {
		resp, err := f.request(t, path, "GET", "")
		if err != nil {
			t.Fatalf("%s %v", path, err)
		}
		raw, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil || resp.StatusCode != 200 || !bytes.Contains(raw, []byte(`"ok":true`)) {
			t.Fatalf("%s: %s %v", path, raw, err)
		}
	}
	if !f.passwordSeen.Load() {
		t.Fatal("daemon password did not travel in encrypted hello")
	}
	if f.directoryCalls.Load() != 7 {
		t.Fatal("every operation must recheck admission and pins")
	}
	resp, err := f.request(t, "/api/v1/hosts/test/agents/agent/messages", "POST", `{"prompt":"SYNTHETIC-PROMPT"}`)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 202 || !bytes.Contains(raw, []byte(`"status":"accepted"`)) {
		t.Fatalf("send: %s", raw)
	}
	guard := filepath.Join(f.transport.keys.native.StateDir, "submissions", "test.agent.json")
	if _, err = os.Lstat(guard); !os.IsNotExist(err) {
		t.Fatal("confirmed send retained guard")
	}
}
func TestNativeUncertainSendSurvivesRestart(t *testing.T) {
	f := newNativeFixture(t)
	f.mode.Store("drop-send")
	resp, err := f.request(t, "/api/v1/hosts/test/agents/agent/messages", "POST", `{"prompt":"SYNTHETIC-PROMPT"}`)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if !bytes.Contains(raw, []byte(`"status":"unknown"`)) {
		t.Fatalf("uncertain: %s", raw)
	}
	f.mode.Store("")
	clone := *f.transport
	f.transport = &clone
	if _, err = f.request(t, "/api/v1/hosts/test/agents/agent/messages", "POST", `{"prompt":"SYNTHETIC-PROMPT"}`); err == nil {
		t.Fatal("unresolved send allowed retry")
	}
	if f.sends.Load() != 1 {
		t.Fatal("uncertain send replayed")
	}
}
func TestNativeRejectsUntrustedTraffic(t *testing.T) {
	for _, mode := range []string{"wrong-server", "wrong-request", "wrong-type", "wrong-pin", "restricted-token", "directory-public-key"} {
		t.Run(mode, func(t *testing.T) {
			f := newNativeFixture(t)
			f.mode.Store(mode)
			if mode == "wrong-pin" {
				p := f.transport.keys.native.Hosts["test"]
				raw := make([]byte, 32)
				raw[0] = 9
				p.PublicKey = base64.StdEncoding.EncodeToString(raw)
				f.transport.keys.native.Hosts["test"] = p
			}
			r, _ := http.NewRequest("GET", f.server.URL+"/api/v1/agents?host=test", nil)
			r.Header.Set("Authorization", "Bearer owner")
			if mode == "restricted-token" {
				r.Header.Set("Authorization", "Bearer restricted")
			}
			if resp, err := f.transport.RoundTrip(r); err == nil {
				if resp != nil {
					resp.Body.Close()
				}
				t.Fatal("untrusted native operation succeeded")
			}
			if f.sends.Load() != 0 {
				t.Fatal("untrusted handshake sent task")
			}
		})
	}
}
func TestNativeObservation(t *testing.T) {
	f := newNativeFixture(t)
	resp, err := f.request(t, "/api/v1/hosts/test/agents/agent/events?include=activity", "GET", "")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	buf := make([]byte, 4096)
	var out bytes.Buffer
	for i := 0; i < 4; i++ {
		n, err := resp.Body.Read(buf)
		out.Write(buf[:n])
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(out.String(), "event: activity") {
			break
		}
	}
	for _, kind := range []string{"snapshot", "connection", "activity"} {
		if !strings.Contains(out.String(), "event: "+kind) {
			t.Fatalf("missing %s: %s", kind, out.String())
		}
	}
}
func TestNativeGuardKeepsLegacyRecord(t *testing.T) {
	f := newNativeFixture(t)
	state := f.transport.keys.native.StateDir
	path, err := nativeGuardPath(state, "test", "agent")
	if err != nil {
		t.Fatal(err)
	}
	legacy := fmt.Sprintf(`{"version":1,"host":"test","agent":"agent","requestId":%q,"createdAt":"2026-10-11T00:00:00Z"}`, nativeUUID())
	if err = os.WriteFile(path, []byte(legacy), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = f.request(t, "/api/v1/hosts/test/agents/agent/messages", "POST", `{"prompt":"SYNTHETIC-PROMPT"}`); err == nil {
		t.Fatal("legacy guard overwritten")
	}
	raw, _ := os.ReadFile(path)
	if string(raw) != legacy || f.sends.Load() != 0 {
		t.Fatal("legacy guard lost")
	}
}

func TestNativeRejectsUnverifiedTLS(t *testing.T) {
	f := newNativeFixture(t)
	f.transport.base = http.DefaultTransport
	if _, err := f.request(t, "/api/v1/hosts/test", "GET", ""); err == nil {
		t.Fatal("untrusted TLS certificate accepted")
	}
	if f.directoryCalls.Load() != 0 {
		t.Fatal("unauthenticated TLS reached directory")
	}
}
func TestNativeGuardExcludesConcurrentPreflight(t *testing.T) {
	f := newNativeFixture(t)
	f.mode.Store("drop-send")
	errors := make(chan error, 2)
	for i := 0; i < 2; i++ {
		go func() {
			resp, err := f.request(t, "/api/v1/hosts/test/agents/agent/messages", "POST", `{"prompt":"SYNTHETIC-PROMPT"}`)
			if resp != nil {
				_, readErr := io.ReadAll(resp.Body)
				resp.Body.Close()
				if err == nil {
					err = readErr
				}
			}
			errors <- err
		}()
	}
	for i := 0; i < 2; i++ {
		<-errors
	}
	if f.sends.Load() != 1 {
		t.Fatalf("concurrent preflights dispatched %d messages", f.sends.Load())
	}
}

func TestNativeDirectorySyncBeforeDescendantCreation(t *testing.T) {
	base := t.TempDir()
	directory := filepath.Join(base, "new-parent", "submissions")
	if err := nativeSecureDirWithSync(directory, func(parent string) error {
		if parent == base {
			if _, err := os.Lstat(directory); !os.IsNotExist(err) {
				t.Fatal("descendant created before parent entry sync")
			}
			return fmt.Errorf("synthetic parent fsync failure")
		}
		return nativeSyncDir(parent)
	}); err == nil {
		t.Fatal("parent fsync failure ignored")
	}
	if _, err := os.Lstat(directory); !os.IsNotExist(err) {
		t.Fatal("failed durability allowed guard directory")
	}
	// The retry must sync even the entry created by the failed first writer.
	syncedParent := false
	if err := nativeSecureDirWithSync(directory, func(parent string) error {
		if parent == base {
			syncedParent = true
			if _, err := os.Lstat(directory); !os.IsNotExist(err) {
				t.Fatal("existing unsynced parent skipped")
			}
		}
		return nativeSyncDir(parent)
	}); err != nil {
		t.Fatal(err)
	}
	if !syncedParent {
		t.Fatal("existing ancestor was not persisted")
	}
	info, err := os.Lstat(directory)
	if err != nil || info.Mode().Perm() != 0700 {
		t.Fatal("private state directory permissions changed")
	}
}

func TestNativeSendReceiptRequiresTargetAgent(t *testing.T) {
	for _, mode := range []string{"wrong-send-agent", "missing-send-agent"} {
		t.Run(mode, func(t *testing.T) {
			f := newNativeFixture(t)
			f.mode.Store(mode)
			resp, err := f.request(t, "/api/v1/hosts/test/agents/agent/messages", "POST", `{"prompt":"SYNTHETIC-PROMPT"}`)
			if err != nil {
				t.Fatal(err)
			}
			raw, err := io.ReadAll(resp.Body)
			resp.Body.Close()
			if err != nil || !bytes.Contains(raw, []byte(`"status":"unknown"`)) {
				t.Fatalf("invalid target receipt accepted: %s %v", raw, err)
			}
			guard := filepath.Join(f.transport.keys.native.StateDir, "submissions", "test.agent.json")
			if _, err = os.Lstat(guard); err != nil {
				t.Fatal("invalid target receipt cleared guard")
			}
			f.mode.Store("")
			if _, err = f.request(t, "/api/v1/hosts/test/agents/agent/messages", "POST", `{"prompt":"SYNTHETIC-PROMPT"}`); err == nil {
				t.Fatal("wrong receipt allowed resend")
			}
			if f.sends.Load() != 1 {
				t.Fatal("wrong receipt caused replay")
			}
		})
	}
}
