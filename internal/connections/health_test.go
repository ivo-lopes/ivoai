package connections

import (
	"encoding/json"
	"testing"
)

func TestUnprobedHealthJSONDoesNotImplyUnhealthy(t *testing.T) {
	body, err := json.Marshal(ProfileHealth{})
	if err != nil {
		t.Fatal(err)
	}
	var value map[string]any
	if err := json.Unmarshal(body, &value); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"state", "context_state", "memory_state"} {
		if value[field] != "not_probed" {
			t.Fatalf("%s=%v", field, value[field])
		}
	}
	if value["probed"] != false {
		t.Fatal("unprobed result must be explicit")
	}
}
