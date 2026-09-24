// Package devopshrt launches the pinned mcp2cli runtime for DevOpsH.
package devopshrt

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/0xfe10/aicli/internal/contextflow"
)

const config = `schema_version: 1
app:
  profile: bridge
server:
  display_name: DevOpsH
  transport: streamable_http
  endpoint: https://backend-stg.hast.so/api/public/v1/mcp
  protocol_version: auto
defaults:
  output: human
  timeout_seconds: 120
events:
  enable_stdio_events: false
branding:
  name: devopsh
  about: DevOpsH MCP command line client
  builtin_commands: [auth, ls, ping, doctor, inspect, tool]
discovery:
  auto: true
`

var manager = contextflow.New("devopsh", "DEVOPSH_CONTEXT")
var runtimeSHA256 string

func ContextManager() contextflow.Manager { return manager }

// Run delegates the dynamic command surface and bearer-token storage to mcp2cli.
func Run(args []string, selection contextflow.Selection) error {
	if selection.CacheDir == "" {
		return fmt.Errorf("devopsh cache directory is unavailable")
	}
	configPath := filepath.Join(selection.CacheDir, "mcp2cli.yaml")
	if err := os.WriteFile(configPath, []byte(config), 0o600); err != nil {
		return fmt.Errorf("write devopsh runtime config: %w", err)
	}
	runtime, err := runtimePath(selection.CacheDir)
	if err != nil {
		return err
	}
	cmd := exec.Command(runtime, args[1:]...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	env := make([]string, 0, len(os.Environ())+4)
	for _, value := range os.Environ() {
		if !strings.HasPrefix(strings.ToUpper(value), "MCP2CLI_") {
			env = append(env, value)
		}
	}
	cmd.Env = append(env,
		"MCP2CLI_CONFIG="+configPath,
		"MCP2CLI_DATA_DIR="+selection.CacheDir,
		"MCP2CLI_INVOKED_AS=devopsh",
		"MCP2CLI_TELEMETRY=off",
	)
	if err := cmd.Run(); err != nil {
		if exit, ok := err.(*exec.ExitError); ok {
			return exit
		}
		return fmt.Errorf("run mcp2cli: %w", err)
	}
	return nil
}

func runtimePath(cacheDir string) (string, error) {
	if path := strings.TrimSpace(os.Getenv("DEVOPSH_MCP2CLI")); path != "" {
		return path, nil
	}
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
