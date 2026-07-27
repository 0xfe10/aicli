package fnsrt

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFixSpecFiltersAndRewritesAuth(t *testing.T) {
	body, err := os.ReadFile(filepath.Join("testdata", "swagger.json"))
	if err != nil {
		t.Fatal(err)
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
		t.Fatal("servers should be removed")
	}
	paths, _ := doc["paths"].(map[string]any)
	for path := range paths {
		if !pathAllowed(path) {
			t.Fatalf("unexpected path kept: %s", path)
		}
	}
	for _, blocked := range []string{"/api/vault", "/api/vault/get", "/api/admin/config", "/api/auth/logout", "/api/share", "/api/backup/configs", "/api/storage", "/api/webgui"} {
		if _, ok := paths[blocked]; ok {
			t.Fatalf("blocked path still present: %s", blocked)
		}
	}
	for _, required := range []string{"/api/note", "/api/file", "/api/folder", "/api/notes", "/api/files", "/api/folders"} {
		if _, ok := paths[required]; !ok {
			t.Fatalf("missing required path: %s", required)
		}
	}
	schemes, _ := doc["components"].(map[string]any)["securitySchemes"].(map[string]any)
	user, _ := schemes[securitySchemeName].(map[string]any)
	if user["type"] != "http" || user["scheme"] != "bearer" {
		t.Fatalf("security scheme = %#v", user)
	}
	if _, ok := schemes["ShareAuthToken"]; ok {
		t.Fatal("ShareAuthToken should be removed")
	}
	noteGet, _ := paths["/api/note"].(map[string]any)["get"].(map[string]any)
	for _, raw := range noteGet["parameters"].([]any) {
		param := raw.(map[string]any)
		if param["name"] == "token" {
			t.Fatal("token header parameter should be removed")
		}
	}
}

func TestCommandTreeSnapshot(t *testing.T) {
	body, err := os.ReadFile(filepath.Join("testdata", "swagger.json"))
	if err != nil {
		t.Fatal(err)
	}
	fixed, err := ConvertAndFix(body)
	if err != nil {
		t.Fatal(err)
	}
	tree, err := CommandTree(fixed)
	if err != nil {
		t.Fatal(err)
	}
	snapshotPath := filepath.Join("testdata", "command_tree_v1.txt")
	wantBytes, err := os.ReadFile(snapshotPath)
	if err != nil {
		if os.IsNotExist(err) {
			if writeErr := os.WriteFile(snapshotPath, []byte(strings.Join(tree, "\n")+"\n"), 0o644); writeErr != nil {
				t.Fatal(writeErr)
			}
			t.Fatalf("wrote initial command tree snapshot to %s; re-run test", snapshotPath)
		}
		t.Fatal(err)
	}
	want := strings.Split(strings.TrimSuffix(string(wantBytes), "\n"), "\n")
	if len(want) == 1 && want[0] == "" {
		want = nil
	}
	if strings.Join(tree, "\n") != strings.Join(want, "\n") {
		t.Fatalf("command tree changed\n got:\n%s\nwant:\n%s", strings.Join(tree, "\n"), strings.Join(want, "\n"))
	}
	for _, entry := range tree {
		if strings.Contains(entry, " token") || strings.HasSuffix(entry, "token") {
			t.Fatalf("command tree should not include token positional args: %q", entry)
		}
		parts := strings.SplitN(entry, " ", 2)
		if len(parts) != 2 {
			t.Fatalf("bad entry %q", entry)
		}
		if parts[0] == "vault" || parts[0] == "admin" || parts[0] == "auth" || parts[0] == "backup" || parts[0] == "storage" || parts[0] == "webgui" {
			t.Fatalf("unexpected management command group %q", entry)
		}
	}
}
