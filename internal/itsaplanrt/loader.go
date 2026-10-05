package itsaplanrt

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"github.com/pb33f/libopenapi"
	restish "github.com/rest-sh/restish/v2"
)

// SpecLoader excludes endpoints whose credentials are not personal API keys.
// Every retained operation uses our handler, including public operations, so
// raw requests and writes cannot bypass the safety gate through security: [].
type SpecLoader struct{}

func (SpecLoader) Priority() int { return 100 }
func (SpecLoader) Detect(_ string, b []byte) bool {
	var d struct {
		OpenAPI string `json:"openapi"`
	}
	return json.Unmarshal(b, &d) == nil && strings.HasPrefix(d.OpenAPI, "3.")
}
func (SpecLoader) LoadWithOptions(body []byte, opts restish.LoadOptions) (*restish.APISpec, error) {
	fixed, err := fixSpec(body)
	if err != nil {
		return nil, err
	}
	doc, err := libopenapi.NewDocument(fixed)
	if err != nil {
		return nil, err
	}
	return &restish.APISpec{ContentType: "application/json", Raw: body, Document: doc, RequestedURL: opts.RequestedURL, SourceURL: opts.SourceURL, LocalPath: opts.LocalPath, AllowCrossOrigin: false}, nil
}
func fixSpec(body []byte) ([]byte, error) {
	var doc map[string]any
	if err := json.Unmarshal(body, &doc); err != nil {
		return nil, err
	}
	if v, _ := doc["openapi"].(string); !strings.HasPrefix(v, "3.") {
		return nil, fmt.Errorf("expected OpenAPI 3 JSON")
	}
	if err := rejectRefs(doc); err != nil {
		return nil, err
	}
	paths, ok := doc["paths"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("OpenAPI paths missing")
	}
	for path, raw := range paths {
		item, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		for method, rawOp := range item {
			switch method {
			case "get", "post", "put", "patch", "delete", "head", "options":
			default:
				continue
			}
			op, ok := rawOp.(map[string]any)
			if !ok {
				continue
			}
			security, exists := op["security"]
			if !exists {
				security = doc["security"]
			}
			supported := security == nil
			if entries, ok := security.([]any); ok {
				supported = len(entries) == 0
				for _, entry := range entries {
					if req, ok := entry.(map[string]any); ok && len(req) == 1 {
						if _, ok := req[securitySchemeName]; ok {
							supported = true
						}
					}
				}
			}
			if !supported {
				delete(item, method)
				continue
			}
			op["security"] = []any{map[string]any{securitySchemeName: []any{}}}
		}
		if len(item) == 0 {
			delete(paths, path)
		}
	}
	// Use the configured API origin, never a server address supplied by the spec.
	delete(doc, "servers")
	for _, raw := range paths {
		if item, ok := raw.(map[string]any); ok {
			delete(item, "servers")
			for _, rawOp := range item {
				if op, ok := rawOp.(map[string]any); ok {
					delete(op, "servers")
				}
			}
		}
	}
	return json.Marshal(doc)
}
func rejectRefs(node any) error {
	switch v := node.(type) {
	case map[string]any:
		for k, item := range v {
			if k == "$ref" {
				ref, _ := item.(string)
				if _, isReference := item.(string); isReference && !strings.HasPrefix(ref, "#/") {
					return fmt.Errorf("external OpenAPI references are unsupported")
				}
			}
			if err := rejectRefs(item); err != nil {
				return err
			}
		}
	case []any:
		for _, item := range v {
			if err := rejectRefs(item); err != nil {
				return err
			}
		}
	}
	return nil
}
func validateOrigin(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("invalid API origin")
	}
	if u.Path != "" && u.Path != "/" {
		return fmt.Errorf("itsaplan Base URL must be an origin without a path")
	}
	return nil
}
