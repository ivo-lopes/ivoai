package benchmarkfixtures

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"io"
	"strconv"
	"unicode/utf8"
)

// ExecObservation contains only fixed enums and counts. IDs, tool names,
// payloads and repetition fingerprints remain bounded, invocation-local RAM.
// Counts describe the client's partial JSONL projection, never actual totals.
type ExecObservation struct {
	SchemaVersion            int            `json:"schema_version"`
	StreamStatus             string         `json:"stream_status"`
	Coverage                 string         `json:"coverage"`
	ToolCompletions          map[string]int `json:"tool_completions"`
	RepeatedExactPayloads    map[string]int `json:"repeated_exact_payloads"`
	CollaborationCompletions map[string]int `json:"collaboration_completions"`
	ReportedSpawnReceivers   *int           `json:"reported_spawn_receivers"`
	UnclassifiedErrors       int            `json:"unclassified_errors"`
	DuplicateTerminalItems   int            `json:"duplicate_terminal_items"`
	UnknownEvents            int            `json:"unknown_events"`
	UnknownItems             int            `json:"unknown_items"`
	NativeWorkers            *int           `json:"native_workers"`
	NativeDelegations        *int           `json:"native_delegations"`
	NativeRetries            *int           `json:"native_retries"`
}

type ExecObservationResult struct {
	Observation                                  ExecObservation
	Answer                                       string
	HasUsage                                     bool
	InputTokens, CachedInputTokens, OutputTokens int64
	ToolCalls, ToolResultBytes                   int
}

func emptyExecObservation() ExecObservation {
	return ExecObservation{SchemaVersion: 1, StreamStatus: "invalid", Coverage: "partial_client_projection",
		ToolCompletions:          map[string]int{"command_execution": 0, "mcp_tool_call": 0, "file_change": 0, "web_search": 0, "collab_tool_call": 0},
		RepeatedExactPayloads:    map[string]int{"command_execution": 0, "mcp_tool_call": 0},
		CollaborationCompletions: map[string]int{"spawn_agent": 0, "send_input": 0, "wait": 0, "close_agent": 0}}
}

// ObserveExec implements the reviewed 0.162.1 JSONL shape, not a version probe.
// Missing/duplicate terminals, malformed/ambiguous JSON or incomplete calls
// cannot produce a complete observation. Unsupported items retain partial scope.
func ObserveExec(body []byte) ExecObservationResult {
	r := ExecObservationResult{Observation: emptyExecObservation()}
	if len(body) > 2<<20 || !utf8.Valid(body) {
		return r
	}
	thread, turn, terminal, failed, invalid := false, false, false, false, false
	completed := map[string][32]byte{}
	active := map[string]bool{}
	payloads := map[[32]byte]bool{}
	receivers := map[string]bool{}
	partial := false
	for _, line := range bytes.Split(body, []byte{'\n'}) {
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		event, ok := decodeExecObject(line)
		if !ok {
			invalid = true
			continue
		}
		kind, _ := event["type"].(string)
		if terminal {
			invalid = true
			continue
		}
		switch kind {
		case "thread.started":
			id, _ := event["thread_id"].(string)
			if thread || turn || !execID(id) {
				invalid = true
			}
			thread = true
		case "turn.started":
			if !thread || turn {
				invalid = true
			}
			turn = true
		case "turn.completed":
			if !turn || len(active) != 0 {
				invalid = true
			}
			usage, ok := event["usage"].(map[string]any)
			input, okI := execInteger(usage["input_tokens"])
			cache, okC := execInteger(usage["cached_input_tokens"])
			output, okO := execInteger(usage["output_tokens"])
			if !ok || !okI || !okC || !okO || cache > input {
				invalid = true
			} else {
				r.HasUsage = true
				r.InputTokens = input
				r.CachedInputTokens = cache
				r.OutputTokens = output
			}
			terminal = true
		case "turn.failed":
			if !turn {
				invalid = true
			}
			terminal = true
			failed = true
		case "error":
			if !turn {
				invalid = true
			}
			r.Observation.UnclassifiedErrors++
			// The projection carries no typed retry/willRetry field. Never classify
			// English error text as an attested retry or persist that text.
		case "item.started", "item.updated", "item.completed":
			if !turn {
				invalid = true
			}
			item, ok := event["item"].(map[string]any)
			if !ok {
				invalid = true
				continue
			}
			id, _ := item["id"].(string)
			typ, _ := item["type"].(string)
			if !execID(id) || len(completed)+len(active) >= 1024 {
				invalid = true
				continue
			}
			_, tool := r.Observation.ToolCompletions[typ]
			known := tool || typ == "agent_message" || typ == "reasoning" || typ == "todo_list" || typ == "error"
			if !known {
				r.Observation.UnknownItems++
				partial = true
			}
			if kind != "item.completed" {
				if tool {
					active[id] = true
				}
				continue
			}
			raw, _ := json.Marshal(item)
			digest := sha256.Sum256(raw)
			if previous, seen := completed[id]; seen {
				if previous != digest {
					invalid = true
				} else {
					r.Observation.DuplicateTerminalItems++
				}
				continue
			}
			completed[id] = digest
			delete(active, id)
			if typ == "agent_message" {
				text, ok := item["text"].(string)
				if !ok {
					invalid = true
				}
				r.Answer = text
			}
			if typ == "error" {
				r.Observation.UnclassifiedErrors++
			}
			if !tool {
				continue
			}
			status, _ := item["status"].(string)
			validStatus := typ == "web_search" || status == "completed" || status == "failed" || typ == "command_execution" && status == "declined"
			if !validStatus {
				invalid = true
				continue
			}
			r.Observation.ToolCompletions[typ]++
			var identity any
			switch typ {
			case "command_execution":
				command, ok := item["command"].(string)
				if !ok {
					invalid = true
					continue
				}
				output, ok := item["aggregated_output"].(string)
				if !ok {
					invalid = true
					continue
				}
				r.ToolCalls++
				r.ToolResultBytes += len(output)
				identity = []any{typ, command}
			case "mcp_tool_call":
				server, okS := item["server"].(string)
				name, okT := item["tool"].(string)
				if !okS || !okT {
					invalid = true
					continue
				}
				r.ToolCalls++
				identity = []any{typ, server, name, item["arguments"]}
			case "collab_tool_call":
				collab, _ := item["tool"].(string)
				if _, known := r.Observation.CollaborationCompletions[collab]; !known {
					r.Observation.UnknownItems++
					partial = true
					continue
				}
				sender, _ := item["sender_thread_id"].(string)
				list, ok := item["receiver_thread_ids"].([]any)
				if !execID(sender) || !ok || len(list) > 64 {
					invalid = true
					continue
				}
				ids := map[string]bool{}
				valid := true
				for _, v := range list {
					id, ok := v.(string)
					if !ok || !execID(id) || ids[id] {
						valid = false
						break
					}
					ids[id] = true
				}
				if !valid {
					invalid = true
					continue
				}
				r.Observation.CollaborationCompletions[collab]++
				if collab == "spawn_agent" && status == "completed" && len(ids) > 0 {
					for id := range ids {
						if len(receivers) >= 1024 {
							invalid = true
							break
						}
						receivers[id] = true
					}
					n := len(receivers)
					r.Observation.ReportedSpawnReceivers = &n
				}
			}
			if identity != nil {
				encoded, _ := json.Marshal(identity)
				fingerprint := sha256.Sum256(encoded)
				if payloads[fingerprint] {
					r.Observation.RepeatedExactPayloads[typ]++
				}
				payloads[fingerprint] = true
			}
		default:
			r.Observation.UnknownEvents++
			partial = true
		}
	}
	if invalid || !thread || !turn || !terminal {
		r.HasUsage = false
		return r
	}
	if failed {
		r.Observation.StreamStatus = "failed"
		return r
	}
	if partial {
		r.Observation.StreamStatus = "partial"
		return r
	}
	r.Observation.StreamStatus = "complete"
	return r
}

func execID(value string) bool { return value != "" && len(value) <= 256 }
func execInteger(value any) (int64, bool) {
	number, ok := value.(json.Number)
	if !ok {
		return 0, false
	}
	v, err := strconv.ParseInt(string(number), 10, 64)
	return v, err == nil && v >= 0 && v <= 1<<53-1
}

// Decode all keys rather than accepting encoding/json's last duplicate wins.
// A bounded token walk rejects ambiguous nested payloads without storing them.
func decodeExecObject(body []byte) (map[string]any, bool) {
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	tokens := 0
	var read func(int) (any, bool)
	read = func(depth int) (any, bool) {
		tokens++
		if depth > 48 || tokens > 32768 {
			return nil, false
		}
		token, err := decoder.Token()
		if err != nil {
			return nil, false
		}
		delimiter, container := token.(json.Delim)
		if !container {
			return token, true
		}
		switch delimiter {
		case '{':
			value := map[string]any{}
			for decoder.More() {
				key, err := decoder.Token()
				name, ok := key.(string)
				if err != nil || !ok || len(value) >= 1024 {
					return nil, false
				}
				if _, exists := value[name]; exists {
					return nil, false
				}
				v, ok := read(depth + 1)
				if !ok {
					return nil, false
				}
				value[name] = v
			}
			end, err := decoder.Token()
			return value, err == nil && end == json.Delim('}')
		case '[':
			value := []any{}
			for decoder.More() {
				if len(value) >= 4096 {
					return nil, false
				}
				v, ok := read(depth + 1)
				if !ok {
					return nil, false
				}
				value = append(value, v)
			}
			end, err := decoder.Token()
			return value, err == nil && end == json.Delim(']')
		}
		return nil, false
	}
	value, ok := read(0)
	if !ok {
		return nil, false
	}
	if _, err := decoder.Token(); err != io.EOF {
		return nil, false
	}
	object, ok := value.(map[string]any)
	return object, ok
}
