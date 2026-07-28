package fnsrt

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestFixSpecFiltersAndRewritesAuth(t *testing.T) {
	for _, name := range []string{"swagger.json", "openapi3.json", "openapi3.yaml"} {
		t.Run(name, func(t *testing.T) {
			body, err := os.ReadFile(filepath.Join("testdata", name))
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
			for _, blocked := range []string{"/api/vault/get", "/api/admin/config", "/api/auth/logout", "/api/share", "/api/backup/configs", "/api/storage", "/api/webgui"} {
				if _, ok := paths[blocked]; ok {
					t.Fatalf("blocked path still present: %s", blocked)
				}
			}
			vault, ok := paths[vaultListPath].(map[string]any)
			if !ok {
				t.Fatal("missing required vault list path")
			}
			if _, ok := vault["get"]; !ok {
				t.Fatal("missing required vault list operation")
			}
			for _, blockedMethod := range []string{"post", "put", "patch", "delete"} {
				if _, ok := vault[blockedMethod]; ok {
					t.Fatalf("vault management method still present: %s", blockedMethod)
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
		})
	}
}

func TestNormalizeSpecRejectsUnknown(t *testing.T) {
	_, err := NormalizeSpec([]byte(`{"info":{"title":"x"},"paths":{}}`))
	if err == nil {
		t.Fatal("expected error")
	}
}
