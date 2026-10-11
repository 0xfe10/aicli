package paseobridgert

import (
	"bytes"
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func testE2EEKeys(t *testing.T, host string) (*e2eeKeys, *ecdh.PrivateKey, string) {
	t.Helper()
	client, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	server, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "key")
	configPath := filepath.Join(dir, "e2ee.json")
	if err = os.WriteFile(keyPath, []byte(base64.StdEncoding.EncodeToString(client.Bytes())), 0600); err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(e2eeConfig{Version: 1, KeyFile: keyPath, Hosts: map[string]string{host: base64.StdEncoding.EncodeToString(server.PublicKey().Bytes())}})
	if err = os.WriteFile(configPath, b, 0600); err != nil {
		t.Fatal(err)
	}
	keys, err := loadE2EE(configPath)
	if err != nil {
		t.Fatal(err)
	}
	return keys, server, configPath
}

// The fixture emulates only the host's crypto endpoint. The broker cannot parse
// original paths, prompts, response text, or SSE events from its wire envelope.
func encryptedFixture(t *testing.T, keys *e2eeKeys, server *ecdh.PrivateKey, target http.Handler, mutate func([]e2eePacket) []e2eePacket) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/e2ee/requests" || r.Method != "POST" {
			t.Error("plaintext business request")
			w.WriteHeader(400)
			return
		}
		wire, _ := io.ReadAll(r.Body)
		if bytes.Contains(wire, []byte("test prompt")) || bytes.Contains(wire, []byte("/agents/")) {
			t.Error("plaintext leaked")
		}
		var p e2eePacket
		if strictJSON(wire, &p) != nil {
			t.Error("bad packet")
			w.WriteHeader(400)
			return
		}
		shared, err := server.ECDH(keys.private.PublicKey())
		if err != nil {
			t.Fatal(err)
		}
		a, _ := packetCipher(shared, p.ID, "request")
		plain, err := openPacket(p, a)
		if err != nil {
			w.WriteHeader(400)
			return
		}
		var request struct {
			Method      string `json:"method"`
			Path        string `json:"path"`
			Body        string `json:"body"`
			LastEventID string `json:"lastEventId"`
		}
		if strictJSON(plain, &request) != nil {
			t.Error("bad inner request")
		}
		body, _ := base64.StdEncoding.DecodeString(request.Body)
		inner := httptest.NewRequest(request.Method, request.Path, bytes.NewReader(body))
		inner.Header.Set("Authorization", r.Header.Get("Authorization"))
		inner.Header.Set("Last-Event-ID", request.LastEventID)
		rec := httptest.NewRecorder()
		target.ServeHTTP(rec, inner)
		a, _ = packetCipher(shared, p.ID, "response")
		var packets []e2eePacket
		add := func(kind string, value any) {
			b, _ := json.Marshal(value)
			frame := p
			frame.Kind = kind
			frame.Seq = len(packets)
			frame, _ = sealPacket(frame, a, b)
			packets = append(packets, frame)
		}
		contentType := rec.Header().Get("Content-Type")
		if contentType == "" {
			contentType = "application/json"
		}
		add("headers", map[string]any{"status": rec.Code, "contentType": contentType})
		add("chunk", map[string]any{"data": base64.StdEncoding.EncodeToString(rec.Body.Bytes())})
		add("end", map[string]any{"frames": len(packets), "bytes": rec.Body.Len()})
		if mutate != nil {
			packets = mutate(packets)
		}
		w.Header().Set("Content-Type", "application/x-ndjson")
		for _, p := range packets {
			_ = json.NewEncoder(w).Encode(p)
		}
	}
}
func TestEncryptedTransportIntegrity(t *testing.T) {
	keys, serverKey, _ := testE2EEKeys(t, "test")
	for _, mode := range []string{"good", "tamper", "truncate", "reorder", "wrong-host", "wrong-pin", "sse", "sse-truncate", "tail", "huge-chunk"} {
		t.Run(mode, func(t *testing.T) {
			handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if mode == "huge-chunk" {
					w.Header().Set("Content-Type", "application/json")
					fmt.Fprint(w, strings.Repeat(" ", 32769))
					return
				}
				if mode == "sse" || mode == "sse-truncate" {
					w.Header().Set("Content-Type", "text/event-stream")
					fmt.Fprint(w, "event: snapshot\ndata: safe\n\n")
				} else {
					w.Header().Set("Content-Type", "application/json")
					fmt.Fprint(w, `{"ok":true,"data":{"host":"test","agentId":"a","status":"accepted"}}`)
				}
			})
			mutation := func(p []e2eePacket) []e2eePacket {
				switch mode {
				case "tamper":
					p[1].Data = base64.StdEncoding.EncodeToString([]byte("bad ciphertext"))
				case "truncate", "sse-truncate":
					return p[:2]
				case "tail":
					p = append(p, p[len(p)-1])
				case "reorder":
					p[1], p[2] = p[2], p[1]
				case "wrong-host":
					p[1].Host = "evil"
				}
				return p
			}
			srv := httptest.NewServer(encryptedFixture(t, keys, serverKey, handler, mutation))
			defer srv.Close()
			selected := keys
			if mode == "wrong-pin" {
				selected, _, _ = testE2EEKeys(t, "test")
			}
			transport := &encryptedTransport{base: http.DefaultTransport, origin: srv.URL, keys: selected}
			req, _ := http.NewRequest("POST", srv.URL+"/api/v1/hosts/test/agents/a/messages", strings.NewReader(`{"prompt":"test prompt"}`))
			req.Header.Set("Authorization", "Bearer secret")
			resp, err := transport.RoundTrip(req)
			if err == nil {
				_, err = io.ReadAll(resp.Body)
				resp.Body.Close()
			}
			good := mode == "good" || mode == "sse"
			if (err == nil) != good {
				t.Fatalf("%s: %v", mode, err)
			}
		})
	}
}
func TestEncryptedFailClosed(t *testing.T) {
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hits++ }))
	defer srv.Close()
	transport := &encryptedTransport{base: http.DefaultTransport, origin: srv.URL}
	req, _ := http.NewRequest("GET", srv.URL+"/api/v1/hosts", nil)
	if _, err := transport.RoundTrip(req); err == nil || hits != 0 {
		t.Fatal("missing E2EE allowed request")
	}
	keys, _, path := testE2EEKeys(t, "test")
	transport.keys = keys
	req, _ = http.NewRequest("GET", srv.URL+"/api/v1/hosts/unknown", nil)
	if _, err := transport.RoundTrip(req); err == nil || hits != 0 {
		t.Fatal("unpinned host allowed request")
	}
	if err := os.Chmod(path, 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := loadE2EE(path); err == nil {
		t.Fatal("world readable key configuration allowed")
	}
}
func TestEncryptedProtocolVector(t *testing.T) {
	raw := make([]byte, 32)
	for i := range raw {
		raw[i] = byte(i)
	}
	key, _ := ecdh.X25519().NewPrivateKey(raw)
	otherRaw := make([]byte, 32)
	for i := range otherRaw {
		otherRaw[i] = byte(i + 32)
	}
	other, _ := ecdh.X25519().NewPrivateKey(otherRaw)
	shared, _ := key.ECDH(other.PublicKey())
	a, _ := packetCipher(shared, "00000000-0000-4000-8000-000000000000", "request")
	p := e2eePacket{V: 1, Host: "aspen", Client: "client", ID: "00000000-0000-4000-8000-000000000000", Expires: 100000, Op: "send", Kind: "request", Seq: 0}
	nonce := make([]byte, 12)
	plain := []byte(`{"method":"POST","path":"/api/v1/hosts/aspen/agents/a/messages","body":"e30=","lastEventId":""}`)
	sealed := a.Seal(nil, nonce, plain, packetAAD(p))
	t.Log("vector shared", hex.EncodeToString(shared), "ciphertext", base64.StdEncoding.EncodeToString(sealed), "aad", string(packetAAD(p)))
	const want = "U3IPxRJd063SxlOQ4o3i4sujtqFXpIjByCUgk5EevpWWRP5hlgozsEKlkU0uuSKrh6O6yRqgrKSVa/udMja/TcOEiBNg3CCqmo0fl76h+lrHUHCGR0RSOxsHD6lB/c+4edNBgJm+smtllJGTr77o"
	if base64.StdEncoding.EncodeToString(sealed) != want {
		t.Fatal("cross-language protocol vector changed")
	}
}

func TestEncryptedFanoutAndScopes(t *testing.T) {
	keys, serverKey, _ := testE2EEKeys(t, "test")
	keys.hosts["offline"] = serverKey.PublicKey()
	var paths []string
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.RequestURI())
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(r.URL.Path, "offline") {
			w.WriteHeader(503)
			fmt.Fprint(w, `{"ok":false,"error":{"code":"offline"}}`)
			return
		}
		fmt.Fprint(w, `{"ok":true,"data":{"results":[{"host":"test","state":"online"}],"partial":false}}`)
	})
	srv := httptest.NewServer(encryptedFixture(t, keys, serverKey, handler, nil))
	defer srv.Close()
	transport := &encryptedTransport{base: http.DefaultTransport, origin: srv.URL, keys: keys}
	req, _ := http.NewRequest("GET", srv.URL+"/api/v1/hosts", nil)
	resp, err := transport.RoundTrip(req)
	if err != nil {
		t.Fatal(err)
	}
	b, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil || !bytes.Contains(b, []byte(`"partial":true`)) || !bytes.Contains(b, []byte(`"host":"offline"`)) {
		t.Fatalf("partial: %s %v", b, err)
	}
	if len(paths) != 2 || paths[0] != "/api/v1/hosts/offline" || paths[1] != "/api/v1/hosts/test" {
		t.Fatalf("plaintext/all route used: %v", paths)
	}
	for _, tc := range []struct{ path, op string }{{"/api/v1/hosts/test/agents/a/events?include=activity", "observe-activity"}, {"/api/v1/hosts/test/agents/a/events?include=state", "wait"}} {
		r, _ := http.NewRequest("GET", srv.URL+tc.path, nil)
		_, op, _, err := businessRoute(r)
		if err != nil || op != tc.op {
			t.Fatalf("scope %s: %s %v", tc.path, op, err)
		}
	}
}

func TestEncryptedResponseIdentity(t *testing.T) {
	request, _ := http.NewRequest("GET", "https://bridge.test/api/v1/hosts/test/agents/a", nil)
	for _, tc := range []struct {
		body  string
		valid bool
	}{
		{`{"ok":true,"data":{"host":"test","agentId":"a","agent":{"id":"a","status":"idle"}}}`, true},
		{`{"ok":true,"data":{"host":"test","agentId":"other","agent":{"id":"a"}}}`, false},
		{`{"ok":true,"data":{"agentId":"a","agent":{"id":"a"}}}`, false},
		{`{"ok":true,"data":{"host":"test","agent":{"id":"a"}}}`, false},
		{`{"ok":false,"error":{"code":"offline","message":"Host offline"}}`, true},
		{`{"ok":false,"error":{"message":"Unstructured failure"}}`, false},
	} {
		if valid := validateResponseIdentity([]byte(tc.body), request, "test") == nil; valid != tc.valid {
			t.Fatalf("identity %s: %v", tc.body, valid)
		}
	}
	request, _ = http.NewRequest("GET", "https://bridge.test/api/v1/agents?host=test", nil)
	for _, body := range []string{`{"ok":true,"data":{"results":[]}}`, `{"ok":true,"data":{"results":[{"host":"test"},{"host":"test"}]}}`, `{"ok":true,"data":{"results":[{"host":"other"}]}}`} {
		if validateResponseIdentity([]byte(body), request, "test") == nil {
			t.Fatalf("unbound host results: %s", body)
		}
	}
}
