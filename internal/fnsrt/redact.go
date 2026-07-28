package fnsrt

import (
	"strings"

	"github.com/0xfe10/aicli/internal/authflow"
)

// RedactSecrets masks bearer tokens and token-like values in diagnostic text.
func RedactSecrets(input string) string {
	return authflow.RedactSecrets(input)
}

// ContainsSecret reports whether text appears to include a raw secret value.
func ContainsSecret(haystack, secret string) bool {
	secret = strings.TrimSpace(secret)
	return secret != "" && strings.Contains(haystack, secret)
}
