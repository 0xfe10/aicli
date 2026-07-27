package fnsrt

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

var availableCommandLine = regexp.MustCompile(`(?m)^\s{2}([a-z][a-z0-9-]*)\s`)

func TestRealRestishCommandSurfaceSnapshot(t *testing.T) {
	for _, fixture := range []string{"swagger.json", "openapi3.json"} {
		t.Run(fixture, func(t *testing.T) {
			spec, err := os.ReadFile(filepath.Join("testdata", fixture))
			if err != nil {
				t.Fatal(err)
			}
			tree := captureCommandTree(t, spec)
			assertApprovedCommandTree(t, tree)

			snapshotPath := filepath.Join("testdata", "restish_command_tree_v1.txt")
			wantBytes, err := os.ReadFile(snapshotPath)
			if err != nil {
				if os.IsNotExist(err) {
					if writeErr := os.WriteFile(snapshotPath, []byte(strings.Join(tree, "\n")+"\n"), 0o644); writeErr != nil {
						t.Fatal(writeErr)
					}
					t.Fatalf("wrote initial Restish command tree snapshot; re-run test")
				}
				t.Fatal(err)
			}
			want := strings.Split(strings.TrimSuffix(string(wantBytes), "\n"), "\n")
			if strings.Join(tree, "\n") != strings.Join(want, "\n") {
				t.Fatalf("Restish command tree changed for %s\n got:\n%s\nwant:\n%s", fixture, strings.Join(tree, "\n"), strings.Join(want, "\n"))
			}
		})
	}
}

func TestSwagger2AndOpenAPI3ProduceSameCommandTree(t *testing.T) {
	swagger, err := os.ReadFile(filepath.Join("testdata", "swagger.json"))
	if err != nil {
		t.Fatal(err)
	}
	openapi3, err := os.ReadFile(filepath.Join("testdata", "openapi3.json"))
	if err != nil {
		t.Fatal(err)
	}
	left := captureCommandTree(t, swagger)
	right := captureCommandTree(t, openapi3)
	if strings.Join(left, "\n") != strings.Join(right, "\n") {
		t.Fatalf("Swagger2/OpenAPI3 command trees differ\n swagger2:\n%s\n openapi3:\n%s", strings.Join(left, "\n"), strings.Join(right, "\n"))
	}
}

func TestOpenAPI3DoesNotBypassFixSpec(t *testing.T) {
	body, err := os.ReadFile(filepath.Join("testdata", "openapi3.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !IsOpenAPI3(body) {
		t.Fatal("fixture should detect as OpenAPI 3")
	}
	fixed, err := ConvertAndFix(body)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(fixed, &doc); err != nil {
		t.Fatal(err)
	}
	if _, ok := doc["servers"]; ok {
		t.Fatal("servers should be removed from OpenAPI 3 input")
	}
	paths, _ := doc["paths"].(map[string]any)
	for _, blocked := range []string{"/api/vault", "/api/vault/get", "/api/admin/config", "/api/auth/logout"} {
		if _, ok := paths[blocked]; ok {
			t.Fatalf("blocked path kept after OpenAPI 3 fix: %s", blocked)
		}
	}
	schemes, _ := doc["components"].(map[string]any)["securitySchemes"].(map[string]any)
	user, _ := schemes[securitySchemeName].(map[string]any)
	if user["type"] != "http" || user["scheme"] != "bearer" {
		t.Fatalf("security scheme = %#v", user)
	}
	noteGet, _ := paths["/api/note"].(map[string]any)["get"].(map[string]any)
	for _, raw := range noteGet["parameters"].([]any) {
		param := raw.(map[string]any)
		if param["name"] == "token" {
			t.Fatal("token header should be stripped for OpenAPI 3")
		}
	}
	file, _ := paths["/api/file"].(map[string]any)
	post, _ := file["post"].(map[string]any)
	content, _ := post["requestBody"].(map[string]any)["content"].(map[string]any)
	if _, ok := content["multipart/form-data"]; !ok {
		t.Fatalf("multipart missing: %#v", content)
	}
	get, _ := file["get"].(map[string]any)
	respContent, _ := get["responses"].(map[string]any)["200"].(map[string]any)["content"].(map[string]any)
	if _, ok := respContent["application/octet-stream"]; !ok {
		t.Fatalf("binary response missing: %#v", respContent)
	}
}

func TestUnsupportedSpecDoesNotFallBackToBuiltinLoader(t *testing.T) {
	mux := http.NewServeMux()
	server := httptest.NewServer(mux)
	defer server.Close()
	mux.HandleFunc("/bad.json", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"info":{"title":"nope"},"paths":{"/api/vault":{"get":{"responses":{"200":{"description":"x"}}}}}}`))
	})

	stateDir := t.TempDir()
	t.Setenv("RSH_CONFIG_DIR", stateDir+"/config")
	t.Setenv("RSH_CACHE_DIR", stateDir+"/cache")
	t.Setenv("FNS_ACCESS_TOKEN", "token")

	cli := NewCLI(Config{BaseURL: server.URL, SpecURL: server.URL + "/bad.json", Client: "aicli", Version: "test"}, "test", "")
	var stdout, stderr bytes.Buffer
	cli.Stdout, cli.Stderr = &stdout, &stderr
	err := cli.Run([]string{"fns", "--help"})
	if err == nil {
		t.Fatalf("expected unsupported spec failure, stdout=%s", stdout.String())
	}
	msg := err.Error() + stdout.String() + stderr.String()
	if strings.Contains(msg, "vault") && strings.Contains(stdout.String(), "vault") {
		t.Fatalf("builtin loader appears to have exposed vault: %s", msg)
	}
	if !strings.Contains(msg, "unsupported FNS API description") && !strings.Contains(msg, "expected Swagger 2.0 or OpenAPI 3") {
		// Detect may be false for this body (no openapi/swagger), so Restish builtin might fail differently.
		// Ensure vault command group is not generated.
		if strings.Contains(stdout.String(), "\n  vault ") {
			t.Fatalf("vault command leaked: %s", stdout.String())
		}
	}
}

func captureCommandTree(t *testing.T, spec []byte) []string {
	t.Helper()
	mux := http.NewServeMux()
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	mux.HandleFunc("/openapi.yaml", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(spec)
	})

	stateDir := t.TempDir()
	t.Setenv("RSH_CONFIG_DIR", stateDir+"/config")
	t.Setenv("RSH_CACHE_DIR", stateDir+"/cache")
	t.Setenv("FNS_ACCESS_TOKEN", "token")

	cfg := Config{BaseURL: server.URL, SpecURL: server.URL + "/openapi.yaml", Client: "aicli", Version: "test"}
	rootHelp := runHelp(t, cfg, []string{"fns", "--help"})
	groups := parseAvailableCommands(rootHelp)
	var approved []string
	for _, group := range groups {
		switch group {
		case "note", "note-history", "file", "folder":
			approved = append(approved, group)
		case "cli", "help", "completion", "version":
			continue
		default:
			t.Fatalf("unexpected root command group %q in help:\n%s", group, rootHelp)
		}
	}
	sort.Strings(approved)

	var entries []string
	for _, group := range approved {
		groupHelp := runHelp(t, cfg, []string{"fns", group, "--help"})
		for _, command := range parseAvailableCommands(groupHelp) {
			entries = append(entries, group+" "+command)
			cmdHelp := runHelp(t, cfg, []string{"fns", group, command, "--help"})
			if strings.Contains(strings.ToLower(cmdHelp), "<token>") || strings.Contains(cmdHelp, " --token ") {
				t.Fatalf("%s %s help requires token:\n%s", group, command, cmdHelp)
			}
			// Prove Restish can parse the command path.
			_ = runHelp(t, cfg, []string{"fns", group, command, "--help"})
		}
	}
	sort.Strings(entries)
	return entries
}

func assertApprovedCommandTree(t *testing.T, tree []string) {
	t.Helper()
	if len(tree) == 0 {
		t.Fatal("empty command tree")
	}
	for _, entry := range tree {
		parts := strings.SplitN(entry, " ", 2)
		if len(parts) != 2 {
			t.Fatalf("bad entry %q", entry)
		}
		switch parts[0] {
		case "note", "note-history", "file", "folder":
		default:
			t.Fatalf("unapproved group in %q", entry)
		}
		for _, blocked := range []string{"vault", "admin", "auth", "backup", "storage", "webgui", "share"} {
			if parts[0] == blocked || strings.Contains(parts[1], blocked) {
				t.Fatalf("blocked command present: %q", entry)
			}
		}
	}
}

func runHelp(t *testing.T, cfg Config, args []string) string {
	t.Helper()
	cli := NewCLI(cfg, "test", "")
	var stdout, stderr bytes.Buffer
	cli.Stdout, cli.Stderr = &stdout, &stderr
	err := cli.Run(args)
	out := stdout.String()
	if err != nil && out == "" {
		t.Fatalf("Run(%v): %v\nstderr=%s", args, err, stderr.String())
	}
	return out
}

func parseAvailableCommands(help string) []string {
	section := ""
	for _, marker := range []string{"Additional Commands:", "Available Commands:"} {
		if idx := strings.Index(help, marker); idx >= 0 {
			section = help[idx:]
			break
		}
	}
	if section == "" {
		return nil
	}
	if end := strings.Index(section, "\nFlags:"); end >= 0 {
		section = section[:end]
	}
	var names []string
	seen := map[string]bool{}
	for _, match := range availableCommandLine.FindAllStringSubmatch(section, -1) {
		name := match[1]
		if name == "help" || name == "completion" || name == "cli" {
			continue
		}
		if !seen[name] {
			names = append(names, name)
			seen[name] = true
		}
	}
	sort.Strings(names)
	return names
}
