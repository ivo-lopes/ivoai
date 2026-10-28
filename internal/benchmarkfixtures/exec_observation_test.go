package benchmarkfixtures

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func observationStream(items ...map[string]any) []byte {
	var body bytes.Buffer
	encoder := json.NewEncoder(&body)
	encoder.Encode(map[string]any{"type": "thread.started", "thread_id": "private-thread"})
	encoder.Encode(map[string]any{"type": "turn.started"})
	for _, item := range items {
		encoder.Encode(map[string]any{"type": "item.completed", "item": item})
	}
	encoder.Encode(map[string]any{"type": "item.completed", "item": map[string]any{"id": "final", "type": "agent_message", "text": "FINAL"}})
	encoder.Encode(map[string]any{"type": "turn.completed", "usage": map[string]any{"input_tokens": 100, "cached_input_tokens": 60, "output_tokens": 2}})
	return body.Bytes()
}
func observationCommand(id, command string) map[string]any {
	return map[string]any{"id": id, "type": "command_execution", "status": "completed", "command": command, "aggregated_output": "PRIVATE_OUTPUT"}
}
func TestExecObservationCountsWithoutPersistingPayloads(t *testing.T) {
	a := observationCommand("private-id-a", "cat /private/path?SECRET=key")
	b := observationCommand("private-id-b", "cat /private/path?SECRET=key")
	c := map[string]any{"id": "mcp1", "type": "mcp_tool_call", "status": "failed", "server": "SECRET_SERVER", "tool": "SECRET_TOOL", "arguments": map[string]any{"SECRET": "PRIVATE_ARGUMENT"}}
	d := map[string]any{}
	for k, v := range c {
		d[k] = v
	}
	d["id"] = "mcp2"
	result := ObserveExec(observationStream(a, a, b, c, d))
	o := result.Observation
	if o.StreamStatus != "complete" || !result.HasUsage || result.InputTokens != 100 || result.CachedInputTokens != 60 || result.Answer != "FINAL" || result.ToolCalls != 4 || o.ToolCompletions["command_execution"] != 2 || o.ToolCompletions["mcp_tool_call"] != 2 || o.DuplicateTerminalItems != 1 || o.RepeatedExactPayloads["command_execution"] != 1 || o.RepeatedExactPayloads["mcp_tool_call"] != 1 {
		t.Fatal("incorrect unique terminal or exact-payload observation")
	}
	encoded, _ := json.Marshal(o)
	for _, secret := range []string{"PRIVATE", "SECRET", "private-id", "private-thread", "/private/path"} {
		if bytes.Contains(encoded, []byte(secret)) {
			t.Fatal("observation leaked an identifier or payload")
		}
	}
	if o.NativeWorkers != nil || o.NativeDelegations != nil || o.NativeRetries != nil || o.ReportedSpawnReceivers != nil {
		t.Fatal("absence became a real-world zero")
	}
}
func TestExecObservationCollaborationIsOnlyReportedLowerBound(t *testing.T) {
	spawn := func(id string, receivers []string, status string) map[string]any {
		return map[string]any{"id": id, "type": "collab_tool_call", "tool": "spawn_agent", "sender_thread_id": "SECRET_SENDER", "receiver_thread_ids": receivers, "prompt": "SECRET_PROMPT", "agents_states": map[string]any{}, "status": status}
	}
	result := ObserveExec(observationStream(spawn("s1", []string{"SECRET_R1", "SECRET_R2"}, "completed"), spawn("s2", []string{"SECRET_R1"}, "completed"), spawn("s3", []string{"SECRET_R3"}, "failed")))
	o := result.Observation
	if o.StreamStatus != "complete" || o.CollaborationCompletions["spawn_agent"] != 3 || o.ReportedSpawnReceivers == nil || *o.ReportedSpawnReceivers != 2 || o.NativeWorkers != nil || o.NativeDelegations != nil || o.NativeRetries != nil {
		t.Fatal("reported recipient identity counted as actual worker/delegation total")
	}
	encoded, _ := json.Marshal(o)
	if bytes.Contains(encoded, []byte("SECRET")) {
		t.Fatal("collaboration identities/prompt leaked")
	}
	missing := ObserveExec(observationStream())
	if missing.Observation.ReportedSpawnReceivers != nil {
		t.Fatal("missing spawn projected as measured zero")
	}
}
func TestExecObservationRejectsIncompleteAndAmbiguousStreams(t *testing.T) {
	good := observationStream(observationCommand("c1", "safe"))
	cases := map[string][]byte{
		"missing_terminal":   good[:bytes.LastIndex(good, []byte(`{"type":"turn.completed"`))],
		"duplicate_terminal": append(bytes.Clone(good), []byte(`{"type":"turn.completed","usage":{"input_tokens":1,"cached_input_tokens":0,"output_tokens":1}}`)...),
		"malformed_line":     append([]byte("PRIVATE_NOT_JSON\n"), good...),
		"invalid_utf8":       bytes.Replace(good, []byte("safe"), []byte{0xff}, 1),
		"duplicate_key":      bytes.Replace(good, []byte(`"input_tokens":100`), []byte(`"input_tokens":100,"input_tokens":2`), 1),
		"fractional_usage":   bytes.Replace(good, []byte(`"input_tokens":100`), []byte(`"input_tokens":1.5`), 1),
		"wrong_cache":        bytes.Replace(good, []byte(`"cached_input_tokens":60`), []byte(`"cached_input_tokens":101`), 1),
		"incomplete_tool":    bytes.Replace(good, []byte(`"type":"item.completed"`), []byte(`"type":"item.started"`), 1),
		"conflicting_id":     observationStream(observationCommand("same", "one"), observationCommand("same", "two")),
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			r := ObserveExec(body)
			if r.Observation.StreamStatus != "invalid" || r.HasUsage {
				t.Fatal("ambiguous/incomplete stream accepted")
			}
		})
	}
}
func TestExecObservationUnknownAndErrorsRemainPartial(t *testing.T) {
	unknown := ObserveExec(observationStream(map[string]any{"id": "unknown", "type": "PRIVATE_UNKNOWN_TYPE", "status": "completed"}))
	if unknown.Observation.StreamStatus != "partial" || unknown.Observation.UnknownItems != 1 {
		t.Fatal("unknown item silently treated as full coverage")
	}
	body := observationStream()
	marker := []byte(`{"type":"turn.started"}`)
	body = bytes.Replace(body, marker, append(bytes.Clone(marker), []byte("\n{\"type\":\"error\",\"message\":\"Reconnecting PRIVATE_RETRY_SECRET\"}")...), 1)
	result := ObserveExec(body)
	if result.Observation.StreamStatus != "complete" || result.Observation.UnclassifiedErrors != 1 || result.Observation.NativeRetries != nil {
		t.Fatal("message text invented a retry count")
	}
}
func TestExecObservationBounds(t *testing.T) {
	if ObserveExec([]byte(strings.Repeat(" ", (2<<20)+1))).Observation.StreamStatus != "invalid" {
		t.Fatal("oversized stream accepted")
	}
	items := []map[string]any{}
	for n := 0; n < 1025; n++ {
		items = append(items, observationCommand(strings.Repeat("x", n%255+1)+string(rune(n+256)), "command"))
	}
	if ObserveExec(observationStream(items...)).Observation.StreamStatus != "invalid" {
		t.Fatal("unbounded identities accepted")
	}
	if _, ok := decodeExecObject([]byte(strings.Repeat("[", 50) + "0" + strings.Repeat("]", 50))); ok {
		t.Fatal("deep JSON accepted")
	}
}
