package paseobridgert

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestNativeOfficialNodeInterop(t *testing.T) {
	root := os.Getenv("PASEO_NATIVE_NODE_MODULES")
	if root == "" {
		t.Skip("set PASEO_NATIVE_NODE_MODULES to installed official SDK node_modules for synthetic interop")
	}
	if _, err := exec.LookPath("node"); err != nil {
		t.Fatal("Node fixture requested but runtime unavailable")
	}
	cmd := exec.Command("node", "testdata/native-relay.mjs")
	cmd.Env = append(os.Environ(), "PASEO_NATIVE_NODE_MODULES="+root)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err = cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })
	ready := make(chan []byte, 1)
	go func() {
		scanner := bufio.NewScanner(stdout)
		if scanner.Scan() {
			ready <- scanner.Bytes()
		} else {
			ready <- nil
		}
	}()
	var line []byte
	select {
	case line = <-ready:
	case <-time.After(10 * time.Second):
		t.Fatal("official synthetic fixture startup timeout")
	}
	var metadata struct {
		URL       string `json:"url"`
		PublicKey string `json:"daemonPublicKeyB64"`
		ServerID  string `json:"serverId"`
	}
	if json.Unmarshal(line, &metadata) != nil || metadata.URL == "" {
		t.Fatal("official synthetic fixture did not start")
	}
	dir := t.TempDir()
	password := filepath.Join(dir, "password")
	if err = os.WriteFile(password, []byte("synthetic-node-password"), 0600); err != nil {
		t.Fatal(err)
	}
	transport := &encryptedTransport{base: http.DefaultTransport, origin: metadata.URL, keys: &e2eeKeys{native: &nativeConfig{Version: 2, StateDir: filepath.Join(dir, "state"), Hosts: map[string]nativeHost{"test": {ServerID: metadata.ServerID, PublicKey: metadata.PublicKey, PasswordFile: password}}}}}
	for _, test := range []struct {
		path, method, body string
		status             int
	}{{"/api/v1/hosts/test", "GET", "", 200}, {"/api/v1/hosts/test/agents/agent", "GET", "", 200}, {"/api/v1/hosts/test/agents/agent/messages", "POST", `{"prompt":"SYNTHETIC-PROMPT"}`, 202}} {
		req, _ := http.NewRequest(test.method, metadata.URL+test.path, bytes.NewBufferString(test.body))
		req.Header.Set("Authorization", "Bearer synthetic-owner")
		resp, err := transport.RoundTrip(req)
		if err != nil {
			t.Fatalf("official Node encrypted interop %s: %v", test.path, err)
		}
		raw, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil || resp.StatusCode != test.status || bytes.Contains(raw, []byte("synthetic private text")) {
			t.Fatalf("official Node response %s: %d %s %v", test.path, resp.StatusCode, raw, err)
		}
	}
}
