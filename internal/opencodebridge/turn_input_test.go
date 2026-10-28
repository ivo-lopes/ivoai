package opencodebridge

import (
	"context"
	"encoding/json"
	"testing"
)

func TestSingleUserInputRequiresEntireLiteralEnvelope(t *testing.T) {
	for _, test := range []struct {
		body string
		want bool
	}{
		{`[{"role":"user","content":"What is the project version?"}]`, true},
		{`[]`, false},
		{`[{"role":"system","content":"question"},{"role":"user","content":"question"}]`, false},
		{`[{"role":"developer","content":"question"},{"role":"user","content":"question"}]`, false},
		{`[{"role":"assistant","content":"history"},{"role":"user","content":"question"}]`, false},
		{`[{"role":"user","content":[{"type":"text","text":"question"}]}]`, false},
	} {
		var messages []chatMessage
		if err := json.Unmarshal([]byte(test.body), &messages); err != nil {
			t.Fatal(err)
		}
		if got := singleUserInput(messages); got != test.want {
			t.Fatalf("envelope admission %s = %v", test.body, got)
		}
	}
	if SingleUserMessage(context.Background()) {
		t.Fatal("absence of transport observation granted eligibility")
	}
}
