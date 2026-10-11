// Package mcp2clirt extracts the pinned MCP runtime shared by service CLIs.
package mcp2clirt

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

var runtimeSHA256 string

func Path(cacheDir string) (string, error) {
	if len(embeddedRuntime) == 0 || len(runtimeSHA256) != sha256.Size*2 {
		return "", fmt.Errorf("embedded mcp2cli runtime is unavailable")
	}
	dir := filepath.Join(cacheDir, "runtime")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("create runtime cache: %w", err)
	}
	path := filepath.Join(dir, "mcp2cli-"+runtimeSHA256[:16])
	if validRuntime(path, runtimeSHA256) {
		return path, nil
	}
	return extractRuntime(path, runtimeSHA256)
}

func validRuntime(path, expected string) bool {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return false
	}
	file, err := os.Open(path)
	if err != nil {
		return false
	}
	defer file.Close()
	sum := sha256.New()
	if _, err := io.Copy(sum, file); err != nil || fmt.Sprintf("%x", sum.Sum(nil)) != expected {
		return false
	}
	return os.Chmod(path, 0o700) == nil
}

func extractRuntime(path, expected string) (string, error) {
	reader, err := gzip.NewReader(bytes.NewReader(embeddedRuntime))
	if err != nil {
		return "", fmt.Errorf("open embedded mcp2cli runtime: %w", err)
	}
	defer reader.Close()
	tmp, err := os.CreateTemp(filepath.Dir(path), ".mcp2cli-*.tmp")
	if err != nil {
		return "", err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if err := tmp.Chmod(0o700); err != nil {
		tmp.Close()
		return "", err
	}
	sum := sha256.New()
	_, copyErr := io.Copy(io.MultiWriter(tmp, sum), reader)
	closeErr := tmp.Close()
	if copyErr != nil {
		return "", fmt.Errorf("extract embedded mcp2cli runtime: %w", copyErr)
	}
	if closeErr != nil {
		return "", closeErr
	}
	if actual := fmt.Sprintf("%x", sum.Sum(nil)); actual != expected {
		return "", fmt.Errorf("embedded mcp2cli checksum mismatch")
	}
	if err := os.Rename(tmpPath, path); err != nil {
		if validRuntime(path, expected) {
			return path, nil
		}
		return "", fmt.Errorf("install embedded mcp2cli runtime: %w", err)
	}
	return path, nil
}
