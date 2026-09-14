package codexfrontend

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestHistoryDiscoveryFiltersUnknownAndFailsClosed(t *testing.T) {
	f := &Facade{options: Options{ThreadAvailable: func(id string) bool { return id == "owned" }}}
	for _, method := range []string{"thread/list", "thread/loaded/list"} {
		payload := `{"data":[{"id":"owned"},{"id":"external"}],"nextCursor":"next"}`
		if method == "thread/loaded/list" {
			payload = `{"data":["owned","external"]}`
		}
		got := f.filterHistory(method, rpc{ID: json.RawMessage(`1`), Result: json.RawMessage(payload)})
		if len(got.Error) != 0 || !strings.Contains(string(got.Result), "owned") || strings.Contains(string(got.Result), "external") {
			t.Fatal("unmanaged history crossed discovery scope")
		}
		for _, invalid := range []string{`null`, `{"data":{"private":"external"}}`, `{"unexpected":"external"}`} {
			got = f.filterHistory(method, rpc{ID: json.RawMessage(`1`), Result: json.RawMessage(invalid)})
			if len(got.Error) == 0 || len(got.Result) != 0 {
				t.Fatal("unknown history schema escaped fail-closed filtering")
			}
		}
	}
}
