package codeintel

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestNativeToolAliasesAreBoundedAndCannotSetScope(t *testing.T) {
	for name, op := range map[string]string{"graft_find_code": "ask", "graft_find_all": "grep", "graft_trace_calls": "callers", "graft_file_api": "skeleton", "graft_repo_map": "map", "graft_check_freshness": "check"} {
		q, e := toolQuery(name, json.RawMessage(`{"query":"Run","file":"main.go","symbol":"Run"}`))
		if e != nil || q.Operation != op {
			t.Fatalf("%s %+v %v", name, q, e)
		}
	}
	if _, e := toolQuery("code_inspect", json.RawMessage(`{"operation":"map","root":"/private"}`)); e == nil {
		t.Fatal("caller supplied root")
	}
	if _, e := toolQuery("graft_trace_calls", json.RawMessage(`{"symbol":"Run","depth":"all"}`)); e == nil {
		t.Fatal("unbounded depth")
	}
}

func TestNativeContractParameters(t *testing.T) {
	q, err := toolQuery("graft_find_all", json.RawMessage(`{"pattern":"Needle","in":"src","fixed":true,"ignore_case":true}`))
	if err != nil || q.Query != "Needle" {
		t.Fatalf("literal native contract: %+v %v", q, err)
	}
	for _, body := range []string{`{"pattern":"x","in":"../private"}`, `{"pattern":"x","fixed":false}`, `{"query":"x","full":true}`} {
		name := "graft_find_all"
		if strings.Contains(body, "full") {
			name = "graft_find_code"
		}
		if _, err := toolQuery(name, json.RawMessage(body)); err == nil {
			t.Fatalf("unsafe contract accepted: %s", body)
		}
	}
	q, err = toolQuery("graft_repo_map", json.RawMessage(`{"max_dirs":12}`))
	if err != nil || q.Limit != 12 {
		t.Fatalf("map contract: %+v %v", q, err)
	}
}
