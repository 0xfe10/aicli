package pingcodert

import (
	"bytes"
	"encoding/json"
	"fmt"
	"html"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"unicode"

	"github.com/pb33f/libopenapi"
	restish "github.com/rest-sh/restish/v2"
)

const loaderPriority = 100

var (
	htmlTagPattern = regexp.MustCompile(`<[^>]+>`)
	spacePattern   = regexp.MustCompile(`\s+`)
)

// APIDocLoader converts PingCode's official api_data.json format into the
// canonical OpenAPI model consumed by Restish.
type APIDocLoader struct{}

func (APIDocLoader) Priority() int { return loaderPriority }

func (APIDocLoader) Detect(contentType string, body []byte) bool {
	if len(bytes.TrimSpace(body)) == 0 || bytes.TrimSpace(body)[0] != '[' {
		return false
	}
	var records []apiDocRecord
	if err := json.Unmarshal(body, &records); err != nil {
		return false
	}
	for _, record := range records {
		if isHTTPMethod(record.Type) && strings.TrimSpace(record.URL) != "" && strings.TrimSpace(record.Name) != "" {
			return true
		}
	}
	return false
}

func (APIDocLoader) LoadWithOptions(body []byte, _ restish.LoadOptions) (*restish.APISpec, error) {
	openAPI, err := ConvertAPIDoc(body)
	if err != nil {
		return nil, err
	}
	document, err := libopenapi.NewDocument(openAPI)
	if err != nil {
		return nil, fmt.Errorf("parse converted PingCode OpenAPI: %w", err)
	}
	return &restish.APISpec{
		ContentType: "application/json",
		Raw:         body,
		Document:    document,
	}, nil
}

// ConvertAPIDoc returns a deterministic OpenAPI 3.1 document for PingCode's
// vendor API description. Duplicate method/path records are merged because
// OpenAPI intentionally permits only one operation per method and path.
func ConvertAPIDoc(body []byte) ([]byte, error) {
	var records []apiDocRecord
	if err := json.Unmarshal(body, &records); err != nil {
		return nil, fmt.Errorf("decode PingCode api_data.json: %w", err)
	}
	if len(records) == 0 {
		return nil, fmt.Errorf("PingCode api_data.json contains no records")
	}

	paths := map[string]any{}
	operationCount := 0
	for index, record := range records {
		method := strings.ToUpper(strings.TrimSpace(record.Type))
		if !isHTTPMethod(method) || excludedAuthOperation(record.URL) {
			continue
		}
		path, queryTemplate, err := parseOperationURL(record.URL)
		if err != nil {
			return nil, fmt.Errorf("record %d (%s %s): %w", index, method, record.URL, err)
		}
		operation, err := convertOperation(method, path, queryTemplate, record)
		if err != nil {
			return nil, fmt.Errorf("record %d (%s %s): %w", index, method, record.URL, err)
		}
		pathItem, _ := paths[path].(map[string]any)
		if pathItem == nil {
			pathItem = map[string]any{}
			paths[path] = pathItem
		}
		key := strings.ToLower(method)
		if existing, ok := pathItem[key].(map[string]any); ok {
			mergeOperation(existing, operation)
			continue
		}
		pathItem[key] = operation
		operationCount++
	}
	if operationCount == 0 {
		return nil, fmt.Errorf("PingCode api_data.json contains no supported API operations")
	}

	document := map[string]any{
		"openapi": "3.1.0",
		"info": map[string]any{
			"title":       "PingCode API",
			"description": "Generated from the official PingCode api_data.json description.",
			"version":     "1.0.0",
		},
		"paths": paths,
		"components": map[string]any{"securitySchemes": map[string]any{
			"enterpriseToken": map[string]any{"type": "http", "scheme": "bearer", "description": "PingCode enterprise token"},
			"userToken":       map[string]any{"type": "http", "scheme": "bearer", "description": "PingCode user token"},
		}},
	}
	converted, err := json.Marshal(document)
	if err != nil {
		return nil, fmt.Errorf("encode converted PingCode OpenAPI: %w", err)
	}
	return converted, nil
}

type apiDocRecord struct {
	Version     string                  `json:"version"`
	Type        string                  `json:"type"`
	URL         string                  `json:"url"`
	Group       string                  `json:"group"`
	Name        string                  `json:"name"`
	Description string                  `json:"description"`
	Permission  []namedValue            `json:"permission"`
	Scopes      []namedValue            `json:"scopes"`
	Parameter   apiDocFieldsAndExamples `json:"parameter"`
	Success     apiDocFieldsAndExamples `json:"success"`
}

type namedValue struct {
	Name string `json:"name"`
}

type apiDocFieldsAndExamples struct {
	Fields   map[string][]apiDocField `json:"fields"`
	Examples []apiDocExample          `json:"examples"`
}

type apiDocField struct {
	Type          string   `json:"type"`
	Optional      bool     `json:"optional"`
	Field         string   `json:"field"`
	Description   string   `json:"description"`
	AllowedValues []string `json:"allowedValues"`
}

type apiDocExample struct {
	Content string `json:"content"`
	Type    string `json:"type"`
}

func convertOperation(method, path string, queryTemplate url.Values, record apiDocRecord) (map[string]any, error) {
	parameters := make([]any, 0)
	parameterSeen := map[string]bool{}
	addParameter := func(field apiDocField, location string, required bool) error {
		name := strings.TrimSpace(field.Field)
		if name == "" {
			return nil
		}
		key := location + ":" + name
		if parameterSeen[key] {
			return nil
		}
		schema, err := schemaForField(field)
		if err != nil {
			return err
		}
		parameterSeen[key] = true
		parameters = append(parameters, map[string]any{
			"name":        name,
			"in":          location,
			"required":    required,
			"description": plainText(field.Description),
			"schema":      schema,
		})
		return nil
	}

	for _, field := range record.Parameter.Fields["路径参数"] {
		if err := addParameter(field, "path", true); err != nil {
			return nil, err
		}
	}
	for _, field := range record.Parameter.Fields["查询参数"] {
		if err := addParameter(field, "query", !field.Optional); err != nil {
			return nil, err
		}
	}
	for name, values := range queryTemplate {
		field := apiDocField{Type: "String", Field: name}
		if len(values) > 0 && !isTemplateValue(values[0], name) {
			field.AllowedValues = []string{values[0]}
		}
		if err := addParameter(field, "query", true); err != nil {
			return nil, err
		}
	}
	for _, name := range pathTemplateNames(path) {
		if !parameterSeen["path:"+name] {
			if err := addParameter(apiDocField{Type: "String", Field: name}, "path", true); err != nil {
				return nil, err
			}
		}
	}

	bodyFields, multipartFields := requestBodyFields(record.Parameter.Fields)
	requestContent := map[string]any{}
	if len(bodyFields) > 0 {
		schema, err := objectSchema(bodyFields)
		if err != nil {
			return nil, err
		}
		media := map[string]any{"schema": schema}
		if example, ok := firstJSONExample(record.Parameter.Examples); ok {
			media["example"] = example
		}
		requestContent["application/json"] = media
	}
	if len(multipartFields) > 0 {
		schema, err := objectSchema(multipartFields)
		if err != nil {
			return nil, err
		}
		requestContent["multipart/form-data"] = map[string]any{"schema": schema}
	}

	response := map[string]any{"description": "Successful response"}
	responseMedia := map[string]any{}
	if schema, err := responseSchema(record.Success.Fields); err != nil {
		return nil, err
	} else if schema != nil {
		responseMedia["schema"] = schema
	}
	if example, ok := firstJSONExample(record.Success.Examples); ok {
		responseMedia["example"] = example
	}
	if len(responseMedia) > 0 {
		response["content"] = map[string]any{"application/json": responseMedia}
	}

	tag, operationID := operationIdentity(method, path, queryTemplate)
	security := operationSecurity(record.Permission)
	if isWriteMethod(method) && len(security) == 0 {
		return nil, fmt.Errorf("write operation has no recognized PingCode token permission")
	}
	operation := map[string]any{
		"operationId": operationID,
		"summary":     plainText(record.Name),
		"description": plainText(record.Description),
		"tags":        []string{tag},
		"responses":   map[string]any{"200": response},
		"security":    security,
	}
	if scopes := names(record.Scopes); len(scopes) > 0 {
		operation["x-pingcode-scopes"] = scopes
	}
	if len(parameters) > 0 {
		operation["parameters"] = parameters
	}
	if len(requestContent) > 0 {
		operation["requestBody"] = map[string]any{"required": hasRequiredBodyField(bodyFields, multipartFields), "content": requestContent}
	}
	return operation, nil
}

func requestBodyFields(groups map[string][]apiDocField) (jsonFields, multipartFields []apiDocField) {
	keys := make([]string, 0, len(groups))
	for key := range groups {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		switch key {
		case "路径参数", "查询参数":
			continue
		case "请求参数 form-data":
			multipartFields = append(multipartFields, groups[key]...)
		default:
			jsonFields = append(jsonFields, groups[key]...)
		}
	}
	return jsonFields, multipartFields
}

func objectSchema(fields []apiDocField) (map[string]any, error) {
	root := map[string]any{"type": "object", "properties": map[string]any{}}
	for _, field := range fields {
		if strings.TrimSpace(field.Field) == "" {
			continue
		}
		fieldSchema, err := schemaForField(field)
		if err != nil {
			return nil, err
		}
		insertSchema(root, strings.Split(field.Field, "."), fieldSchema, !field.Optional)
	}
	return root, nil
}

func insertSchema(root map[string]any, path []string, value map[string]any, required bool) {
	current := root
	for index, name := range path {
		properties := ensureProperties(current)
		if index == len(path)-1 {
			if existing, ok := properties[name].(map[string]any); ok {
				mergeSchema(existing, value)
			} else {
				properties[name] = value
			}
			if required {
				addRequired(current, name)
			}
			return
		}
		next, _ := properties[name].(map[string]any)
		if next == nil {
			next = map[string]any{"type": "object", "properties": map[string]any{}}
			properties[name] = next
		}
		if next["type"] == "array" {
			items, _ := next["items"].(map[string]any)
			if items == nil {
				items = map[string]any{"type": "object", "properties": map[string]any{}}
				next["items"] = items
			}
			current = items
		} else {
			next["type"] = "object"
			ensureProperties(next)
			current = next
		}
	}
}

func schemaForField(field apiDocField) (map[string]any, error) {
	typ := strings.ToLower(strings.TrimSpace(field.Type))
	var schema map[string]any
	switch typ {
	case "string", "sting":
		schema = map[string]any{"type": "string"}
	case "number":
		schema = map[string]any{"type": "number"}
	case "boolean":
		schema = map[string]any{"type": "boolean"}
	case "object":
		schema = map[string]any{"type": "object", "additionalProperties": true}
	case "object/string":
		schema = map[string]any{"oneOf": []any{
			map[string]any{"type": "object", "additionalProperties": true},
			map[string]any{"type": "string"},
		}}
	case "string[]":
		schema = map[string]any{"type": "array", "items": map[string]any{"type": "string"}}
	case "object[]":
		schema = map[string]any{"type": "array", "items": map[string]any{"type": "object", "additionalProperties": true}}
	case "file":
		schema = map[string]any{"type": "string", "format": "binary"}
	default:
		return nil, fmt.Errorf("field %q has unsupported type %q", field.Field, field.Type)
	}
	if description := plainText(field.Description); description != "" {
		schema["description"] = description
	}
	if len(field.AllowedValues) > 0 {
		schema["enum"] = field.AllowedValues
	}
	return schema, nil
}

func responseSchema(groups map[string][]apiDocField) (map[string]any, error) {
	var fields []apiDocField
	keys := make([]string, 0, len(groups))
	for key := range groups {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		fields = append(fields, groups[key]...)
	}
	if len(fields) == 0 {
		return nil, nil
	}
	return objectSchema(fields)
}

func operationSecurity(permissions []namedValue) []any {
	var security []any
	seen := map[string]bool{}
	add := func(name string) {
		if !seen[name] {
			security = append(security, map[string]any{name: []string{}})
			seen[name] = true
		}
	}
	for _, permission := range permissions {
		switch strings.TrimSpace(permission.Name) {
		case "企业令牌":
			add("enterpriseToken")
		case "用户令牌":
			add("userToken")
		case "企业令牌/用户令牌", "用户令牌/企业令牌":
			add("enterpriseToken")
			add("userToken")
		}
	}
	return security
}

func names(values []namedValue) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value.Name != "" {
			result = append(result, value.Name)
		}
	}
	return result
}

func operationIdentity(method, path string, query url.Values) (string, string) {
	segments := strings.Split(strings.Trim(path, "/"), "/")
	if len(segments) > 0 && segments[0] == "v1" {
		segments = segments[1:]
	}
	tag := "api"
	if len(segments) > 0 {
		tag = slug(segments[0])
		segments = segments[1:]
	}
	if len(segments) == 0 {
		segments = []string{tag}
	}
	parts := []string{strings.ToLower(method)}
	for _, segment := range segments {
		if strings.HasPrefix(segment, "{") && strings.HasSuffix(segment, "}") {
			parts = append(parts, "by", slug(strings.Trim(segment, "{}")))
		} else {
			parts = append(parts, slug(segment))
		}
	}
	queryNames := make([]string, 0, len(query))
	for name := range query {
		queryNames = append(queryNames, slug(name))
	}
	sort.Strings(queryNames)
	if len(queryNames) > 0 {
		parts = append(parts, "by")
		parts = append(parts, queryNames...)
	}
	return tag, strings.Join(parts, "-")
}

func parseOperationURL(raw string) (string, url.Values, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return "", nil, err
	}
	if u.IsAbs() {
		return "", nil, fmt.Errorf("absolute operation URLs are not supported")
	}
	if !strings.HasPrefix(u.Path, "/") {
		return "", nil, fmt.Errorf("operation path must start with /")
	}
	return u.Path, u.Query(), nil
}

func pathTemplateNames(path string) []string {
	var names []string
	for _, segment := range strings.Split(path, "/") {
		if strings.HasPrefix(segment, "{") && strings.HasSuffix(segment, "}") {
			names = append(names, strings.Trim(segment, "{}"))
		}
	}
	return names
}

func excludedAuthOperation(raw string) bool {
	return strings.Contains(raw, "/v1/auth/token") || strings.Contains(raw, "/authorize?")
}

func isHTTPMethod(method string) bool {
	switch strings.ToUpper(strings.TrimSpace(method)) {
	case "GET", "POST", "PUT", "PATCH", "DELETE":
		return true
	default:
		return false
	}
}

func isWriteMethod(method string) bool {
	switch strings.ToUpper(strings.TrimSpace(method)) {
	case "POST", "PUT", "PATCH", "DELETE":
		return true
	default:
		return false
	}
}

func isTemplateValue(value, name string) bool { return value == "{"+name+"}" }

func firstJSONExample(examples []apiDocExample) (any, bool) {
	for _, example := range examples {
		if strings.TrimSpace(example.Content) == "" {
			continue
		}
		var value any
		if json.Unmarshal([]byte(example.Content), &value) == nil {
			return value, true
		}
	}
	return nil, false
}

func hasRequiredBodyField(groups ...[]apiDocField) bool {
	for _, fields := range groups {
		for _, field := range fields {
			if !field.Optional {
				return true
			}
		}
	}
	return false
}

func ensureProperties(schema map[string]any) map[string]any {
	properties, _ := schema["properties"].(map[string]any)
	if properties == nil {
		properties = map[string]any{}
		schema["properties"] = properties
	}
	return properties
}

func addRequired(schema map[string]any, name string) {
	required, _ := schema["required"].([]string)
	for _, existing := range required {
		if existing == name {
			return
		}
	}
	schema["required"] = append(required, name)
}

func mergeSchema(dst, src map[string]any) {
	for key, value := range src {
		if key == "properties" {
			dstProps := ensureProperties(dst)
			for name, child := range value.(map[string]any) {
				dstProps[name] = child
			}
			continue
		}
		dst[key] = value
	}
}

func mergeOperation(dst, src map[string]any) {
	dst["description"] = strings.TrimSpace(fmt.Sprint(dst["description"]) + "\n\n" + fmt.Sprint(src["description"]))
	dst["parameters"] = mergeParameterLists(dst["parameters"], src["parameters"])
	mergeRequestBodies(dst, src)
}

func mergeParameterLists(left, right any) []any {
	var merged []any
	seen := map[string]bool{}
	for _, source := range []any{left, right} {
		items, _ := source.([]any)
		for _, item := range items {
			parameter, _ := item.(map[string]any)
			key := fmt.Sprint(parameter["in"]) + ":" + fmt.Sprint(parameter["name"])
			if !seen[key] {
				merged = append(merged, parameter)
				seen[key] = true
			}
		}
	}
	return merged
}

func mergeRequestBodies(dst, src map[string]any) {
	srcBody, _ := src["requestBody"].(map[string]any)
	if srcBody == nil {
		return
	}
	dstBody, _ := dst["requestBody"].(map[string]any)
	if dstBody == nil {
		dst["requestBody"] = srcBody
		return
	}
	dstContent, _ := dstBody["content"].(map[string]any)
	srcContent, _ := srcBody["content"].(map[string]any)
	for mediaType, media := range srcContent {
		dstContent[mediaType] = media
	}
	if required, _ := srcBody["required"].(bool); required {
		dstBody["required"] = true
	}
}

func plainText(value string) string {
	value = html.UnescapeString(htmlTagPattern.ReplaceAllString(value, " "))
	return strings.TrimSpace(spacePattern.ReplaceAllString(value, " "))
}

func slug(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var out strings.Builder
	lastDash := false
	for _, char := range value {
		if unicode.IsLetter(char) || unicode.IsDigit(char) {
			out.WriteRune(char)
			lastDash = false
			continue
		}
		if !lastDash && out.Len() > 0 {
			out.WriteByte('-')
			lastDash = true
		}
	}
	return strings.Trim(out.String(), "-")
}
