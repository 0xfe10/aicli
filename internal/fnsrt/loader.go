package fnsrt

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/0xfe10/aicli/internal/swagger2rt"
	"github.com/pb33f/libopenapi"
	"github.com/pb33f/libopenapi/datamodel"
	restish "github.com/rest-sh/restish/v2"
	"gopkg.in/yaml.v3"
)

const loaderPriority = 100

// SpecLoader normalizes FNS Swagger 2 / OpenAPI 3 documents and always applies
// FNS contract fixes before Restish generates commands.
type SpecLoader struct{}

func (SpecLoader) Priority() int { return loaderPriority }

func (SpecLoader) Detect(contentType string, body []byte) bool {
	_ = contentType
	return swagger2rt.IsSwagger2(body) || IsOpenAPI3(body)
}

func (SpecLoader) LoadWithOptions(body []byte, opts restish.LoadOptions) (*restish.APISpec, error) {
	converted, err := ConvertAndFix(body)
	if err != nil {
		return nil, err
	}
	document, err := libopenapi.NewDocumentWithConfiguration(converted, documentConfig(opts))
	if err != nil {
		return nil, fmt.Errorf("parse fixed FNS OpenAPI: %w", err)
	}
	return &restish.APISpec{
		ContentType:      "application/json",
		Raw:              body,
		Document:         document,
		RequestedURL:     opts.RequestedURL,
		SourceURL:        opts.SourceURL,
		LocalPath:        opts.LocalPath,
		AllowCrossOrigin: opts.AllowCrossOrigin,
	}, nil
}

// ConvertAndFix normalizes Swagger 2 or OpenAPI 3 input and applies FixSpec.
func ConvertAndFix(body []byte) ([]byte, error) {
	normalized, err := NormalizeSpec(body)
	if err != nil {
		return nil, err
	}
	return FixSpec(normalized)
}

// NormalizeSpec converts Swagger 2 to OpenAPI 3 JSON, or re-encodes OpenAPI 3
// JSON/YAML to canonical JSON without applying FNS filters.
func NormalizeSpec(body []byte) ([]byte, error) {
	switch {
	case swagger2rt.IsSwagger2(body):
		return swagger2rt.Convert(body)
	case IsOpenAPI3(body):
		return normalizeOpenAPI3(body)
	default:
		return nil, fmt.Errorf("unsupported FNS API description: expected Swagger 2.0 or OpenAPI 3.x")
	}
}

// IsOpenAPI3 reports whether body is an OpenAPI 3.x document.
func IsOpenAPI3(body []byte) bool {
	raw, err := decodeSpecMap(body)
	if err != nil {
		return false
	}
	version := strings.TrimSpace(fmt.Sprint(raw["openapi"]))
	return strings.HasPrefix(version, "3.")
}

func normalizeOpenAPI3(body []byte) ([]byte, error) {
	raw, err := decodeSpecMap(body)
	if err != nil {
		return nil, err
	}
	version := strings.TrimSpace(fmt.Sprint(raw["openapi"]))
	if !strings.HasPrefix(version, "3.") {
		return nil, fmt.Errorf("document is not OpenAPI 3.x: openapi=%q", version)
	}
	out, err := json.Marshal(raw)
	if err != nil {
		return nil, fmt.Errorf("normalize OpenAPI 3 document to JSON: %w", err)
	}
	return out, nil
}

func decodeSpecMap(body []byte) (map[string]any, error) {
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

func documentConfig(opts restish.LoadOptions) *datamodel.DocumentConfiguration {
	cfg := datamodel.NewDocumentConfiguration()
	cfg.AllowFileReferences = true
	cfg.AllowRemoteReferences = opts.AllowCrossOrigin
	if opts.LocalPath != "" {
		cfg.BasePath = opts.LocalPath
		if info, err := os.Stat(opts.LocalPath); err == nil && !info.IsDir() {
			cfg.BasePath = filepath.Dir(opts.LocalPath)
		}
	}
	if opts.SourceURL != "" {
		if u, err := url.Parse(opts.SourceURL); err == nil {
			cfg.BaseURL = u
		}
	}
	return cfg
}
