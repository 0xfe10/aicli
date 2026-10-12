package paseobridgert

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"golang.org/x/crypto/nacl/box"
	"golang.org/x/net/websocket"
)

var nativeFailure = fmt.Errorf("PASEO_BRIDGE native encrypted operation failed; send outcome may be uncertain")

type nativeFrame struct {
	data   []byte
	binary bool
}

var nativeCodec = websocket.Codec{
	Marshal: func(v any) ([]byte, byte, error) {
		f := v.(nativeFrame)
		op := byte(websocket.TextFrame)
		if f.binary {
			op = websocket.BinaryFrame
		}
		return f.data, op, nil
	},
	Unmarshal: func(data []byte, op byte, v any) error {
		if op != websocket.TextFrame && op != websocket.BinaryFrame {
			return nativeFailure
		}
		f := v.(*nativeFrame)
		f.data = append([]byte(nil), data...)
		f.binary = op == websocket.BinaryFrame
		return nil
	},
}

type nativeChannel struct {
	ws     *websocket.Conn
	shared [32]byte
	ctx    context.Context
	stop   func() bool
	frames int
	bytes  int
	seen   map[[24]byte]struct{}
}

func nativeUUID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return ""
	}
	b[6] = (b[6] & 15) | 64
	b[8] = (b[8] & 63) | 128
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[:4], b[4:6], b[6:8], b[8:10], b[10:])
}
func (t *encryptedTransport) nativeConnect(ctx context.Context, host, authorization string) (*nativeChannel, error) {
	pin, ok := t.keys.native.Hosts[host]
	if !ok {
		return nil, fmt.Errorf("Bridge host has no verified native Relay pin")
	}
	if err := t.nativeDirectory(ctx, authorization, host, pin); err != nil {
		return nil, err
	}
	clientID, err := nativeClientID(t.keys.native.StateDir, host)
	if err != nil {
		return nil, err
	}
	password := ""
	if pin.PasswordFile != "" {
		raw, e := protectedRead(pin.PasswordFile)
		if e != nil {
			return nil, fmt.Errorf("native Relay password file is unavailable")
		}
		password = strings.TrimSpace(string(raw))
		if password == "" {
			return nil, fmt.Errorf("native Relay password file is empty")
		}
	}
	origin, err := url.Parse(t.origin)
	if err != nil {
		return nil, nativeFailure
	}
	target := *origin
	switch origin.Scheme {
	case "https":
		target.Scheme = "wss"
	case "http":
		if origin.Hostname() != "127.0.0.1" && origin.Hostname() != "localhost" && origin.Hostname() != "::1" {
			return nil, fmt.Errorf("native Relay requires HTTPS")
		}
		target.Scheme = "ws"
	default:
		return nil, nativeFailure
	}
	target.Path = "/api/v1/native/connect/" + host
	target.RawQuery = url.Values{"role": {"client"}, "serverId": {pin.ServerID}}.Encode()
	cfg, err := websocket.NewConfig(target.String(), origin.String())
	if err != nil {
		return nil, nativeFailure
	}
	cfg.Header.Set("Authorization", authorization)
	cfg.Header.Set("User-Agent", "paseo-bridge/native-relay")
	cfg.Dialer = &net.Dialer{Timeout: 8 * time.Second}
	cfg.TlsConfig = &tls.Config{MinVersion: tls.VersionTLS12}
	if transport, ok := t.base.(*http.Transport); ok && transport.TLSClientConfig != nil {
		cfg.TlsConfig = transport.TLSClientConfig.Clone()
		if cfg.TlsConfig.MinVersion < tls.VersionTLS12 {
			cfg.TlsConfig.MinVersion = tls.VersionTLS12
		}
		if cfg.TlsConfig.InsecureSkipVerify {
			return nil, fmt.Errorf("native Relay refuses unverified TLS")
		}
	}
	// Classic WebSocket upgrade uses HTTP/1.1. The directory HTTP transport
	// may have enabled h2 on its shared TLS config; never inherit that ALPN.
	cfg.TlsConfig.NextProtos = []string{"http/1.1"}
	handshakeCtx, handshakeCancel := context.WithTimeout(ctx, 8*time.Second)
	ws, err := cfg.DialContext(handshakeCtx)
	handshakeCancel()
	if err != nil {
		return nil, nativeFailure
	}
	ws.MaxPayloadBytes = 2 << 20
	channel := &nativeChannel{ws: ws, ctx: ctx, seen: map[[24]byte]struct{}{}}
	channel.stop = context.AfterFunc(ctx, func() { _ = ws.Close() })
	success := false
	defer func() {
		if !success {
			channel.close()
		}
	}()
	pub, secret, err := box.GenerateKey(rand.Reader)
	if err != nil {
		return nil, nativeFailure
	}
	raw, _ := base64.StdEncoding.Strict().DecodeString(pin.PublicKey)
	var peer [32]byte
	copy(peer[:], raw)
	box.Precompute(&channel.shared, &peer, secret)
	hello, _ := json.Marshal(map[string]any{"type": "e2ee_hello", "key": base64.StdEncoding.EncodeToString(pub[:]), "capabilities": map[string]bool{"binaryCiphertext": true}})
	channel.deadline(8 * time.Second)
	if nativeCodec.Send(ws, nativeFrame{data: hello}) != nil {
		return nil, nativeFailure
	}
	var ready nativeFrame
	if nativeCodec.Receive(ws, &ready) != nil || ready.binary {
		return nil, nativeFailure
	}
	var r struct {
		Type         string `json:"type"`
		Capabilities struct {
			Binary bool `json:"binaryCiphertext"`
		} `json:"capabilities"`
	}
	if json.Unmarshal(ready.data, &r) != nil || r.Type != "e2ee_ready" {
		return nil, nativeFailure
	}
	appHello := map[string]any{"type": "hello", "clientId": clientID, "clientType": "cli", "protocolVersion": 1, "capabilities": map[string]bool{"hello_rejection": true, "explicit_event_subscriptions": true, "all_providers": true}}
	if password != "" {
		appHello["auth"] = map[string]string{"kind": "password", "password": password}
	}
	if channel.send(appHello) != nil {
		return nil, nativeFailure
	}
	// Ready is unauthenticated. Only decrypting the pinned daemon's server_info
	// confirms the connection and completion of daemon authentication.
	for i := 0; i < 100; i++ {
		m, e := channel.read()
		if e != nil {
			return nil, nativeFailure
		}
		if stringField(m, "type") == "hello.rejected" {
			return nil, fmt.Errorf("native daemon authentication rejected")
		}
		if stringField(m, "type") != "session" {
			continue
		}
		msg := objectField(m, "message")
		payload := objectField(msg, "payload")
		if stringField(msg, "type") == "status" && stringField(payload, "status") == "server_info" {
			if stringField(payload, "serverId") != pin.ServerID {
				return nil, fmt.Errorf("native daemon identity does not match local pin")
			}
			success = true
			return channel, nil
		}
	}
	return nil, nativeFailure
}
func (t *encryptedTransport) nativeDirectory(ctx context.Context, authorization, host string, pin nativeHost) error {
	req, err := http.NewRequestWithContext(ctx, "GET", t.origin+"/api/v1/native/hosts", nil)
	if err != nil {
		return nativeFailure
	}
	req.Header.Set("Authorization", authorization)
	req.Header.Set("User-Agent", "paseo-bridge/native-relay")
	req.Header.Set("Cache-Control", "no-store")
	resp, err := t.base.RoundTrip(req)
	if err != nil {
		return nativeFailure
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("native Relay owner admission denied")
	}
	raw, err := readBounded(resp.Body, 65536)
	if err != nil {
		return nativeFailure
	}
	var directory struct {
		OK   bool `json:"ok"`
		Data struct {
			Hosts map[string]struct {
				ServerID string `json:"serverId"`
			} `json:"hosts"`
		} `json:"data"`
	}
	if strictJSON(raw, &directory) != nil || !directory.OK {
		return nativeFailure
	}
	published, ok := directory.Data.Hosts[host]
	if !ok || published.ServerID != pin.ServerID {
		return fmt.Errorf("native Relay directory does not match independently verified local pins")
	}
	return nil
}
func (c *nativeChannel) close() {
	if c.stop != nil {
		c.stop()
	}
	_ = c.ws.Close()
	for i := range c.shared {
		c.shared[i] = 0
	}
}
func (c *nativeChannel) deadline(limit time.Duration) {
	end := time.Now().Add(limit)
	if d, ok := c.ctx.Deadline(); ok && d.Before(end) {
		end = d
	}
	_ = c.ws.SetDeadline(end)
}
func (c *nativeChannel) send(value any) error {
	raw, err := json.Marshal(value)
	if err != nil || len(raw) > 262144 {
		return nativeFailure
	}
	var nonce [24]byte
	if _, err = rand.Read(nonce[:]); err != nil {
		return nativeFailure
	}
	bundle := append([]byte(nil), nonce[:]...)
	bundle = box.SealAfterPrecomputation(bundle, raw, &nonce, &c.shared)
	return nativeCodec.Send(c.ws, nativeFrame{data: []byte(base64.StdEncoding.EncodeToString(bundle))})
}
func (c *nativeChannel) read() (map[string]any, error) {
	c.frames++
	if c.frames > 10000 {
		return nil, nativeFailure
	}
	var f nativeFrame
	if nativeCodec.Receive(c.ws, &f) != nil {
		return nil, nativeFailure
	}
	var bundle []byte
	var err error
	if f.binary {
		bundle = f.data
	} else {
		bundle, err = base64.StdEncoding.Strict().DecodeString(string(f.data))
	}
	if err != nil || len(bundle) < 40 {
		return nil, nativeFailure
	}
	var nonce [24]byte
	copy(nonce[:], bundle[:24])
	if _, exists := c.seen[nonce]; exists {
		return nil, nativeFailure
	}
	c.seen[nonce] = struct{}{}
	raw, ok := box.OpenAfterPrecomputation(nil, bundle[24:], &nonce, &c.shared)
	c.bytes += len(raw)
	if !ok || c.bytes > maxEncryptedResponse {
		return nil, nativeFailure
	}
	var value map[string]any
	if json.Unmarshal(raw, &value) != nil {
		return nil, nativeFailure
	}
	return value, nil
}
func (c *nativeChannel) rpc(requestType, responseType string, fields map[string]any) (map[string]any, error) {
	id := nativeUUID()
	if id == "" {
		return nil, nativeFailure
	}
	fields["type"] = requestType
	fields["requestId"] = id
	c.deadline(15 * time.Second)
	if c.send(map[string]any{"type": "session", "message": fields}) != nil {
		return nil, nativeFailure
	}
	for i := 0; i < 1000; i++ {
		m, err := c.read()
		if err != nil {
			return nil, err
		}
		if stringField(m, "type") != "session" {
			continue
		}
		msg := objectField(m, "message")
		payload := objectField(msg, "payload")
		requestID := stringField(payload, "requestId")
		if requestID == "" {
			continue
		}
		if requestID != id {
			return nil, nativeFailure
		}
		if stringField(msg, "type") != responseType {
			return nil, nativeFailure
		}
		if errorValue, exists := payload["error"]; exists && errorValue != nil && errorValue != "" && responseType != "wait_for_finish_response" {
			return nil, nativeFailure
		}
		return payload, nil
	}
	return nil, nativeFailure
}
func stringField(m map[string]any, key string) string { v, _ := m[key].(string); return v }
func objectField(m map[string]any, key string) map[string]any {
	v, _ := m[key].(map[string]any)
	return v
}
