package enrollment

import (
	"regexp"
	"strings"
)

var bearerPattern = regexp.MustCompile(`(?i)bearer\s+[A-Za-z0-9._~+\-/]+=*`)
var credentialPattern = regexp.MustCompile(`ivoai-(?:enroll|client)_[A-Za-z0-9_-]+`)
var genericSecretPattern = regexp.MustCompile(`(?i)(authorization|api[_-]?key|token|cookie|enrollment[_-]?code)(\s*[:=]\s*)([^\s,;]+)`)

// Redact removes ivoai credentials and common authorization fields from log text.
func Redact(value string) string {
	value = bearerPattern.ReplaceAllString(value, "Bearer [REDACTED]")
	value = credentialPattern.ReplaceAllString(value, "[REDACTED]")
	value = genericSecretPattern.ReplaceAllString(value, `$1$2[REDACTED]`)
	return strings.ReplaceAll(value, "\r", "")
}
