package toolrouting

import (
	"math"
	"testing"
)

func TestSelectionRejectsIneligibleBeforeComparingCost(t *testing.T) {
	good := Candidate{ID: "structural", Authorized: true, Compatible: true, Available: true, Capabilities: []string{"code.symbol"}, Coverage: 1, Reliability: 1, Cost: Cost{InputTokens: Estimate{Value: 100, Known: true}}}
	cheap := good
	cheap.ID = "forbidden"
	cheap.Authorized = false
	cheap.Cost.InputTokens.Value = 0
	got, err := Select(Request{Capability: "code.symbol", MinCoverage: .9, MinReliability: .9}, []Candidate{cheap, good})
	if err != nil || got.Tool != "structural" {
		t.Fatalf("selection = %+v, %v", got, err)
	}
}

func TestSelectionDoesNotTreatUnknownAsFree(t *testing.T) {
	known := Candidate{ID: "direct", Authorized: true, Compatible: true, Available: true, Capabilities: []string{"code.literal"}, Coverage: 1, Reliability: 1, Cost: Cost{InputTokens: Estimate{Value: 10, Known: true}}}
	unknown := known
	unknown.ID = "unknown"
	unknown.Cost.InputTokens = Estimate{}
	got, err := Select(Request{Capability: "code.literal"}, []Candidate{unknown, known})
	if err != nil || got.Tool != "direct" {
		t.Fatalf("selection = %+v, %v", got, err)
	}
}

func TestSelectionRejectsInvalidEvidenceAndMissingCapability(t *testing.T) {
	for _, c := range []Candidate{
		{ID: "bad", Authorized: true, Compatible: true, Available: true, Capabilities: []string{"code.symbol"}, Coverage: math.NaN(), Reliability: 1},
		{ID: "wrong-domain", Authorized: true, Compatible: true, Available: true, Capabilities: []string{"memory.read"}, Coverage: 1, Reliability: 1},
	} {
		if _, err := Select(Request{Capability: "code.symbol"}, []Candidate{c}); err == nil {
			t.Fatal("accepted invalid evidence or wrong domain")
		}
	}
}

func TestSelectionStableAndFreshnessIsRequirement(t *testing.T) {
	a := Candidate{ID: "a", Authorized: true, Compatible: true, Available: true, Capabilities: []string{"code.impact"}, Coverage: 1, Reliability: 1, Fresh: true}
	b := a
	b.ID = "b"
	a.Fresh = false
	got, err := Select(Request{Capability: "code.impact", RequireFresh: true}, []Candidate{a, b})
	if err != nil || got.Tool != "b" {
		t.Fatalf("selection=%+v %v", got, err)
	}
}
