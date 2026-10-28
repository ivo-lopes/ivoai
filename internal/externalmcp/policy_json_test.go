package externalmcp

import (
	"strings"
	"testing"
)

func TestPolicyRequestRejectsAmbiguousIdentities(t *testing.T) {
	for _, raw := range []string{
		`{"jsonrpc":"2.0","method":"tools/call","params":{"name":"read","name":"write"}}`,
		`{"jsonrpc":"2.0","method":"tools/call","params":{"Name":"read"}}`,
		`{"jsonrpc":"2.0","Method":"tools/call","params":{"name":"read"}}`,
		`{"jsonrpc":"2.0","method":"tools/call","params":{"name":"read","arguments":{"q":1,"q":2}}}`,
		`{"jsonrpc":"2.0"} {"method":"tools/call"}`,
		strings.Repeat("[", 34) + `0` + strings.Repeat("]", 34),
	} {
		if ValidPolicyRequest([]byte(raw)) {
			t.Fatal("ambiguous request admitted")
		}
	}
	for _, raw := range []string{
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"read","arguments":{"A":"first","a":"second"}}}`,
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"clientInfo":{"name":"fixture"}}}`,
	} {
		if !ValidPolicyRequest([]byte(raw)) {
			t.Fatal("valid RPC or case-sensitive argument data rejected")
		}
	}
}
