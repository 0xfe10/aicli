package fnsrt

import (
	"regexp"
	"strings"
)

var (
	bearerPattern  = regexp.MustCompile(`(?i)\bBearer\s+[A-Za-z0-9\-._~+/]+=*`)
	tokenKVPattern = regexp.MustCompile(`(?i)\b(access[_-]?token|token)\s*[=:]\s*([^\s,;]+)`)
)

// RedactSecrets masks bearer tokens and token-like values in diagnostic text.
func RedactSecrets(input string) string {
	if input == "" {
		return input
	}
	out := bearerPattern.ReplaceAllString(input, "Bearer ***")
	out = tokenKVPattern.ReplaceAllString(out, "${1}=***")
	return out
}

// ContainsSecret reports whether text appears to include a raw secret value.
func ContainsSecret(haystack, secret string) bool {
	secret = strings.TrimSpace(secret)
	return secret != "" && strings.Contains(haystack, secret)
}
