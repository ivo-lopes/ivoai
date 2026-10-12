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
