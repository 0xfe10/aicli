package redact

import (
	"regexp"
	"strings"
)

var (
	bearerPattern      = regexp.MustCompile(`(?i)Bearer\s+\S+`)
	secretKVPattern    = regexp.MustCompile(`(?i)("?(access_token|refresh_token|client_secret|client_id|token|secret|code|password|api[_-]?key)"?\s*[:=]\s*"?)[^"&\s,}]+`)
	authHeaderPattern  = regexp.MustCompile(`(?i)(Authorization\s*:\s*)\S+(?:\s+\S+)?`)
	querySecretPattern = regexp.MustCompile(`(?i)(access_token|refresh_token|client_secret|client_id|token|secret|code|authorization|password|api[_-]?key)=[^&\s]+`)
)

var sensitiveKeys = map[string]struct{}{
	"access_token":        {},
	"refresh_token":       {},
	"client_secret":       {},
	"client_id":           {},
	"authorization":       {},
	"password":            {},
	"api_key":             {},
	"apikey":              {},
	"api-key":             {},
	"cookie":              {},
	"set-cookie":          {},
	"set_cookie":          {},
	"proxy-authorization": {},
}

// String removes common credential material from free-form text.
func String(text string) string {
	if text == "" {
		return text
	}
	out := bearerPattern.ReplaceAllString(text, "Bearer ***")
	out = authHeaderPattern.ReplaceAllString(out, `${1}***`)
	out = secretKVPattern.ReplaceAllString(out, `${1}***`)
	out = querySecretPattern.ReplaceAllString(out, `${1}=***`)
	return out
}

// HeaderValue redacts Authorization-like and cookie header values.
func HeaderValue(name, value string) string {
	switch {
	case strings.EqualFold(name, "Authorization"), strings.EqualFold(name, "Proxy-Authorization"):
		return "Bearer ***"
	case strings.EqualFold(name, "Cookie"), strings.EqualFold(name, "Set-Cookie"):
		return "***"
	default:
		return String(value)
	}
}

// Value recursively redacts credentials in decoded JSON values.
func Value(v any) any {
	switch typed := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(typed))
		for k, child := range typed {
			if isSensitiveKey(k) {
				out[k] = "***"
				continue
			}
			out[k] = Value(child)
		}
		return out
	case []any:
		out := make([]any, len(typed))
		for i, child := range typed {
			out[i] = Value(child)
		}
		return out
	case string:
		return String(typed)
	default:
		return v
	}
}

func isSensitiveKey(name string) bool {
	_, ok := sensitiveKeys[strings.ToLower(strings.TrimSpace(name))]
	return ok
}
