package itsaplanrt

import (
	"github.com/0xfe10/aicli/internal/authflow"
	"os"
	"regexp"
	"strings"
)

var keyPattern = regexp.MustCompile(`(?i)(x-api-key|api[_-]?key)\s*[=:]\s*([^\s,;]+)`)

func RedactSecrets(input string) string {
	out := authflow.RedactSecrets(input)
	if key := os.Getenv("ITSAPLAN_API_KEY"); key != "" {
		out = strings.ReplaceAll(out, key, "***")
	}
	return keyPattern.ReplaceAllString(out, "${1}=***")
}
