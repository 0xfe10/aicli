package devopshrt

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/0xfe10/aicli/internal/contextflow"
)

func TestRuntimePathPrefersExplicitOverride(t *testing.T) {
	want := filepath.Join(t.TempDir(), "mcp2cli")
	t.Setenv("DEVOPSH_MCP2CLI", want)
	got, err := runtimePath(t.TempDir())
	if err != nil || got != want {
		t.Fatalf("runtimePath() = %q, %v", got, err)
	}
}

func TestRuntimePathExtractsAndReusesEmbeddedRuntime(t *testing.T) {
	payload := []byte("embedded runtime")
	var compressed bytes.Buffer
	writer := gzip.NewWriter(&compressed)
	if _, err := writer.Write(payload); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	oldRuntime, oldSHA := embeddedRuntime, runtimeSHA256
	t.Cleanup(func() { embeddedRuntime, runtimeSHA256 = oldRuntime, oldSHA })
	embeddedRuntime = compressed.Bytes()
	runtimeSHA256 = fmt.Sprintf("%x", sha256.Sum256(payload))
	t.Setenv("DEVOPSH_MCP2CLI", "")

	path, err := runtimePath(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(data, payload) {
		t.Fatalf("runtime = %q", data)
	}
	if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0o700 {
		t.Fatalf("runtime mode = %v, %v", info, err)
	}
	if again, err := runtimePath(filepath.Dir(filepath.Dir(path))); err != nil || again != path {
		t.Fatalf("runtimePath reuse = %q, %v", again, err)
	}

	if err := os.WriteFile(path, []byte("corrupt"), 0o700); err != nil {
		t.Fatal(err)
	}
	if repaired, err := runtimePath(filepath.Dir(filepath.Dir(path))); err != nil || repaired != path {
		t.Fatalf("runtimePath repair = %q, %v", repaired, err)
	}
	if data, err := os.ReadFile(path); err != nil || !bytes.Equal(data, payload) {
		t.Fatalf("repaired runtime = %q, %v", data, err)
	}
}

func TestRunFiltersMCP2CLIEnvironment(t *testing.T) {
	dir := t.TempDir()
	runtime := filepath.Join(dir, "runtime")
	output := filepath.Join(dir, "env.json")
	script := "#!/bin/sh\nenv > \"$DEVOPSH_TEST_OUTPUT\"\n"
	if err := os.WriteFile(runtime, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DEVOPSH_MCP2CLI", runtime)
	t.Setenv("DEVOPSH_TEST_OUTPUT", output)
	t.Setenv("MCP2CLI_SERVER__ENDPOINT", "https://attacker.example/mcp")
	selection := contextflow.Selection{Name: "test", CacheDir: filepath.Join(dir, "cache")}
	if err := os.MkdirAll(selection.CacheDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := Run([]string{"devopsh", "--help"}, selection); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	joined := string(data)
	if strings.Contains(joined, "MCP2CLI_SERVER__ENDPOINT=") {
		t.Fatal("unsafe MCP2CLI endpoint override was inherited")
	}
	for _, key := range []string{"MCP2CLI_CONFIG=", "MCP2CLI_DATA_DIR=", "MCP2CLI_INVOKED_AS=devopsh", "MCP2CLI_TELEMETRY=off"} {
		if !strings.Contains(joined, key) {
			t.Fatalf("controlled environment is missing %s", key)
		}
	}
}
