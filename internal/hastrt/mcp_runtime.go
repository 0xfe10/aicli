package hastrt

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/0xfe10/aicli/internal/contextflow"
	"github.com/0xfe10/aicli/internal/mcp2clirt"
	"gopkg.in/yaml.v3"
)

// RunMCPWithContext keeps existing auth/context state while exposing MCP commands.
func RunMCPWithContext(args []string, selection contextflow.Selection, version, commit string) error {
	return runMCP(args, selection, version, commit, os.Stdout, os.Stderr)
}

func runMCP(args []string, selection contextflow.Selection, version, commit string, stdout, stderr io.Writer) error {
	if len(args) == 2 && (args[1] == "--version" || args[1] == "version") {
		fmt.Fprintf(stdout, "hast version %s (%s)\n", version, commit)
		return nil
	}
	session, _, err := LoadSessionWithContext(selection)
	if err != nil {
		return err
	}
	if !session.HasCredentials {
		return fmt.Errorf("Hast authentication is not configured; run hast auth login --mode key")
	}
	filtered, output, err := mcpArguments(args)
	if err != nil {
		return err
	}
	mode := strings.ToLower(strings.TrimSpace(os.Getenv("HAST_WRITE_MODE")))
	if mode == "" {
		mode = "readonly"
	}
	if mode != "readonly" && mode != "write" && mode != "destructive" {
		return fmt.Errorf("invalid HAST_WRITE_MODE %q: expected readonly, write, or destructive", mode)
	}
	proxy, err := NewMCPProxy(session, mode)
	if err != nil {
		return err
	}
	defer proxy.Close()
	identity := fmt.Sprintf("%x", sha256.Sum256([]byte(session.BaseURL+"\x00"+session.Credentials.APIKey)))
	cacheDir := filepath.Join(selection.CacheDir, "mcp", identity)
	if err := os.MkdirAll(cacheDir, 0700); err != nil {
		return err
	}
	configDir, err := os.MkdirTemp(cacheDir, "run-")
	if err != nil {
		return err
	}
	defer os.Remove(configDir)
	configPath := filepath.Join(configDir, "hast.yaml")
	defer os.Remove(configPath)
	config := map[string]interface{}{
		"schema_version": 1,
		"app":            map[string]interface{}{"profile": "bridge"},
		"server":         map[string]interface{}{"display_name": "Hast", "transport": "streamable_http", "endpoint": proxy.Endpoint, "protocol_version": "auto"},
		"defaults":       map[string]interface{}{"output": "json", "timeout_seconds": 120},
		"branding":       map[string]interface{}{"name": "hast", "about": "Hast AIP MCP CLI", "version": version + " (" + commit + ")", "builtin_commands": []string{"ls", "inspect", "ping", "doctor", "tool"}},
		"discovery":      map[string]interface{}{"auto": true, "ttl_seconds": 300},
		"telemetry":      map[string]interface{}{"enabled": false},
		"events":         map[string]interface{}{"enable_stdio_events": false},
	}
	data, err := yaml.Marshal(config)
	if err != nil {
		return err
	}
	if err := os.WriteFile(configPath, data, 0600); err != nil {
		return err
	}
	runtime := strings.TrimSpace(os.Getenv("HAST_MCP2CLI"))
	if runtime == "" {
		runtime, err = mcp2clirt.Path(selection.CacheDir)
	}
	if err != nil {
		return err
	}
	cmd := exec.Command(runtime, append([]string{"--json"}, filtered...)...)
	cmd.Stdin = os.Stdin
	var out, diagnostic bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &diagnostic
	env := make([]string, 0, len(os.Environ()))
	for _, value := range os.Environ() {
		if !strings.HasPrefix(strings.ToUpper(value), "MCP2CLI_") && !strings.HasPrefix(value, "HAST_API_KEY=") {
			env = append(env, value)
		}
	}
	cmd.Env = append(env, "MCP2CLI_CONFIG="+configPath, "MCP2CLI_DATA_DIR="+cacheDir, "MCP2CLI_INVOKED_AS=hast", "MCP2CLI_TELEMETRY=off")
	runErr := cmd.Run()
	redact := func(s string) string { return strings.ReplaceAll(s, session.Credentials.APIKey, "***") }
	if diagnostic.Len() > 0 {
		fmt.Fprint(stderr, redact(diagnostic.String()))
	}
	payload := []byte(redact(out.String()))
	if err := renderMCPOutput(stdout, payload, output); err != nil {
		return err
	}
	if err := proxy.Err(); err != nil {
		return fmt.Errorf("Hast MCP request failed: %s", redact(err.Error()))
	}
	if mcpResultFailed(payload) {
		return fmt.Errorf("Hast MCP tool returned an error; inspect the response")
	}
	if runErr != nil {
		return fmt.Errorf("Hast MCP command failed: %w", runErr)
	}
	return nil
}

func mcpArguments(args []string) ([]string, string, error) {
	result := []string{}
	output := "human"
	for i := 1; i < len(args); i++ {
		arg := args[i]
		if arg == "--json" {
			output = "json"
			continue
		}
		if arg == "--output" {
			if i+1 >= len(args) {
				return nil, "", fmt.Errorf("--output requires human, json, or ndjson")
			}
			i++
			output = args[i]
			continue
		}
		if strings.HasPrefix(arg, "--output=") {
			output = strings.TrimPrefix(arg, "--output=")
			continue
		}
		for _, flag := range []string{"--url", "--stdio", "--config", "--config-name", "--profile", "--daemon", "--name"} {
			if arg == flag || strings.HasPrefix(arg, flag+"=") {
				return nil, "", fmt.Errorf("%s is not supported; select the Hast endpoint with auth or HAST_BASE_URL", flag)
			}
		}
		result = append(result, arg)
	}
	if output != "human" && output != "json" && output != "ndjson" {
		return nil, "", fmt.Errorf("unsupported output format %q", output)
	}
	if len(result) == 0 {
		result = append(result, "--help")
	}
	return result, output, nil
}

func mcpResultFailed(raw []byte) bool {
	var value interface{}
	if json.Unmarshal(raw, &value) != nil {
		return false
	}
	var failed func(interface{}) bool
	failed = func(v interface{}) bool {
		switch x := v.(type) {
		case map[string]interface{}:
			if x["isError"] == true {
				return true
			}
			// Inspect protocol result/status containers, not arbitrary business fields.
			if status, ok := x["status"].(float64); ok && status >= 400 {
				return true
			}
			for _, key := range []string{"result", "data", "structuredContent"} {
				if failed(x[key]) {
					return true
				}
			}
		}
		return false
	}
	return failed(value)
}

func renderMCPOutput(w io.Writer, raw []byte, output string) error {
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil
	}
	var obj map[string]interface{}
	if json.Unmarshal(raw, &obj) != nil {
		_, err := w.Write(raw)
		return err
	}
	if output == "json" {
		_, err := w.Write(raw)
		return err
	}
	if output == "ndjson" {
		compact := new(bytes.Buffer)
		if err := json.Compact(compact, raw); err != nil {
			return err
		}
		_, err := fmt.Fprintln(w, compact.String())
		return err
	}
	if data, ok := obj["data"].(map[string]interface{}); ok {
		if help, ok := data["help"].(string); ok {
			_, err := fmt.Fprint(w, help)
			return err
		}
		if result, ok := data["result"].(map[string]interface{}); ok {
			if structured, ok := result["structuredContent"]; ok {
				rendered, err := json.MarshalIndent(structured, "", "  ")
				if err != nil {
					return err
				}
				_, err = fmt.Fprintln(w, string(rendered))
				return err
			}
		}
	}
	if lines, ok := obj["lines"].([]interface{}); ok {
		for _, line := range lines {
			fmt.Fprintln(w, line)
		}
		return nil
	}
	_, err := w.Write(raw)
	return err
}
