package codebase

import (
	"bytes"
	"encoding/json"
	"io"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// The duplicate-key/depth check prevents contradictory budget/truncation flags
// and expensive deeply nested data. Arbitrary keys/strings never become policy.
func boundedJSON(raw []byte) bool {
	if len(raw) == 0 || len(raw) > MaxReplyBytes || !utf8.Valid(raw) {
		return false
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	nodes := 0
	var read func(int) bool
	read = func(depth int) bool {
		nodes++
		if depth > 16 || nodes > 4096 {
			return false
		}
		v, err := d.Token()
		if err != nil {
			return false
		}
		delimiter, ok := v.(json.Delim)
		if !ok {
			return true
		}
		switch delimiter {
		case '{':
			keys := map[string]bool{}
			for d.More() {
				k, e := d.Token()
				s, ok := k.(string)
				if e != nil || !ok || keys[s] {
					return false
				}
				keys[s] = true
				if !read(depth + 1) {
					return false
				}
			}
			v, e := d.Token()
			return e == nil && v == json.Delim('}')
		case '[':
			for d.More() {
				if !read(depth + 1) {
					return false
				}
			}
			v, e := d.Token()
			return e == nil && v == json.Delim(']')
		}
		return false
	}
	if !read(0) {
		return false
	}
	_, err := d.Token()
	return err == io.EOF
}
func booleanFields(raw json.RawMessage, keys ...string) bool {
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil {
		return false
	}
	for _, key := range keys {
		if value, ok := fields[key]; ok {
			var b bool
			if bytes.Equal(bytes.TrimSpace(value), []byte("null")) || json.Unmarshal(value, &b) != nil {
				return false
			}
		}
	}
	return true
}
func toolPayload(raw json.RawMessage) (json.RawMessage, error) {
	if !boundedJSON(raw) || !booleanFields(raw, "isError") {
		return nil, ErrResponse
	}
	var envelope struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		Structured    json.RawMessage `json:"structuredContent"`
		IsError       bool            `json:"isError"`
		ResultType    string          `json:"resultType"`
		InputRequests json.RawMessage `json:"inputRequests"`
		RequestState  json.RawMessage `json:"requestState"`
	}
	if json.Unmarshal(raw, &envelope) != nil || envelope.IsError || (envelope.ResultType != "" && envelope.ResultType != "complete") || len(envelope.InputRequests) > 0 || len(envelope.RequestState) > 0 {
		return nil, ErrResponse
	}
	var text json.RawMessage
	for _, content := range envelope.Content {
		if content.Type != "text" || text != nil {
			return nil, ErrResponse
		}
		text = json.RawMessage(content.Text)
	}
	structured := envelope.Structured
	if len(structured) > 0 && string(structured) != "null" {
		if !boundedJSON(structured) {
			return nil, ErrResponse
		}
		if text != nil {
			if !boundedJSON(text) {
				return nil, ErrResponse
			}
			var a, b any
			if json.Unmarshal(text, &a) != nil || json.Unmarshal(structured, &b) != nil {
				return nil, ErrResponse
			}
			aa, _ := json.Marshal(a)
			bb, _ := json.Marshal(b)
			if !bytes.Equal(aa, bb) {
				return nil, ErrResponse
			}
		}
		return bytes.Clone(structured), nil
	}
	if !boundedJSON(text) {
		return nil, ErrResponse
	}
	return bytes.Clone(text), nil
}

type symbol struct {
	Name          string `json:"name"`
	QualifiedName string `json:"qualified_name"`
	File          string `json:"file"`
	StartLine     int    `json:"start_line"`
	EndLine       int    `json:"end_line"`
}
type symbolPage struct {
	Symbols []symbol `json:"symbols"`
	Total   int      `json:"indexed_matches"`
	Partial bool     `json:"partial"`
}
type group struct {
	Prefix *string             `json:"qn_prefix"`
	File   *string             `json:"file"`
	Rows   [][]json.RawMessage `json:"rows"`
}

var linesPattern = regexp.MustCompile(`^([1-9][0-9]{0,7})(?:-([1-9][0-9]{0,7}))?$`)

func nameJSON(raw json.RawMessage) (string, bool) {
	var s string
	if json.Unmarshal(raw, &s) != nil || len(s) > 256 || !identifier.MatchString(s) {
		return "", false
	}
	return s, true
}
func qname(prefix, name string) (string, bool) {
	if prefix == "" {
		return name, true
	}
	full := prefix + "." + name
	return full, len(full) <= 256 && identifier.MatchString(full)
}
func safeFile(s string) bool {
	return s != "" && len(s) <= 2048 && utf8.ValidString(s) && filepath.IsLocal(s) && filepath.Clean(s) == s && !strings.ContainsAny(s, "\\\x00\r\n\t\x1b") && !strings.ContainsFunc(s, func(r rune) bool {
		return unicode.IsControl(r) || r >= 0x202a && r <= 0x202e || r >= 0x2066 && r <= 0x2069
	})
}
func exactCols(cols []string, want ...string) bool {
	if len(cols) != len(want) {
		return false
	}
	for i, c := range cols {
		if c != want[i] {
			return false
		}
	}
	return true
}
func parseSymbols(raw json.RawMessage, requested string, limit int) (symbolPage, error) {
	var value struct {
		Cols      []string        `json:"cols"`
		Groups    []group         `json:"groups"`
		Total     *int            `json:"total"`
		Returned  *int            `json:"returned"`
		Count     *int            `json:"count"`
		HasMore   *bool           `json:"has_more"`
		Truncated *bool           `json:"truncated"`
		Error     json.RawMessage `json:"error"`
	}
	invalid := symbolPage{}
	if !boundedJSON(raw) || json.Unmarshal(raw, &value) != nil || len(value.Error) > 0 || value.Groups == nil || value.Total == nil || value.Returned == nil || value.Count == nil || value.HasMore == nil || value.Truncated == nil || !exactCols(value.Cols, "name", "label", "lines", "in", "out") {
		return invalid, ErrResponse
	}
	result := symbolPage{Symbols: []symbol{}, Total: *value.Total, Partial: *value.HasMore || *value.Truncated}
	if result.Total < 0 || result.Total > 10000000 {
		return invalid, ErrResponse
	}
	seen := map[string]bool{}
	for _, g := range value.Groups {
		if g.Prefix == nil || g.File == nil || g.Rows == nil || !safeFile(*g.File) {
			return invalid, ErrResponse
		}
		for _, row := range g.Rows {
			if len(row) != 5 || len(result.Symbols) >= limit {
				return invalid, ErrResponse
			}
			name, ok := nameJSON(row[0])
			if !ok {
				return invalid, ErrResponse
			}
			full, ok := qname(*g.Prefix, name)
			if !ok || seen[full] || strings.Contains(requested, ".") && full != requested || !strings.Contains(requested, ".") && name != requested {
				return invalid, ErrResponse
			}
			seen[full] = true
			var label, line string
			var in, out int
			if json.Unmarshal(row[1], &label) != nil || (label != "Function" && label != "Method") || json.Unmarshal(row[2], &line) != nil || bytes.Equal(row[3], []byte("null")) || bytes.Equal(row[4], []byte("null")) || json.Unmarshal(row[3], &in) != nil || json.Unmarshal(row[4], &out) != nil || in < 0 || out < 0 {
				return invalid, ErrResponse
			}
			span := linesPattern.FindStringSubmatch(line)
			if span == nil {
				return invalid, ErrResponse
			}
			first, _ := strconv.Atoi(span[1])
			last := first
			if span[2] != "" {
				last, _ = strconv.Atoi(span[2])
			}
			if first > last || last > 10000000 {
				return invalid, ErrResponse
			}
			result.Symbols = append(result.Symbols, symbol{Name: name, QualifiedName: full, File: *g.File, StartLine: first, EndLine: last})
		}
	}
	if *value.Count != len(result.Symbols) || *value.Returned != len(result.Symbols) || result.Total < len(result.Symbols) || result.Total > len(result.Symbols) && !result.Partial {
		return invalid, ErrResponse
	}
	return result, nil
}

type relation struct {
	QualifiedName string   `json:"qualified_name"`
	Hop           int      `json:"hop"`
	Test          bool     `json:"test"`
	Strategy      *string  `json:"strategy,omitempty"`
	Confidence    *float64 `json:"confidence,omitempty"`
}
type tracePage struct {
	Target           string     `json:"target"`
	Rows             []relation `json:"rows"`
	IndexedReachable int        `json:"indexed_callers"`
	Partial          bool       `json:"partial"`
	Basis            string     `json:"basis"`
}

var strategyPattern = regexp.MustCompile(`^[A-Za-z0-9_.:/-]{1,64}$`)

func parseTrace(raw json.RawMessage, target string, limit int) (tracePage, error) {
	var value struct {
		Function      string `json:"function"`
		Direction     string `json:"direction"`
		Mode          string `json:"mode"`
		Total         *int   `json:"callers_total"`
		TotalRelation string `json:"callers_total_relation"`
		Callers       struct {
			Cols   []string `json:"cols"`
			Groups []group  `json:"groups"`
		} `json:"callers"`
		Truncated bool            `json:"truncated"`
		HasMore   bool            `json:"has_more"`
		Error     json.RawMessage `json:"error"`
	}
	invalid := tracePage{}
	if !boundedJSON(raw) || !booleanFields(raw, "truncated", "has_more") || json.Unmarshal(raw, &value) != nil || value.Callers.Groups == nil || len(value.Error) > 0 || value.Function != target || value.Direction != "inbound" || value.Mode != "calls" || value.Total == nil || *value.Total < 0 || *value.Total > 5000 || (value.TotalRelation != "eq" && value.TotalRelation != "gte") || !exactCols(value.Callers.Cols, "name", "hop", "test", "strategy", "confidence") {
		return invalid, ErrResponse
	}
	result := tracePage{Target: target, Rows: []relation{}, IndexedReachable: *value.Total, Partial: value.Truncated || value.HasMore || value.TotalRelation != "eq", Basis: "inferred_static_relationships_not_runtime_execution"}
	seen := map[string]bool{}
	for _, g := range value.Callers.Groups {
		if g.Prefix == nil || g.Rows == nil {
			return invalid, ErrResponse
		}
		for _, row := range g.Rows {
			if len(row) != 5 || len(result.Rows) >= limit {
				return invalid, ErrResponse
			}
			name, ok := nameJSON(row[0])
			if !ok {
				return invalid, ErrResponse
			}
			full, ok := qname(*g.Prefix, name)
			if !ok || seen[full] {
				return invalid, ErrResponse
			}
			seen[full] = true
			r := relation{QualifiedName: full}
			if json.Unmarshal(row[1], &r.Hop) != nil || r.Hop != 1 || bytes.Equal(row[2], []byte("null")) || json.Unmarshal(row[2], &r.Test) != nil || json.Unmarshal(row[3], &r.Strategy) != nil || json.Unmarshal(row[4], &r.Confidence) != nil {
				return invalid, ErrResponse
			}
			if r.Strategy != nil && !strategyPattern.MatchString(*r.Strategy) || r.Confidence != nil && (*r.Confidence < 0 || *r.Confidence > 1) {
				return invalid, ErrResponse
			}
			result.Rows = append(result.Rows, r)
		}
	}
	if len(result.Rows) > result.IndexedReachable || !result.Partial && len(result.Rows) != result.IndexedReachable {
		return invalid, ErrResponse
	}
	return result, nil
}
