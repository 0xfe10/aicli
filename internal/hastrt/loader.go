package hastrt

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"github.com/0xfe10/aicli/internal/swagger2rt"
	"github.com/pb33f/libopenapi"
	restish "github.com/rest-sh/restish/v2"
)

type SpecLoader struct{}

func (SpecLoader) Priority() int                     { return 100 }
func (SpecLoader) Detect(_ string, body []byte) bool { return swagger2rt.IsSwagger2(body) }

func (SpecLoader) LoadWithOptions(body []byte, opts restish.LoadOptions) (*restish.APISpec, error) {
	fixed, err := fixSpec(body)
	if err != nil {
		return nil, err
	}
	doc, err := libopenapi.NewDocument(fixed)
	if err != nil {
		return nil, err
	}
	return &restish.APISpec{ContentType: "application/json", Raw: body, Document: doc,
		RequestedURL: opts.RequestedURL, SourceURL: opts.SourceURL, LocalPath: opts.LocalPath,
		AllowCrossOrigin: false}, nil
}

// Normalize the published Swagger contract without inventing operation names.
// Both upstream security names use the same personal API key in Bearer form.
func fixSpec(body []byte) ([]byte, error) {
	var source map[string]any
	if err := json.Unmarshal(body, &source); err != nil {
		return nil, err
	}
	if source["swagger"] != "2.0" {
		return nil, fmt.Errorf("expected Hast Swagger 2.0 JSON")
	}
	if err := rejectRefs(source); err != nil {
		return nil, err
	}
	paths, ok := source["paths"].(map[string]any)
	if !ok || len(paths) == 0 {
		return nil, fmt.Errorf("Swagger paths missing")
	}
	for path, rawItem := range paths {
		item, ok := rawItem.(map[string]any)
		if !ok {
			continue
		}
		for method, rawOp := range item {
			op, ok := rawOp.(map[string]any)
			if !ok {
				continue
			}
			// Shared upstream handler annotations give GET operations optional
			// mutation bodies. Reads should use their path/query parameters only.
			if method == "get" {
				if parameters, ok := op["parameters"].([]any); ok {
					filtered := make([]any, 0, len(parameters))
					for _, parameter := range parameters {
						p, _ := parameter.(map[string]any)
						if p["in"] != "body" {
							filtered = append(filtered, parameter)
						}
					}
					op["parameters"] = filtered
				}
			}
			if method == "post" && path == "/api/public/v1/files" {
				op["consumes"] = []any{"multipart/form-data"}
			}
		}
	}
	normalized, err := json.Marshal(source)
	if err != nil {
		return nil, err
	}
	converted, err := swagger2rt.Convert(normalized)
	if err != nil {
		return nil, err
	}
	var doc map[string]any
	if err := json.Unmarshal(converted, &doc); err != nil {
		return nil, err
	}
	// The converter preserves vendor extensions verbatim. Hast's event variant
	// map contains Swagger refs inside an extension; relocate those as well.
	relocateExtensionRefs(doc)
	delete(doc, "servers")
	components, ok := doc["components"].(map[string]any)
	if !ok {
		components = map[string]any{}
		doc["components"] = components
	}
	components["securitySchemes"] = map[string]any{securitySchemeName: map[string]any{"type": "http", "scheme": "bearer"}}
	doc["security"] = []any{map[string]any{securitySchemeName: []any{}}}
	for _, rawItem := range doc["paths"].(map[string]any) {
		item, _ := rawItem.(map[string]any)
		delete(item, "servers")
		for method, rawOp := range item {
			switch method {
			case "get", "post", "put", "patch", "delete", "head", "options":
			default:
				continue
			}
			if op, ok := rawOp.(map[string]any); ok {
				delete(op, "servers")
				op["security"] = []any{map[string]any{securitySchemeName: []any{}}}
			}
		}
	}
	return json.Marshal(doc)
}

func relocateExtensionRefs(node any) {
	switch value := node.(type) {
	case map[string]any:
		for key, child := range value {
			if key == "$ref" {
				if ref, ok := child.(string); ok && strings.HasPrefix(ref, "#/definitions/") {
					value[key] = "#/components/schemas/" + strings.TrimPrefix(ref, "#/definitions/")
				}
			}
			relocateExtensionRefs(child)
		}
	case []any:
		for _, child := range value {
			relocateExtensionRefs(child)
		}
	}
}

func rejectRefs(node any) error {
	switch value := node.(type) {
	case map[string]any:
		for key, child := range value {
			if key == "$ref" {
				if ref, ok := child.(string); !ok || !strings.HasPrefix(ref, "#/") {
					return fmt.Errorf("external Swagger references are unsupported")
				}
			}
			if err := rejectRefs(child); err != nil {
				return err
			}
		}
	case []any:
		for _, child := range value {
			if err := rejectRefs(child); err != nil {
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
		return fmt.Errorf("hast Base URL must be an origin without a path")
	}
	return nil
}
