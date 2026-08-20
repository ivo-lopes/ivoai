package platform

import (
	"encoding/json"
	"io"
	"os"
	"strings"
	"time"
)

// DebugLog emits one redacted JSON record only when explicitly enabled.
// Callers pass semantic fields, never raw command arguments or HTTP headers.
func DebugLog(out io.Writer, event string, fields map[string]string) {
	if out == nil || !strings.EqualFold(os.Getenv("IVOAI_LOG_LEVEL"), "debug") {
		return
	}
	record := map[string]any{
		"time":  time.Now().UTC().Format(time.RFC3339Nano),
		"level": "debug",
		"event": Redact(event),
	}
	for key, value := range fields {
		record[key] = Redact(value)
	}
	_ = json.NewEncoder(out).Encode(record)
}
