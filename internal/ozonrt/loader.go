package ozonrt

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"sync"

	"github.com/pb33f/libopenapi"
	restish "github.com/rest-sh/restish/v2"
	"gopkg.in/yaml.v3"
)

const (
	loaderPriority     = 100
	securitySchemeName = "OzonSellerAuth"
)

type SpecLoader struct{ Policy *SafetyPolicy }

func (SpecLoader) Priority() int { return loaderPriority }

func (SpecLoader) Detect(_ string, body []byte) bool {
	var doc map[string]any
	if err := decodeDocument(body, &doc); err != nil {
		return false
	}
	return strings.HasPrefix(strings.TrimSpace(fmt.Sprint(doc["openapi"])), "3.")
}

func (l SpecLoader) LoadWithOptions(body []byte, _ restish.LoadOptions) (*restish.APISpec, error) {
	fixed, routes, err := fixSpec(body)
	if err != nil {
		return nil, err
	}
	if l.Policy != nil {
		l.Policy.Replace(routes)
	}
	document, err := libopenapi.NewDocument(fixed)
	if err != nil {
		return nil, fmt.Errorf("parse fixed Ozon OpenAPI: %w", err)
	}
	return &restish.APISpec{ContentType: "application/json", Raw: body, Document: document}, nil
}

func fixSpec(body []byte) ([]byte, []safetyRoute, error) {
	var doc map[string]any
	if err := decodeDocument(body, &doc); err != nil {
		return nil, nil, err
	}
	if !strings.HasPrefix(strings.TrimSpace(fmt.Sprint(doc["openapi"])), "3.") {
		return nil, nil, fmt.Errorf("Ozon API description must be OpenAPI 3.x")
	}
	delete(doc, "servers")
	components := object(doc, "components")
	securitySchemes := object(components, "securitySchemes")
	securitySchemes[securitySchemeName] = map[string]any{"type": "apiKey", "in": "header", "name": "Api-Key"}
	doc["security"] = []any{map[string]any{securitySchemeName: []any{}}}

	paths, ok := doc["paths"].(map[string]any)
	if !ok || len(paths) == 0 {
		return nil, nil, fmt.Errorf("Ozon OpenAPI document has no paths")
	}
	var routes []safetyRoute
	for path, rawItem := range paths {
		item, _ := rawItem.(map[string]any)
		for _, method := range []string{"get", "post", "put", "patch", "delete", "head", "options"} {
			op, _ := item[method].(map[string]any)
			if op == nil {
				continue
			}
			if parameters := stripAuthParameters(op["parameters"]); parameters != nil {
				op["parameters"] = parameters
			} else {
				delete(op, "parameters")
			}
			op["security"] = []any{map[string]any{securitySchemeName: []any{}}}
			operationID := strings.TrimSpace(fmt.Sprint(op["operationId"]))
			if tags, ok := op["tags"].([]any); ok && len(tags) > 0 {
				tag := strings.TrimSpace(fmt.Sprint(tags[0]))
				if short := strings.TrimPrefix(operationID, tag+"_"); short != operationID && short != "" {
					op["x-cli-name"] = kebabIdentifier(short)
				}
			}
			routes = append(routes, safetyRoute{Method: strings.ToUpper(method), Path: path, Level: classifySafety(method, path, operationID)})
		}
	}
	sanitizeDocumentSchemas(doc)
	rewriteBrokenRefs(doc)
	out, err := json.Marshal(doc)
	if err != nil {
		return nil, nil, fmt.Errorf("encode fixed Ozon OpenAPI: %w", err)
	}
	return out, routes, nil
}

func sanitizeDocumentSchemas(doc map[string]any) {
	if components, ok := doc["components"].(map[string]any); ok {
		if schemas, ok := components["schemas"].(map[string]any); ok {
			for name, schema := range schemas {
				schemas[name] = sanitizeSchema(schema)
			}
		}
	}
	sanitizeSchemaFields(doc)
}

func sanitizeSchemaFields(node any) {
	switch value := node.(type) {
	case map[string]any:
		for key, child := range value {
			if key == "schema" {
				value[key] = sanitizeSchema(child)
				continue
			}
			sanitizeSchemaFields(child)
		}
	case []any:
		for _, child := range value {
			sanitizeSchemaFields(child)
		}
	}
}

var typeCoercions = map[string]string{
	"int": "integer", "int32": "integer", "int64": "integer", "long": "integer",
	"float": "number", "double": "number", "bool": "boolean", "timestamp": "string",
	"date-time": "string", "date": "string",
}

var validTypes = words("string integer number boolean object array null")
var validFormats = words("date-time time date duration email idn-email hostname idn-hostname ipv4 ipv6 uri uri-reference iri iri-reference uuid uri-template json-pointer relative-json-pointer regex int32 int64 float double byte binary password")
var nullableMetadata = words("description title format enum examples required items properties additionalProperties patternProperties allOf anyOf oneOf not")

func sanitizeSchema(node any) any {
	switch value := node.(type) {
	case map[string]any:
		out := make(map[string]any, len(value))
		for key, child := range value {
			if child == nil && nullableMetadata[key] {
				continue
			}
			if key == "required" {
				if _, misplaced := child.(bool); misplaced {
					continue
				}
			}
			if key == "type" {
				if fixed, ok := sanitizeType(child); ok {
					out[key] = fixed
				}
				continue
			}
			if key == "format" {
				if format, ok := child.(string); ok && !validFormats[format] {
					continue
				}
			}
			if key == "pattern" {
				if pattern, ok := child.(string); ok {
					if _, err := regexp.Compile(pattern); err != nil {
						continue
					}
				}
			}
			out[key] = sanitizeSchema(child)
		}
		return out
	case []any:
		out := make([]any, len(value))
		for i, child := range value {
			out[i] = sanitizeSchema(child)
		}
		return out
	default:
		return node
	}
}

func sanitizeType(value any) (any, bool) {
	switch typed := value.(type) {
	case string:
		if validTypes[typed] {
			return typed, true
		}
		coerced, ok := typeCoercions[typed]
		return coerced, ok
	case []any:
		out := make([]any, 0, len(typed))
		for _, item := range typed {
			if fixed, ok := sanitizeType(item); ok {
				out = append(out, fixed)
			}
		}
		return out, len(out) != 0
	default:
		return nil, false
	}
}

func decodeDocument(body []byte, out *map[string]any) error {
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) == 0 {
		return fmt.Errorf("empty Ozon API description")
	}
	var err error
	if trimmed[0] == '{' {
		err = json.Unmarshal(trimmed, out)
	} else {
		err = yaml.Unmarshal(trimmed, out)
	}
	if err != nil {
		return fmt.Errorf("decode Ozon API description: %w", err)
	}
	return nil
}

func object(parent map[string]any, key string) map[string]any {
	if value, ok := parent[key].(map[string]any); ok {
		return value
	}
	value := map[string]any{}
	parent[key] = value
	return value
}

func stripAuthParameters(raw any) any {
	parameters, ok := raw.([]any)
	if !ok {
		return raw
	}
	out := parameters[:0]
	for _, rawParameter := range parameters {
		parameter, _ := rawParameter.(map[string]any)
		ref := strings.ToLower(strings.TrimSpace(fmt.Sprint(parameter["$ref"])))
		name := strings.ToLower(strings.TrimSpace(fmt.Sprint(parameter["name"])))
		if strings.HasSuffix(ref, "/client-id") || strings.HasSuffix(ref, "/api-key") || name == "client-id" || name == "api-key" {
			continue
		}
		out = append(out, rawParameter)
	}
	return out
}

func rewriteBrokenRefs(node any) {
	switch value := node.(type) {
	case map[string]any:
		if ref, ok := value["$ref"].(string); ok && ref == "../../components/schemas/rpcStatus.yaml" {
			value["$ref"] = "#/components/schemas/rpcStatus"
		}
		for _, child := range value {
			rewriteBrokenRefs(child)
		}
	case []any:
		for _, child := range value {
			rewriteBrokenRefs(child)
		}
	}
}

type SafetyPolicy struct {
	mu     sync.RWMutex
	routes []safetyRoute
}

type safetyRoute struct {
	Method string
	Path   string
	Level  string
}

func (p *SafetyPolicy) Replace(routes []safetyRoute) {
	sort.Slice(routes, func(i, j int) bool { return routes[i].Path < routes[j].Path })
	p.mu.Lock()
	p.routes = routes
	p.mu.Unlock()
}

func (p *SafetyPolicy) Allow(method, path, rawMode string) error {
	level, found := p.level(strings.ToUpper(method), path)
	if !found {
		return fmt.Errorf("Ozon %s %s is blocked because it is absent from the loaded OpenAPI safety policy", strings.ToUpper(method), path)
	}
	mode := strings.ToLower(strings.TrimSpace(rawMode))
	if mode == "" {
		mode = "readonly"
	}
	allowed := level == "read" || mode == "destructive" || mode == "write" && level == "write"
	if mode != "readonly" && mode != "write" && mode != "destructive" {
		return fmt.Errorf("invalid OZON_WRITE_MODE %q: expected readonly, write, or destructive", rawMode)
	}
	if !allowed {
		return fmt.Errorf("Ozon %s %s is classified %s and blocked by OZON_WRITE_MODE=%s", strings.ToUpper(method), path, level, mode)
	}
	return nil
}

func (p *SafetyPolicy) level(method, path string) (string, bool) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	for _, route := range p.routes {
		if route.Method == method && matchPath(route.Path, path) {
			return route.Level, true
		}
	}
	return "", false
}

func matchPath(pattern, actual string) bool {
	want, got := strings.Split(strings.Trim(pattern, "/"), "/"), strings.Split(strings.Trim(actual, "/"), "/")
	if len(want) != len(got) {
		return false
	}
	for i := range want {
		if strings.HasPrefix(want[i], "{") && strings.HasSuffix(want[i], "}") {
			continue
		}
		if want[i] != got[i] {
			return false
		}
	}
	return true
}

var camelWord = regexp.MustCompile(`[A-Z]?[a-z]+|[A-Z]+(?:[A-Z]|$)|[0-9]+`)

func kebabIdentifier(value string) string {
	parts := camelWord.FindAllString(value, -1)
	for i := range parts {
		parts[i] = strings.ToLower(parts[i])
	}
	return strings.Join(parts, "-")
}

var readVerbs = words("list info get tree totals count details history search find view show describe summary export fetch lookup preview status available values sources calendar roles")
var writeVerbs = words("create update change set add edit save import upload send sync refresh activate deactivate enable disable start stop move pack ship apply calculate schedule approve answer print transfer confirm submit process execute register bind copy replace request generate mark")
var destructiveVerbs = words("delete remove cancel archive unarchive destroy purge reject decline withdraw")

var safetyOverrides = map[string]string{
	"POST /v2/chat/read":                                  "write",
	"POST /v1/cargoes/delete/status":                      "read",
	"POST /v1/product/certificate/rejection_reasons/list": "read",
	"POST /v1/posting/fbo/cancel-reason/list":             "read",
	"POST /v1/supply-order/cancel/status":                 "read",
	"POST /v1/posting/fbs/cancel-reason":                  "read",
	"POST /v2/posting/fbs/cancel-reason/list":             "read",
	"POST /v2/conditional-cancellation/list":              "read",
	"POST /v1/fbp/archive/get":                            "read",
	"POST /v1/fbp/archive/list":                           "read",
	"POST /v1/cancel-reason/list":                         "read",
	"POST /v1/cancel-reason/list-by-order":                "read",
	"POST /v1/cancel-reason/list-by-posting":              "read",
	"POST /v1/order/cancel/check":                         "read",
	"POST /v1/order/cancel/status":                        "read",
	"POST /v1/posting/cancel/status":                      "read",
}

func words(raw string) map[string]bool {
	out := map[string]bool{}
	for _, word := range strings.Fields(raw) {
		out[word] = true
	}
	return out
}

func classifySafety(method, path, operationID string) string {
	if level := safetyOverrides[strings.ToUpper(method)+" "+path]; level != "" {
		return level
	}
	tokens := map[string]bool{}
	for _, part := range strings.FieldsFunc(strings.ToLower(path), func(r rune) bool { return r == '/' || r == '-' || r == '_' }) {
		tokens[part] = true
	}
	for _, part := range camelWord.FindAllString(operationID, -1) {
		tokens[strings.ToLower(part)] = true
	}
	if intersects(tokens, destructiveVerbs) || strings.EqualFold(method, http.MethodDelete) {
		return "destructive"
	}
	if intersects(tokens, writeVerbs) || strings.EqualFold(method, http.MethodPut) || strings.EqualFold(method, http.MethodPatch) {
		return "write"
	}
	if intersects(tokens, readVerbs) || strings.EqualFold(method, http.MethodGet) || strings.EqualFold(method, http.MethodHead) {
		return "read"
	}
	return "write"
}

func intersects(left, right map[string]bool) bool {
	for key := range left {
		if right[key] {
			return true
		}
	}
	return false
}
