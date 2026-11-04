package core

import (
	"strings"
	"testing"
)

func TestPrimaryTurnOptionalHistoryDigestIsStrictAndBackwardCompatible(t *testing.T) {
	digest := strings.Repeat("a", 64)
	r := PrimaryTurnReceipt{ID: "receipt", TurnID: "turn", AttemptID: "attempt", FrontendSessionID: "frontend", Executor: "codex", Mode: PrimaryReadOnly, ConfigDigest: digest, StateDigest: digest, ScopeDigest: digest}
	if err := r.Validate(); err != nil {
		t.Fatal("legacy receipt rejected", err)
	}
	r.HistoryDigest = digest
	if err := r.Validate(); err != nil {
		t.Fatal("bounded history digest rejected", err)
	}
	for _, bad := range []string{"abc", strings.Repeat("z", 64), strings.Repeat("a", 65), "\n" + digest} {
		r.HistoryDigest = bad
		if r.Validate() == nil {
			t.Fatal("invalid history binding accepted")
		}
	}
}
