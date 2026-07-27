package swagger2rt

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestConvertMinimalJSONPreservesMultipartAndBinary(t *testing.T) {
	body, err := os.ReadFile(filepath.Join("testdata", "minimal.json"))
	if err != nil {
		t.Fatal(err)
	}
	converted, err := Convert(body)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(converted, &doc); err != nil {
		t.Fatal(err)
	}
	if got, _ := doc["openapi"].(string); !strings.HasPrefix(got, "3.0") {
		t.Fatalf("openapi = %q", got)
	}
	paths, _ := doc["paths"].(map[string]any)
	file, _ := paths["/api/file"].(map[string]any)
	post, _ := file["post"].(map[string]any)
	bodyMedia := requestMedia(t, post)
	if _, ok := bodyMedia["multipart/form-data"]; !ok {
		t.Fatalf("POST content types = %#v", bodyMedia)
	}
	schema := mediaSchema(t, bodyMedia["multipart/form-data"])
	props, _ := schema["properties"].(map[string]any)
	for _, name := range []string{"file", "path", "vault"} {
		if _, ok := props[name]; !ok {
			t.Fatalf("missing form property %q in %#v", name, props)
		}
	}
	fileProp, _ := props["file"].(map[string]any)
	if format, _ := fileProp["format"].(string); format != "binary" {
		t.Fatalf("file format = %#v", fileProp)
	}

	get, _ := file["get"].(map[string]any)
	responses, _ := get["responses"].(map[string]any)
	okResp, _ := responses["200"].(map[string]any)
	content, _ := okResp["content"].(map[string]any)
	if _, ok := content["application/octet-stream"]; !ok {
		t.Fatalf("GET content = %#v", content)
	}
}

func TestConvertMinimalYAML(t *testing.T) {
	body, err := os.ReadFile(filepath.Join("testdata", "minimal.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !IsSwagger2(body) {
		t.Fatal("expected YAML swagger 2 detection")
	}
	converted, err := Convert(body)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(converted, &doc); err != nil {
		t.Fatal(err)
	}
	paths, _ := doc["paths"].(map[string]any)
	if _, ok := paths["/ping"]; !ok {
		t.Fatalf("paths = %#v", paths)
	}
}

func TestConvertFNSCorpus(t *testing.T) {
	for _, name := range []string{"swagger.yaml", "swagger.json"} {
		t.Run(name, func(t *testing.T) {
			body, err := os.ReadFile(filepath.Join("..", "fnsrt", "testdata", name))
			if err != nil {
				t.Fatal(err)
			}
			converted, err := Convert(body)
			if err != nil {
				t.Fatal(err)
			}
			paths, ops := countPathsAndOperations(t, converted)
			if paths != 73 || ops != 92 {
				t.Fatalf("paths=%d ops=%d, want 73/92", paths, ops)
			}
			assertFileUploadDownload(t, converted)
		})
	}
}

func TestConvertRejectsInvalidDocuments(t *testing.T) {
	cases := []struct {
		name string
		body string
		want string
	}{
		{"empty", "", "empty"},
		{"openapi3", `{"openapi":"3.0.3","info":{"title":"x","version":"1"},"paths":{}}`, "not Swagger 2.0"},
		{"broken-json", `{"swagger":`, "decode JSON"},
		{"broken-yaml", "swagger: [\n", "decode YAML"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Convert([]byte(tc.body))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want containing %q", err, tc.want)
			}
		})
	}
}

func TestLoaderDetect(t *testing.T) {
	var loader Loader
	if !loader.Detect("application/json", []byte(`{"swagger":"2.0","info":{"title":"t","version":"1"},"paths":{}}`)) {
		t.Fatal("expected detect swagger 2")
	}
	if loader.Detect("application/json", []byte(`{"openapi":"3.0.3"}`)) {
		t.Fatal("did not expect OpenAPI 3 detection")
	}
}

func countPathsAndOperations(t *testing.T, converted []byte) (paths, ops int) {
	t.Helper()
	var doc map[string]any
	if err := json.Unmarshal(converted, &doc); err != nil {
		t.Fatal(err)
	}
	pathMap, _ := doc["paths"].(map[string]any)
	paths = len(pathMap)
	for _, item := range pathMap {
		pathItem, _ := item.(map[string]any)
		for _, method := range []string{"get", "post", "put", "patch", "delete", "head", "options"} {
			if _, ok := pathItem[method]; ok {
				ops++
			}
		}
	}
	return paths, ops
}

func assertFileUploadDownload(t *testing.T, converted []byte) {
	t.Helper()
	var doc map[string]any
	if err := json.Unmarshal(converted, &doc); err != nil {
		t.Fatal(err)
	}
	paths, _ := doc["paths"].(map[string]any)
	file, _ := paths["/api/file"].(map[string]any)
	post, _ := file["post"].(map[string]any)
	bodyMedia := requestMedia(t, post)
	schema := mediaSchema(t, bodyMedia["multipart/form-data"])
	props, _ := schema["properties"].(map[string]any)
	for _, name := range []string{"file", "path", "vault"} {
		if _, ok := props[name]; !ok {
			t.Fatalf("POST /api/file missing %q", name)
		}
	}
	fileProp, _ := props["file"].(map[string]any)
	if format, _ := fileProp["format"].(string); format != "binary" {
		t.Fatalf("file format = %#v", fileProp)
	}
	get, _ := file["get"].(map[string]any)
	responses, _ := get["responses"].(map[string]any)
	okResp, _ := responses["200"].(map[string]any)
	content, _ := okResp["content"].(map[string]any)
	if _, ok := content["application/octet-stream"]; !ok {
		t.Fatalf("GET /api/file content = %#v", content)
	}
}

func requestMedia(t *testing.T, operation map[string]any) map[string]any {
	t.Helper()
	requestBody, _ := operation["requestBody"].(map[string]any)
	content, _ := requestBody["content"].(map[string]any)
	if content == nil {
		t.Fatalf("missing requestBody.content in %#v", operation)
	}
	return content
}

func mediaSchema(t *testing.T, media any) map[string]any {
	t.Helper()
	item, _ := media.(map[string]any)
	schema, _ := item["schema"].(map[string]any)
	if schema == nil {
		t.Fatalf("missing schema in %#v", media)
	}
	return schema
}
