package observability

import (
	"encoding/json"
	"testing"
)

// Run unchanged against v0.12.0 as well. New optional measurements use an
// existing operation/capability envelope, so older session validators can
// still read the event without a persistence migration.
func TestProjectionEnvelopeRemainsV012Readable(t *testing.T) {
	const body = `{"schema_version":1,"sequence":1,"observed_at":"2026-10-12T00:00:00Z","category":"capability","operation":"capability.resolve","component":"tools","capability":"tool.projection","state":"completed","tools_available":7,"tools_presented":1,"bytes_before":4585,"bytes_after":1230,"token_basis":"unavailable","routing_reason":"context_budget_applied"}`
	var event Event
	if err := json.Unmarshal([]byte(body), &event); err != nil {
		t.Fatal(err)
	}
	if err := event.Validate(); err != nil {
		t.Fatal(err)
	}
}
