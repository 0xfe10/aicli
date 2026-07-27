// Package swagger2rt converts Swagger / OpenAPI 2 documents into OpenAPI 3 for Restish.
package swagger2rt

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/getkin/kin-openapi/openapi2"
	"github.com/getkin/kin-openapi/openapi2conv"
	"github.com/pb33f/libopenapi"
	restish "github.com/rest-sh/restish/v2"
	"gopkg.in/yaml.v3"
)

const loaderPriority = 90

// Loader detects Swagger 2.0 documents, converts them to OpenAPI 3.0.3, and
// hands the result to Restish. It does not invent CLI commands.
type Loader struct{}

func (Loader) Priority() int { return loaderPriority }

func (Loader) Detect(contentType string, body []byte) bool {
	_ = contentType
	return IsSwagger2(body)
}

func (Loader) LoadWithOptions(body []byte, _ restish.LoadOptions) (*restish.APISpec, error) {
	converted, err := Convert(body)
	if err != nil {
		return nil, err
	}
	document, err := libopenapi.NewDocument(converted)
	if err != nil {
		return nil, fmt.Errorf("parse converted OpenAPI 3 document: %w", err)
	}
	return &restish.APISpec{
		ContentType: "application/json",
		Raw:         body,
		Document:    document,
	}, nil
}

// IsSwagger2 reports whether body is a Swagger / OpenAPI 2.0 document.
func IsSwagger2(body []byte) bool {
	raw, err := decodeDocument(body)
	if err != nil {
		return false
	}
	return swaggerVersion(raw) == "2.0"
}

// Convert turns a Swagger 2.0 document (JSON or YAML) into OpenAPI 3.0.3 JSON.
// Conversion failures are returned; operations are never silently dropped.
func Convert(body []byte) ([]byte, error) {
	raw, err := decodeDocument(body)
	if err != nil {
		return nil, err
	}
	version := swaggerVersion(raw)
	if version != "2.0" {
		if version == "" {
			return nil, fmt.Errorf("document is not Swagger 2.0: missing swagger field")
		}
		return nil, fmt.Errorf("document is not Swagger 2.0: swagger=%q", version)
	}

	jsonBody, err := json.Marshal(raw)
	if err != nil {
		return nil, fmt.Errorf("normalize Swagger 2 document to JSON: %w", err)
	}
	var doc openapi2.T
	if err := json.Unmarshal(jsonBody, &doc); err != nil {
		return nil, fmt.Errorf("parse Swagger 2 document: %w", err)
	}
	if doc.Swagger != "2.0" {
		return nil, fmt.Errorf("document is not Swagger 2.0: swagger=%q", doc.Swagger)
	}

	v3, err := openapi2conv.ToV3(&doc)
	if err != nil {
		return nil, fmt.Errorf("convert Swagger 2 to OpenAPI 3: %w", err)
	}
	if v3 == nil {
		return nil, fmt.Errorf("convert Swagger 2 to OpenAPI 3: empty result")
	}
	out, err := v3.MarshalJSON()
	if err != nil {
		return nil, fmt.Errorf("encode OpenAPI 3 document: %w", err)
	}
	return out, nil
}

func decodeDocument(body []byte) (map[string]any, error) {
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) == 0 {
		return nil, fmt.Errorf("empty API description")
	}
	var raw map[string]any
	if trimmed[0] == '{' || trimmed[0] == '[' {
		if err := json.Unmarshal(trimmed, &raw); err != nil {
			return nil, fmt.Errorf("decode JSON API description: %w", err)
		}
		return raw, nil
	}
	if err := yaml.Unmarshal(trimmed, &raw); err != nil {
		return nil, fmt.Errorf("decode YAML API description: %w", err)
	}
	if raw == nil {
		return nil, fmt.Errorf("decode YAML API description: empty mapping")
	}
	return raw, nil
}

func swaggerVersion(raw map[string]any) string {
	switch value := raw["swagger"].(type) {
	case string:
		return strings.TrimSpace(value)
	case float64:
		return strings.TrimSpace(fmt.Sprint(value))
	default:
		return ""
	}
}
