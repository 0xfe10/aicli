package redact

import (
	"regexp"
	"strings"
	"unicode"
)

var (
	bearerPattern      = regexp.MustCompile(`(?i)Bearer\s+\S+`)
	secretKVPattern    = regexp.MustCompile(`(?i)("?(access[_-]?token|refresh[_-]?token|client[_-]?secret|client[_-]?id|authorization[_-]?code|auth[_-]?code|authorization|password|api[_-]?key|token|secret|code)"?\s*[:=]\s*"?)[^"&\s,}]+`)
	authHeaderPattern  = regexp.MustCompile(`(?i)(Authorization\s*:\s*)\S+(?:\s+\S+)?`)
	querySecretPattern = regexp.MustCompile(`(?i)(access[_-]?token|refresh[_-]?token|client[_-]?secret|client[_-]?id|authorization[_-]?code|auth[_-]?code|authorization|password|api[_-]?key|token|secret|code)=[^&\s]+`)
)

// Keys are stored in normalized form: lowercase with '_' and '-' removed.
// Bare "code" is intentionally excluded so PingCode business error codes survive.
var sensitiveNormalizedKeys = map[string]struct{}{
	"accesstoken":        {},
	"refreshtoken":       {},
	"clientsecret":       {},
	"clientid":           {},
	"authorization":      {},
	"authorizationcode":  {},
	"authcode":           {},
	"password":           {},
	"apikey":             {},
	"cookie":             {},
	"setcookie":          {},
	"proxyauthorization": {},
	"token":              {},
	"secret":             {},
	"idtoken":            {},
	"sessiontoken":       {},
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
			if IsSensitiveKey(k) {
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

// IsSensitiveKey reports whether a JSON object key should be fully masked.
// Matching ignores case and '_' / '-' so accessToken, access_token, and
// access-token are treated the same. Bare "code" is not sensitive.
func IsSensitiveKey(name string) bool {
	_, ok := sensitiveNormalizedKeys[NormalizeKey(name)]
	return ok
}

// NormalizeKey lowercases and strips '_' / '-' for credential key matching.
func NormalizeKey(name string) string {
	var b strings.Builder
	b.Grow(len(name))
	for _, r := range strings.TrimSpace(name) {
		if r == '_' || r == '-' {
			continue
		}
		b.WriteRune(unicode.ToLower(r))
	}
	return b.String()
}
