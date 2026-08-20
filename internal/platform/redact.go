package platform

import (
	"regexp"
	"strings"
)

var sensitivePatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)((?:proxy-)?authorization\s*:\s*(?:bearer\s+)?)[^\s]+`),
	regexp.MustCompile(`(?im)^((?:set-)?cookie\s*:\s*)[^\r\n]+`),
	regexp.MustCompile(`(?i)((?:api[_-]?key|access[_-]?token|refresh[_-]?token|oauth[_-]?token|id[_-]?token|client[_-]?secret|enrollment[_-]?code)\s*[=:]\s*)[^\s,;]+`),
	regexp.MustCompile(`(?i)\b(?:sk-[A-Za-z0-9_-]{12,}|ivo_[A-Za-z0-9_-]{12,}|ivoai-(?:client|enroll)_[A-Za-z0-9_-]{12,})\b`),
}

func Redact(value string) string {
	result := value
	for _, pattern := range sensitivePatterns {
		result = pattern.ReplaceAllStringFunc(result, func(match string) string {
			if i := strings.IndexAny(match, "=:"); i >= 0 {
				return match[:i+1] + "[REDACTED]"
			}
			parts := strings.Fields(match)
			if len(parts) > 1 {
				return strings.Join(parts[:len(parts)-1], " ") + " [REDACTED]"
			}
			return "[REDACTED]"
		})
	}
	return result
}
