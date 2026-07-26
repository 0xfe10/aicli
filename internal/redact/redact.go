package redact

import (
	"regexp"
	"strings"
)

var (
	bearerPattern      = regexp.MustCompile(`(?i)Bearer\s+\S+`)
	secretKVPattern    = regexp.MustCompile(`(?i)("?(access_token|refresh_token|client_secret|client_id|token|secret|code)"?\s*[:=]\s*"?)[^"&\s,}]+`)
	authHeaderPattern  = regexp.MustCompile(`(?i)(Authorization\s*:\s*)\S+(?:\s+\S+)?`)
	querySecretPattern = regexp.MustCompile(`(?i)(access_token|refresh_token|client_secret|client_id|token|secret|code|authorization)=[^&\s]+`)
)

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

// HeaderValue redacts Authorization-like header values.
func HeaderValue(name, value string) string {
	if strings.EqualFold(name, "Authorization") || strings.EqualFold(name, "Proxy-Authorization") {
		return "Bearer ***"
	}
	return String(value)
}
