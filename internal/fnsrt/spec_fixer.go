package fnsrt

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

const (
	securitySchemeName = "UserAuthToken"
	defaultOpenAPI     = "3.0.3"
)

var allowedPathPrefixes = []string{
	"/api/note",
	"/api/notes",
	"/api/file",
	"/api/files",
	"/api/folder",
	"/api/folders",
}

// FixSpec applies FNS-specific OpenAPI corrections after Swagger 2 conversion.
// It never invents operations; it only filters and rewrites auth/server metadata.
func FixSpec(openAPI3 []byte) ([]byte, error) {
	var doc map[string]any
	if err := json.Unmarshal(openAPI3, &doc); err != nil {
		return nil, fmt.Errorf("decode OpenAPI 3 for FNS fixes: %w", err)
	}
	if err := fixDocument(doc); err != nil {
		return nil, err
	}
	out, err := json.Marshal(doc)
	if err != nil {
		return nil, fmt.Errorf("encode fixed FNS OpenAPI: %w", err)
	}
	return out, nil
}

func fixDocument(doc map[string]any) error {
	delete(doc, "servers")
	if _, ok := doc["openapi"]; !ok {
		doc["openapi"] = defaultOpenAPI
	}

	paths, _ := doc["paths"].(map[string]any)
	if paths == nil {
		return fmt.Errorf("FNS OpenAPI document has no paths")
	}
	filtered := map[string]any{}
	for path, item := range paths {
		if !pathAllowed(path) {
			continue
		}
		pathItem, _ := item.(map[string]any)
		if pathItem == nil {
			continue
		}
		stripTokenParameters(pathItem)
		filtered[path] = pathItem
	}
	if len(filtered) == 0 {
		return fmt.Errorf("FNS OpenAPI document has no Note/File/Folder operations after filtering")
	}
	doc["paths"] = filtered

	components, _ := doc["components"].(map[string]any)
	if components == nil {
		components = map[string]any{}
		doc["components"] = components
	}
	components["securitySchemes"] = map[string]any{
		securitySchemeName: map[string]any{
			"type":         "http",
			"scheme":       "bearer",
			"bearerFormat": "JWT",
			"description":  "FNS user access token",
		},
	}
	doc["security"] = []any{
		map[string]any{securitySchemeName: []any{}},
	}
	return nil
}

func pathAllowed(path string) bool {
	if strings.Contains(path, "/share") {
		return false
	}
	for _, prefix := range allowedPathPrefixes {
		if path == prefix || strings.HasPrefix(path, prefix+"/") {
			return true
		}
	}
	return false
}

func stripTokenParameters(pathItem map[string]any) {
	if params, ok := pathItem["parameters"].([]any); ok {
		pathItem["parameters"] = filterTokenParams(params)
	}
	for _, method := range []string{"get", "post", "put", "patch", "delete", "head", "options", "trace"} {
		operation, _ := pathItem[method].(map[string]any)
		if operation == nil {
			continue
		}
		if params, ok := operation["parameters"].([]any); ok {
			operation["parameters"] = filterTokenParams(params)
		}
		operation["security"] = []any{
			map[string]any{securitySchemeName: []any{}},
		}
	}
}

func filterTokenParams(params []any) []any {
	if len(params) == 0 {
		return params
	}
	out := make([]any, 0, len(params))
	for _, raw := range params {
		param, _ := raw.(map[string]any)
		if param == nil {
			out = append(out, raw)
			continue
		}
		name := strings.ToLower(strings.TrimSpace(fmt.Sprint(param["name"])))
		in := strings.ToLower(strings.TrimSpace(fmt.Sprint(param["in"])))
		if name == "token" && in == "header" {
			continue
		}
		out = append(out, param)
	}
	return out
}

// CommandTree lists tag/command pairs expected from Restish Method+Path naming.
func CommandTree(openAPI3 []byte) ([]string, error) {
	var doc map[string]any
	if err := json.Unmarshal(openAPI3, &doc); err != nil {
		return nil, err
	}
	paths, _ := doc["paths"].(map[string]any)
	var entries []string
	for path, item := range paths {
		pathItem, _ := item.(map[string]any)
		for _, method := range []string{"get", "post", "put", "patch", "delete", "head", "options"} {
			operation, _ := pathItem[method].(map[string]any)
			if operation == nil {
				continue
			}
			tag := firstTag(operation)
			command := fallbackOperationName(method, path)
			entries = append(entries, tag+" "+command)
		}
	}
	sort.Strings(entries)
	return entries, nil
}

func firstTag(operation map[string]any) string {
	tags, _ := operation["tags"].([]any)
	if len(tags) == 0 {
		return "api"
	}
	return slugify(fmt.Sprint(tags[0]))
}

func fallbackOperationName(method, path string) string {
	return slugify(strings.ToLower(method) + "-" + strings.Trim(path, "/"))
}

func slugify(s string) string {
	var b strings.Builder
	lastDash := false
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			if r >= 'A' && r <= 'Z' {
				b.WriteByte(byte(r - 'A' + 'a'))
			} else {
				b.WriteRune(r)
			}
			lastDash = false
			continue
		}
		if !lastDash && b.Len() > 0 {
			b.WriteByte('-')
			lastDash = true
		}
	}
	return strings.Trim(b.String(), "-")
}
