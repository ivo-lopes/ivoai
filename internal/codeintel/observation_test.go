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

func TestDirectObservationDoesNotInventStructuralIndexFreshness(t *testing.T) {
	event := Observation(core.CodeResult{ProviderVersion: "direct", Fresh: true, Files: 3}, nil)
	if event.Provider != "direct" || event.RoutingReason != observability.ReasonDirect || event.SourceFiles == nil || *event.SourceFiles != 3 {
		t.Fatal("direct source observation missing")
	}
	if event.IndexFresh != nil || event.IndexFiles != nil {
		t.Fatal("direct source inspection fabricated a structural index observation")
	}
	structural := Observation(core.CodeResult{ProviderVersion: Version, Fresh: true, IndexedFiles: 3}, nil)
	if structural.IndexFresh == nil || !*structural.IndexFresh || structural.IndexFiles == nil || *structural.IndexFiles != 3 {
		t.Fatal("observed structural index diagnostics lost")
	}
}
