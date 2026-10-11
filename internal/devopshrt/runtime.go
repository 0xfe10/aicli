// Package devopshrt launches the pinned mcp2cli runtime for DevOpsH.
package devopshrt

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/0xfe10/aicli/internal/contextflow"
	"github.com/0xfe10/aicli/internal/mcp2clirt"
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
	return mcp2clirt.Path(cacheDir)
}
