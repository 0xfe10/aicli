package mcp2clirt

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

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

	path, err := Path(t.TempDir())
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
	if again, err := Path(filepath.Dir(filepath.Dir(path))); err != nil || again != path {
		t.Fatalf("runtimePath reuse = %q, %v", again, err)
	}

	if err := os.WriteFile(path, []byte("corrupt"), 0o700); err != nil {
		t.Fatal(err)
	}
	if repaired, err := Path(filepath.Dir(filepath.Dir(path))); err != nil || repaired != path {
		t.Fatalf("runtimePath repair = %q, %v", repaired, err)
	}
	if data, err := os.ReadFile(path); err != nil || !bytes.Equal(data, payload) {
		t.Fatalf("repaired runtime = %q, %v", data, err)
	}
}
