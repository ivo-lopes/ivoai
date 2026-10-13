package toolrouting

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestAggregateCostIsBoundedAndDoesNotInferUnknown(t *testing.T) {
	var history History
	for i := range 100 {
		history.Observe("ask", "graft", Sample{LatencyMS: int64(i), ResultBytes: 400, Success: true})
	}
	c, ok := history.Cost("ask", "graft")
	if !ok || !c.LatencyMS.Known || c.CPUMS.Known || !c.ResultTokens.Estimated {
		t.Fatalf("bad observed estimates %+v", c)
	}
	history.Observe("private prompt", "graft", Sample{Success: true})
	b, _ := json.Marshal(history)
	if strings.Contains(string(b), "private prompt") {
		t.Fatal("unbounded label persisted")
	}
	if len(history.Entries) != 1 {
		t.Fatal("unknown labels accepted")
	}
	history.Observe("ask", "graft", Sample{LatencyMS: -1, Success: true})
	if history.Entries["ask:graft"].Samples != 100 {
		t.Fatal("invalid measure accepted")
	}
}

func TestCostRequiresRepeatedSuccessfulObservations(t *testing.T) {
	var h History
	for i := range 4 {
		h.Observe("ask", "graft", Sample{LatencyMS: 100, ResultBytes: 400, Success: true})
		if _, ok := h.Cost("ask", "graft"); ok {
			t.Fatalf("only %d samples displaced conservative defaults", i+1)
		}
	}
	h.Observe("ask", "graft", Sample{LatencyMS: 100, ResultBytes: 400, Success: true})
	if _, ok := h.Cost("ask", "graft"); !ok {
		t.Fatal("stable history unavailable")
	}
	h.Observe("ask", "graft", Sample{LatencyMS: 0, ResultBytes: 0, Success: false})
	if _, ok := h.Cost("ask", "graft"); ok {
		t.Fatal("unreliable history used for cost selection")
	}
}

func TestCostRejectsMalformedPersistedAggregates(t *testing.T) {
	for _, a := range []Aggregate{
		{Samples: 10, LatencyMS: -1},
		{Samples: 10, ResultBytes: 32769},
		{Samples: 10001},
		{Samples: 10, Failures: -1},
		{Samples: 10, LatencyMS: 90001},
	} {
		h := History{Entries: map[string]Aggregate{"ask:graft": a}}
		if _, ok := h.Cost("ask", "graft"); ok {
			t.Fatalf("invalid persisted aggregate accepted: %+v", a)
		}
	}
}
