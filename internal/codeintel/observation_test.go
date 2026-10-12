package codeintel

import (
	"encoding/json"
	"errors"
	"github.com/ivo-lopes/ivoai/internal/core"
	"github.com/ivo-lopes/ivoai/internal/observability"
	"strings"
	"testing"
)

func TestObservationIsSanitizedAndDistinguishesUnknown(t *testing.T) {
	r := core.CodeResult{Text: "SECRET private repository body", ProviderVersion: Version, CacheHit: true, Fresh: true, Files: 2, IndexedFiles: 1, DurationMS: 12}
	event := Observation(r, nil)
	normalized, err := observability.Normalize(event)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(normalized)
	if strings.Contains(string(body), "SECRET") {
		t.Fatal("content leaked")
	}
	if !strings.Contains(string(body), `"cache_hit":true`) || !strings.Contains(string(body), `"index_files":1`) {
		t.Fatalf("missing observations %s", body)
	}
	if strings.Contains(string(body), "cpu_ms") || strings.Contains(string(body), "peak_memory_bytes") {
		t.Fatal("unknown measured as zero")
	}
	fail := Observation(r, errors.New("SECRET failure"))
	if fail.State != observability.StateDegraded {
		t.Fatal(fail)
	}
}
