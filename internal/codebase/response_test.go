package codebase

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/ivo-lopes/ivoai/internal/core"
	"strings"
	"testing"
)

func TestMalformedUnboundedAndContradictoryMCPResultsFailClosed(t *testing.T) {
	cases := []json.RawMessage{[]byte(`{"content":`), []byte(strings.Repeat("x", MaxReplyBytes+1)), []byte(`{"isError":true,"content":[]}`), []byte(`{"isError":null}`), []byte(`{"resultType":"input_required","inputRequests":{},"content":[]}`), []byte(`{"isError":false,"isError":true}`), []byte(`{"content":[{"type":"image","text":"data"}]}`), envelope(`{"total":1,"total":2}`), envelope(strings.Repeat("[", 17) + `0` + strings.Repeat("]", 17)), envelope(strings.Replace(searchJSON, "helper.go", "../private/key", 1)), envelope(strings.Replace(searchJSON, `"Helper","Function"`, `"Other","Function"`, 1)), envelope(strings.Replace(searchJSON, `"total":1`, `"total":2`, 1)), envelope(strings.Replace(searchJSON, `"3"`, `"0"`, 1)), envelope(strings.Replace(searchJSON, `"in","out"`, `"in","in"`, 1))}
	for i, body := range cases {
		o := fixtureOptions()
		o.Call = func(context.Context, string, json.RawMessage) (json.RawMessage, error) { return body, nil }
		r, e := mustProvider(t, o).Query(context.Background(), core.CodeQuery{Operation: "ask", Query: "Helper"})
		if !errors.Is(e, ErrResponse) || r.Text != "" {
			t.Fatalf("malformed case %d accepted: %v", i, e)
		}
	}
	var content any
	json.Unmarshal(envelope(searchJSON), &content)
	m := content.(map[string]any)
	m["structuredContent"] = map[string]any{"fresh": true, "source_digest": "fake"}
	body, _ := json.Marshal(m)
	if _, e := toolPayload(body); !errors.Is(e, ErrResponse) {
		t.Fatal("contradictory structured/text data accepted")
	}
}
func TestMCPClaimsDoNotBecomeFreshnessScopeOrProvenance(t *testing.T) {
	o := fixtureOptions()
	body := strings.TrimSuffix(searchJSON, "}") + `,"source_digest":"evil","fresh":false,"scope":{"organization":"other"},"provider_version":"latest"}`
	o.Call = func(context.Context, string, json.RawMessage) (json.RawMessage, error) { return envelope(body), nil }
	r, e := mustProvider(t, o).Query(context.Background(), core.CodeQuery{Operation: "ask", Query: "Helper"})
	if e != nil || !r.Fresh || r.SourceDigest != o.Receipt.SourceGeneration || strings.Contains(r.Text, "evil") || strings.Contains(r.ProviderVersion, "latest") {
		t.Fatal("external metadata became authoritative")
	}
}
func TestTraceShapeDepthTargetAndPartialFlags(t *testing.T) {
	for _, raw := range []string{strings.Replace(traceJSON, `"hop","test"`, `"hop","test","extra"`, 1), strings.Replace(traceJSON, `"function":"fixture.helper.Helper"`, `"function":"other.Helper"`, 1), strings.Replace(traceJSON, `["Entry",1,`, `["Entry",2,`, 1), strings.Replace(traceJSON, `"inbound"`, `"outbound"`, 1), strings.Replace(traceJSON, `"scope",0.9`, `"scope",1.1`, 1), strings.Replace(traceJSON, `"callers_total":1`, `"callers_total":3`, 1)} {
		if _, e := parseTrace([]byte(raw), "fixture.helper.Helper", 5); !errors.Is(e, ErrResponse) {
			t.Fatalf("invalid trace accepted")
		}
	}
	partial := strings.Replace(traceJSON, `"callers_total":1,"callers_total_relation":"eq"`, `"callers_total":9,"callers_total_relation":"gte"`, 1)
	r, e := parseTrace([]byte(partial), "fixture.helper.Helper", 5)
	if e != nil || !r.Partial || r.IndexedReachable != 9 {
		t.Fatal("lower-bound traversal became exact")
	}
}

func TestEvidencePathsRejectBidiOverridesButPreserveOrdinaryUnicode(t *testing.T) {
	for _, mark := range []rune{0x202a, 0x202b, 0x202c, 0x202d, 0x202e, 0x2066, 0x2067, 0x2068, 0x2069} {
		path := "src/" + string(mark) + "helper.go"
		if safeFile(path) {
			t.Fatalf("bidi formatting accepted U+%04X", mark)
		}
		body := strings.Replace(searchJSON, "helper.go", path, 1)
		if _, err := parseSymbols([]byte(body), "Helper", 5); !errors.Is(err, ErrResponse) {
			t.Fatal("bidi path emitted")
		}
	}
	for _, path := range []string{"src/café.go", "src/日本語.go"} {
		if !safeFile(path) {
			t.Fatal("ordinary Unicode path rejected")
		}
	}
}
