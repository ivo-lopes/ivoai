package evidence

import (
	"strings"
	"unicode/utf8"

	"github.com/ivo-lopes/ivoai/internal/platform"
)

// ValidatePersistableContent checks authoritative content before a typed JSON
// envelope can escape its strings. Encoding is not a sensitivity boundary.
func ValidatePersistableContent(raw []byte) error {
	if len(raw) > MaxPayloadBytes || !utf8.Valid(raw) || strings.ContainsRune(string(raw), '\x00') || platform.Redact(string(raw)) != string(raw) || sensitiveAssignment.Match(raw) || privateKey.Match(raw) {
		return ErrSensitive
	}
	return nil
}
