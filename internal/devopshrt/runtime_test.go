package devopshrt

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/0xfe10/aicli/internal/contextflow"
)

func TestRuntimePathPrefersExplicitOverride(t *testing.T) {
	want := filepath.Join(t.TempDir(), "mcp2cli")
	t.Setenv("DEVOPSH_MCP2CLI", want)
	got, err := runtimePath("devopsh", "devopsh")
	if err != nil || got != want {
		t.Fatalf("runtimePath() = %q, %v", got, err)
	}
}

func TestRuntimePathFindsSibling(t *testing.T) {
	dir := t.TempDir()
	devopsh := filepath.Join(dir, "devopsh")
	runtime := filepath.Join(dir, "devopsh-mcp2cli")
	if err := os.WriteFile(devopsh, nil, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(runtime, nil, 0o700); err != nil {
		t.Fatal(err)
	}
	wrongDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(wrongDir, "mcp2cli"), nil, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DEVOPSH_MCP2CLI", "")
	t.Setenv("PATH", wrongDir)
	got, err := runtimePath(devopsh, devopsh)
	if err != nil || got != runtime {
		t.Fatalf("runtimePath() = %q, %v", got, err)
	}
}

func TestRuntimePathFindsShimSibling(t *testing.T) {
	root := t.TempDir()
	shimDir, releaseDir := filepath.Join(root, "shims"), filepath.Join(root, "release")
	if err := os.MkdirAll(shimDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(releaseDir, 0o700); err != nil {
		t.Fatal(err)
	}
	realDevopsh := filepath.Join(releaseDir, "devopsh")
	if err := os.WriteFile(realDevopsh, nil, 0o700); err != nil {
		t.Fatal(err)
	}
	shim := filepath.Join(shimDir, "devopsh")
	if err := os.Symlink(realDevopsh, shim); err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(shimDir, "devopsh-mcp2cli")
	if err := os.WriteFile(want, nil, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DEVOPSH_MCP2CLI", "")
	got, err := runtimePath(shim, realDevopsh)
	if err != nil || got != want {
		t.Fatalf("runtimePath() = %q, %v", got, err)
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
