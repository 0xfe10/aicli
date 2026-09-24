// Package devopshrt launches the pinned mcp2cli runtime for DevOpsH.
package devopshrt

import (
	"fmt"
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
	executable, err := os.Executable()
	if err != nil {
		return fmt.Errorf("locate devopsh executable: %w", err)
	}
	runtime, err := runtimePath(args[0], executable)
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

func runtimePath(invokedAs, executable string) (string, error) {
	if path := strings.TrimSpace(os.Getenv("DEVOPSH_MCP2CLI")); path != "" {
		return path, nil
	}
	const runtimeName = "devopsh-mcp2cli"
	invokedPath := invokedAs
	if !strings.ContainsRune(invokedPath, os.PathSeparator) {
		if path, err := exec.LookPath(invokedPath); err == nil {
			invokedPath = path
		}
	}
	if absolute, err := filepath.Abs(invokedPath); err == nil {
		invokedPath = absolute
	}
	if path := siblingRuntime(invokedPath, runtimeName); path != "" {
		return path, nil
	}
	realExecutable, err := filepath.EvalSymlinks(executable)
	if err != nil {
		return "", fmt.Errorf("resolve devopsh executable: %w", err)
	}
	if path := siblingRuntime(realExecutable, runtimeName); path != "" {
		return path, nil
	}
	return "", fmt.Errorf("%s runtime not found beside devopsh; set DEVOPSH_MCP2CLI for local development", runtimeName)
}

func siblingRuntime(executable, name string) string {
	path := filepath.Join(filepath.Dir(executable), name)
	if info, err := os.Stat(path); err == nil && info.Mode().IsRegular() {
		return path
	}
	return ""
}
