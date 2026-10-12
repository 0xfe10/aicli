package paseobridgert

import (
	"bufio"
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"time"
)

const maxEncryptedResponse = 16 << 20

var hostName = regexp.MustCompile(`^[a-z][a-z0-9-]{0,62}$`)
var errEncryptedResponse = fmt.Errorf("PASEO_BRIDGE encrypted response authentication or framing failed; send outcome may be uncertain")

type e2eeConfig struct {
	Version int               `json:"version"`
	KeyFile string            `json:"keyFile"`
	Hosts   map[string]string `json:"hosts"`
}
type e2eeKeys struct {
	native  *nativeConfig
	private *ecdh.PrivateKey
	client  string
	hosts   map[string]*ecdh.PublicKey
}

type e2eePacket struct {
	V       int    `json:"v"`
	Host    string `json:"host"`
	Client  string `json:"client"`
	ID      string `json:"id"`
	Expires int64  `json:"expires"`
	Op      string `json:"op"`
	Kind    string `json:"kind"`
	Seq     int    `json:"seq"`
	Nonce   string `json:"nonce"`
	Data    string `json:"data"`
}

func strictJSON(data []byte, value any) error {
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if err := d.Decode(value); err != nil {
		return err
	}
	if d.Decode(new(any)) != io.EOF {
		return fmt.Errorf("trailing JSON")
	}
	return nil
}
func protectedRead(path string) ([]byte, error) {
	if runtime.GOOS == "windows" {
		return nil, fmt.Errorf("PASEO_BRIDGE E2EE is unavailable on Windows until file ACL verification is implemented")
	}
	if !filepath.IsAbs(path) {
		return nil, fmt.Errorf("E2EE paths must be absolute")
	}
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if err = rejectInsecureFile(path, info); err != nil {
		return nil, err
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	actual, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !os.SameFile(info, actual) {
		return nil, fmt.Errorf("E2EE file changed")
	}
	b, err := io.ReadAll(io.LimitReader(f, 65537))
	if len(b) > 65536 {
		return nil, fmt.Errorf("E2EE file too large")
	}
	return b, err
}
func loadE2EE(path string) (*e2eeKeys, error) {
	if path == "" {
		return nil, fmt.Errorf("PASEO_BRIDGE E2EE is not configured; set e2ee_file or PASEO_BRIDGE_E2EE_FILE")
	}
	b, err := protectedRead(path)
	if err != nil {
		return nil, err
	}
	var version struct {
		Version int `json:"version"`
	}
	if json.Unmarshal(b, &version) != nil {
		return nil, fmt.Errorf("invalid E2EE configuration")
	}
	if version.Version == 2 {
		native, err := loadNativeConfig(b)
		if err != nil {
			return nil, err
		}
		return &e2eeKeys{native: native}, nil
	}
	var c e2eeConfig
	if err = strictJSON(b, &c); err != nil {
		return nil, fmt.Errorf("invalid E2EE configuration")
	}
	if c.Version != 1 || len(c.Hosts) == 0 {
		return nil, fmt.Errorf("invalid E2EE configuration")
	}
	b, err = protectedRead(c.KeyFile)
	if err != nil {
		return nil, err
	}
	raw, err := base64.StdEncoding.Strict().DecodeString(strings.TrimSpace(string(b)))
	if err != nil || len(raw) != 32 {
		return nil, fmt.Errorf("invalid E2EE private key")
	}
	private, err := ecdh.X25519().NewPrivateKey(raw)
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(private.PublicKey().Bytes())
	k := &e2eeKeys{private: private, client: hex.EncodeToString(sum[:]), hosts: map[string]*ecdh.PublicKey{}}
	for host, value := range c.Hosts {
		if !hostName.MatchString(host) {
			return nil, fmt.Errorf("invalid E2EE host")
		}
		raw, err = base64.StdEncoding.Strict().DecodeString(value)
		if err != nil || len(raw) != 32 {
			return nil, fmt.Errorf("invalid E2EE host pin")
		}
		pub, e := ecdh.X25519().NewPublicKey(raw)
		if e != nil {
			return nil, e
		}
		if _, e = private.ECDH(pub); e != nil {
			return nil, fmt.Errorf("invalid E2EE host pin")
		}
		k.hosts[host] = pub
	}
	return k, nil
}
func packetAAD(p e2eePacket) []byte {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	_ = enc.Encode([]any{p.V, p.Host, p.Client, p.ID, p.Expires, p.Op, p.Kind, p.Seq})
	return bytes.TrimSuffix(b.Bytes(), []byte("\n"))
}
func packetCipher(shared []byte, id, direction string) (cipher.AEAD, error) {
	k, err := hkdf.Key(sha256.New, shared, []byte(id), "paseo-bridge-e2ee-v1/"+direction, 32)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(k)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}
func sealPacket(p e2eePacket, a cipher.AEAD, plain []byte) (e2eePacket, error) {
	nonce := make([]byte, 12)
	if _, err := rand.Read(nonce); err != nil {
		return p, err
	}
	p.Nonce = base64.StdEncoding.EncodeToString(nonce)
	p.Data = base64.StdEncoding.EncodeToString(a.Seal(nil, nonce, plain, packetAAD(p)))
	return p, nil
}
func openPacket(p e2eePacket, a cipher.AEAD) ([]byte, error) {
	nonce, err := base64.StdEncoding.Strict().DecodeString(p.Nonce)
	if err != nil || len(nonce) != 12 {
		return nil, errEncryptedResponse
	}
	data, err := base64.StdEncoding.Strict().DecodeString(p.Data)
	if err != nil {
		return nil, errEncryptedResponse
	}
	return a.Open(nil, nonce, data, packetAAD(p))
}

type encryptedTransport struct {
	base      http.RoundTripper
	origin    string
	specURL   string
	keys      *e2eeKeys
	configErr error
}

func businessRoute(r *http.Request) (host, op string, all bool, err error) {
	p := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(p) < 3 || p[0] != "api" || p[1] != "v1" {
		err = fmt.Errorf("unsupported Bridge business route")
		return
	}
	switch p[2] {
	case "hosts":
		op = "status"
		if len(p) == 3 {
			all = true
			return
		}
		host = p[3]
		if len(p) == 4 {
			return
		}
		if len(p) < 6 || p[4] != "agents" {
			err = fmt.Errorf("unsupported Bridge business route")
			return
		}
		op = "wait"
		if len(p) == 7 {
			switch p[6] {
			case "messages":
				op = "send"
			case "activity":
				op = "activity"
			case "events":
				op = "wait"
				if r.URL.Query().Get("include") == "activity" {
					op = "observe-activity"
				}
			default:
				err = fmt.Errorf("unsupported Bridge business route")
			}
		} else if len(p) != 6 {
			err = fmt.Errorf("unsupported Bridge business route")
		}
	case "agents", "workspaces":
		op = p[2]
		if len(p) != 3 {
			err = fmt.Errorf("unsupported Bridge business route")
			return
		}
		host = r.URL.Query().Get("host")
		all = host == "" || host == "all"
	default:
		err = fmt.Errorf("unsupported Bridge business route")
	}
	return
}
func (t *encryptedTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.Method == "GET" && t.specURL != "" && r.URL.String() == t.specURL && r.Header.Get("Authorization") == "" {
		return t.base.RoundTrip(r)
	}
	origin := r.URL.Scheme + "://" + r.URL.Host
	if origin != t.origin {
		return nil, fmt.Errorf("refusing cross-origin Bridge request")
	}
	if r.Method == "GET" && r.URL.Path == "/api/v1/openapi.json" {
		return t.base.RoundTrip(r)
	}
	if t.configErr != nil {
		return nil, t.configErr
	}
	if t.keys == nil {
		return nil, fmt.Errorf("Bridge E2EE required")
	}
	host, op, all, err := businessRoute(r)
	if err != nil {
		return nil, err
	}
	if all {
		return t.fanout(r, op)
	}
	return t.one(r, host, op)
}
func (t *encryptedTransport) one(r *http.Request, host, op string) (*http.Response, error) {
	if t.keys.native != nil {
		return t.nativeOne(r, host, op)
	}
	pub, ok := t.keys.hosts[host]
	if !ok {
		return nil, fmt.Errorf("Bridge host has no verified E2EE pin")
	}
	if r.Method != "GET" && r.Method != "POST" {
		return nil, fmt.Errorf("unsupported E2EE method")
	}
	var body []byte
	var err error
	if r.Body != nil {
		body, err = io.ReadAll(io.LimitReader(r.Body, 262145))
		_ = r.Body.Close()
		if err != nil {
			return nil, err
		}
		if len(body) > 262144 {
			return nil, fmt.Errorf("E2EE request too large")
		}
	}
	id := make([]byte, 16)
	if _, err = rand.Read(id); err != nil {
		return nil, err
	}
	id[6] = (id[6] & 15) | 64
	id[8] = (id[8] & 63) | 128
	p := e2eePacket{V: 1, Host: host, Client: t.keys.client, ID: fmt.Sprintf("%x-%x-%x-%x-%x", id[:4], id[4:6], id[6:8], id[8:10], id[10:]), Expires: time.Now().UnixMilli() + 90000, Op: op, Kind: "request"}
	shared, err := t.keys.private.ECDH(pub)
	if err != nil {
		return nil, err
	}
	a, err := packetCipher(shared, p.ID, "request")
	if err != nil {
		return nil, err
	}
	plain, _ := json.Marshal(struct {
		Method      string `json:"method"`
		Path        string `json:"path"`
		Body        string `json:"body"`
		LastEventID string `json:"lastEventId"`
	}{r.Method, r.URL.RequestURI(), base64.StdEncoding.EncodeToString(body), r.Header.Get("Last-Event-ID")})
	p, err = sealPacket(p, a, plain)
	if err != nil {
		return nil, err
	}
	encoded, _ := json.Marshal(p)
	ctx, cancel := context.WithTimeout(r.Context(), 150*time.Second)
	keepStreaming := false
	defer func() {
		if !keepStreaming {
			cancel()
		}
	}()
	outer, err := http.NewRequestWithContext(ctx, "POST", t.origin+"/api/v1/e2ee/requests", bytes.NewReader(encoded))
	if err != nil {
		return nil, err
	}
	outer.Header.Set("Authorization", r.Header.Get("Authorization"))
	outer.Header.Set("Content-Type", "application/json")
	outer.GetBody = nil
	response, err := t.base.RoundTrip(outer)
	if err != nil {
		return nil, fmt.Errorf("Bridge encrypted transport failed; send outcome may be uncertain")
	}
	if response.StatusCode != 200 || strings.Split(response.Header.Get("Content-Type"), ";")[0] != "application/x-ndjson" {
		response.Body.Close()
		return nil, fmt.Errorf("Bridge encrypted broker rejected request; send outcome may be uncertain")
	}
	a, err = packetCipher(shared, p.ID, "response")
	if err != nil {
		response.Body.Close()
		return nil, err
	}
	frames := &encryptedFrames{scanner: bufio.NewScanner(response.Body), a: a, meta: p}
	frames.scanner.Buffer(make([]byte, 4096), 128<<10)
	header, kind, err := frames.next()
	if err != nil || kind != "headers" {
		response.Body.Close()
		return nil, errEncryptedResponse
	}
	var h struct {
		Status      int    `json:"status"`
		ContentType string `json:"contentType"`
	}
	if strictJSON(header, &h) != nil || h.Status < 200 || h.Status > 599 || (h.ContentType != "application/json" && h.ContentType != "text/event-stream") {
		response.Body.Close()
		return nil, errEncryptedResponse
	}
	frames.requireEOF = h.ContentType == "application/json"
	result := &http.Response{StatusCode: h.Status, Status: fmt.Sprintf("%d %s", h.Status, http.StatusText(h.Status)), Header: http.Header{"Content-Type": []string{h.ContentType}}, Request: r, ContentLength: -1}
	if h.ContentType == "text/event-stream" {
		reader, writer := io.Pipe()
		result.Body = &encryptedStream{PipeReader: reader, source: response.Body, cancel: cancel}
		keepStreaming = true
		go func() {
			defer response.Body.Close()
			defer cancel()
			stop := context.AfterFunc(ctx, func() { _ = response.Body.Close(); _ = writer.CloseWithError(ctx.Err()) })
			defer stop()
			_ = writer.CloseWithError(frames.copy(writer))
		}()
		return result, nil
	}
	defer response.Body.Close()
	var out bytes.Buffer
	if err = frames.copy(&out); err != nil {
		return nil, err
	}
	if err = validateResponseIdentity(out.Bytes(), r, host); err != nil {
		return nil, err
	}
	result.Body = io.NopCloser(bytes.NewReader(out.Bytes()))
	result.ContentLength = int64(out.Len())
	return result, nil
}

type encryptedStream struct {
	*io.PipeReader
	source io.Closer
	cancel context.CancelFunc
}

func (s *encryptedStream) Close() error {
	s.cancel()
	_ = s.source.Close()
	return s.PipeReader.Close()
}

type encryptedFrames struct {
	scanner    *bufio.Scanner
	a          cipher.AEAD
	meta       e2eePacket
	seq        int
	bytes      int
	requireEOF bool
}

func (f *encryptedFrames) next() ([]byte, string, error) {
	if !f.scanner.Scan() {
		return nil, "", errEncryptedResponse
	}
	var p e2eePacket
	if strictJSON(f.scanner.Bytes(), &p) != nil || p.V != 1 || p.Host != f.meta.Host || p.Client != f.meta.Client || p.ID != f.meta.ID || p.Expires != f.meta.Expires || p.Op != f.meta.Op || p.Seq != f.seq || p.Seq > 1000000 || time.Now().UnixMilli() > p.Expires+60000 {
		return nil, "", errEncryptedResponse
	}
	plain, err := openPacket(p, f.a)
	if err != nil {
		return nil, "", errEncryptedResponse
	}
	f.seq++
	return plain, p.Kind, nil
}
func (f *encryptedFrames) copy(w io.Writer) error {
	for {
		plain, kind, err := f.next()
		if err != nil {
			return err
		}
		switch kind {
		case "chunk":
			var c struct {
				Data string `json:"data"`
			}
			if strictJSON(plain, &c) != nil {
				return errEncryptedResponse
			}
			raw, err := base64.StdEncoding.Strict().DecodeString(c.Data)
			if err != nil || len(raw) > 32768 {
				return errEncryptedResponse
			}
			f.bytes += len(raw)
			if f.bytes > maxEncryptedResponse {
				return errEncryptedResponse
			}
			if _, err = w.Write(raw); err != nil {
				return err
			}
		case "end":
			var e struct {
				Frames int `json:"frames"`
				Bytes  int `json:"bytes"`
			}
			if strictJSON(plain, &e) != nil || e.Frames != f.seq-1 || e.Bytes != f.bytes {
				return errEncryptedResponse
			}
			if f.requireEOF && (f.scanner.Scan() || f.scanner.Err() != nil) {
				return errEncryptedResponse
			}
			return nil
		default:
			return errEncryptedResponse
		}
	}
}
func validateResponseIdentity(body []byte, r *http.Request, host string) error {
	var v struct {
		OK    *bool `json:"ok"`
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
		Data struct {
			Host    string `json:"host"`
			AgentID string `json:"agentId"`
			Results []struct {
				Host string `json:"host"`
			} `json:"results"`
		} `json:"data"`
	}
	if json.Unmarshal(body, &v) != nil || v.OK == nil {
		return errEncryptedResponse
	}
	if !*v.OK {
		if v.Error.Code == "" {
			return errEncryptedResponse
		}
		return nil
	}
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) >= 6 {
		if v.Data.Host != host || v.Data.AgentID != parts[5] {
			return errEncryptedResponse
		}
	} else {
		if len(v.Data.Results) != 1 || v.Data.Results[0].Host != host {
			return errEncryptedResponse
		}
	}
	return nil
}

func (t *encryptedTransport) fanout(r *http.Request, op string) (*http.Response, error) {
	if r.Method != "GET" {
		return nil, fmt.Errorf("E2EE fanout only supports reads")
	}
	hosts := make([]string, 0, len(t.keys.hosts))
	if t.keys.native != nil {
		for h := range t.keys.native.Hosts {
			hosts = append(hosts, h)
		}
	} else {
		for h := range t.keys.hosts {
			hosts = append(hosts, h)
		}
	}
	sort.Strings(hosts)
	results := []any{}
	partial := false
	totalBytes := 0
	failedHost := func(host string) any {
		if op == "status" {
			return map[string]any{"host": host, "fetchedAt": nativeFetchedAt(), "state": "unavailable"}
		}
		return map[string]any{"host": host, "fetchedAt": nativeFetchedAt(), "error": map[string]string{"code": "e2ee_failed", "message": "Encrypted host request failed"}}
	}
	for _, host := range hosts {
		clone := r.Clone(r.Context())
		u := *r.URL
		clone.URL = &u
		if op == "status" {
			u.Path = "/api/v1/hosts/" + host
		} else {
			q := u.Query()
			q.Set("host", host)
			u.RawQuery = q.Encode()
		}
		resp, err := t.one(clone, host, op)
		if err != nil {
			partial = true
			results = append(results, failedHost(host))
			continue
		}
		b, e := io.ReadAll(resp.Body)
		resp.Body.Close()
		totalBytes += len(b)
		if totalBytes > maxEncryptedResponse {
			return nil, fmt.Errorf("encrypted aggregate response limit exceeded; narrow the host selection")
		}
		var data struct {
			OK   bool `json:"ok"`
			Data struct {
				Results []any `json:"results"`
				Partial bool  `json:"partial"`
			} `json:"data"`
		}
		if e != nil || resp.StatusCode != 200 || json.Unmarshal(b, &data) != nil || !data.OK {
			partial = true
			results = append(results, failedHost(host))
			continue
		}
		partial = partial || data.Data.Partial
		results = append(results, data.Data.Results...)
	}
	b, _ := json.Marshal(map[string]any{"ok": true, "data": map[string]any{"results": results, "partial": partial}})
	return &http.Response{StatusCode: 200, Status: "200 OK", Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(bytes.NewReader(b)), ContentLength: int64(len(b)), Request: r}, nil
}
