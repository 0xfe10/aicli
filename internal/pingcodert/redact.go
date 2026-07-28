package pingcodert

import "github.com/0xfe10/aicli/internal/authflow"

// RedactSecrets masks authorization values in diagnostic text.
func RedactSecrets(input string) string { return authflow.RedactSecrets(input) }
