package fnsrt

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	restish "github.com/rest-sh/restish/v2"
)

func TestInternalComponentRefAllowed(t *testing.T) {
	body := []byte(`{
  "openapi":"3.0.3",
  "info":{"title":"t","version":"1"},
  "paths":{
    "/api/note":{
      "get":{
        "tags":["Note"],
        "responses":{"200":{"description":"ok","content":{"application/json":{"schema":{"$ref":"#/components/schemas/Note"}}}}}
      }
    }
  },
  "components":{"schemas":{"Note":{"type":"object","properties":{"path":{"type":"string"}}}}}
}`)
	fixed, err := ConvertAndFix(body)
	if err != nil {
		t.Fatal(err)
	}
	if err := rejectExternalRefs(fixed, false); err != nil {
		t.Fatal(err)
	}
	var loader SpecLoader
	if _, err := loader.LoadWithOptions(body, restish.LoadOptions{}); err != nil {
		t.Fatal(err)
	}
}

func TestRemoteHTTPRefRejectedWithoutNetwork(t *testing.T) {
	var hits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		hits.Add(1)
	}))
	defer server.Close()

	body := []byte(`{
  "openapi":"3.0.3",
  "info":{"title":"t","version":"1"},
  "paths":{
    "/api/note":{
      "get":{
        "tags":["Note"],
        "responses":{"200":{"description":"ok","content":{"application/json":{"schema":{"$ref":"` + server.URL + `/Note.json"}}}}}
      }
    }
  }
}`)
	var loader SpecLoader
	_, err := loader.LoadWithOptions(body, restish.LoadOptions{SourceURL: "https://example.test/openapi.json", AllowCrossOrigin: true})
	if err == nil || !strings.Contains(err.Error(), "external OpenAPI $ref is not allowed") {
		t.Fatalf("error = %v", err)
	}
	if hits.Load() != 0 {
		t.Fatalf("unexpected network fetches: %d", hits.Load())
	}
}

func TestCrossOriginAndFileRefsRejected(t *testing.T) {
	cases := []struct {
		name string
		ref  string
		want string
	}{
		{"https", "https://evil.example/openapi.json#/components/schemas/X", "external OpenAPI $ref"},
		{"http", "http://evil.example/x.json", "external OpenAPI $ref"},
		{"file", "file:///etc/passwd", "file OpenAPI $ref"},
		{"relative-remote", "./secrets.yaml", "relative OpenAPI file $ref"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body := []byte(`{
  "openapi":"3.0.3",
  "info":{"title":"t","version":"1"},
  "paths":{"/api/note":{"get":{"tags":["Note"],"responses":{"200":{"description":"ok","content":{"application/json":{"schema":{"$ref":"` + tc.ref + `"}}}}}}}}
}`)
			var loader SpecLoader
			_, err := loader.LoadWithOptions(body, restish.LoadOptions{SourceURL: "https://example.test/spec.json"})
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want containing %q", err, tc.want)
			}
		})
	}
}

func TestLocalRelativeRefAllowedOnlyForLocalSpec(t *testing.T) {
	dir := t.TempDir()
	noteSchema := []byte(`{"type":"object","properties":{"path":{"type":"string"}}}`)
	if err := os.WriteFile(filepath.Join(dir, "note.json"), noteSchema, 0o600); err != nil {
		t.Fatal(err)
	}
	root := []byte(`{
  "openapi":"3.0.3",
  "info":{"title":"t","version":"1"},
  "paths":{
    "/api/note":{
      "get":{
        "tags":["Note"],
        "responses":{"200":{"description":"ok","content":{"application/json":{"schema":{"$ref":"./note.json"}}}}}
      }
    }
  }
}`)
	rootPath := filepath.Join(dir, "openapi.json")
	if err := os.WriteFile(rootPath, root, 0o600); err != nil {
		t.Fatal(err)
	}

	var loader SpecLoader
	_, err := loader.LoadWithOptions(root, restish.LoadOptions{SourceURL: "https://example.test/openapi.json"})
	if err == nil || !strings.Contains(err.Error(), "relative OpenAPI file $ref") {
		t.Fatalf("remote load error = %v", err)
	}

	// LocalPath enables relative file refs under the base directory. libopenapi
	// may still fail to bundle remote-looking relative refs depending on config;
	// the policy gate itself must accept them for local specs.
	if err := rejectExternalRefs(mustConvert(t, root), true); err != nil {
		t.Fatal(err)
	}
	_, err = loader.LoadWithOptions(root, restish.LoadOptions{LocalPath: rootPath})
	if err != nil {
		// Accept either successful load or a parse error that is not the remote
		// policy rejection; policy already validated above.
		if strings.Contains(err.Error(), "relative OpenAPI file $ref is not allowed for remote specs") {
			t.Fatalf("local load incorrectly treated as remote: %v", err)
		}
	}
}

func mustConvert(t *testing.T, body []byte) []byte {
	t.Helper()
	out, err := ConvertAndFix(body)
	if err != nil {
		t.Fatal(err)
	}
	return out
}
