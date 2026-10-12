package externalmcp

import (
	"context"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
)

func TestLocalCapabilitySharesAdmissionAndInventoryBoundary(t *testing.T) {
	var calls atomic.Int32
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(string(body), "tools/list") {
			io.WriteString(w, `{"jsonrpc":"2.0","id":1,"result":{"tools":[{"name":"list_projects"},{"name":"forbidden"}]}}`)
			return
		}
		calls.Add(1)
		io.WriteString(w, `{"jsonrpc":"2.0","id":1,"result":{"content":[]}}`)
	})
	g, e := Start([]Target{{Name: "native-code", Local: handler, Restricted: true, AllowedTools: []string{"list_projects"}}}, "full")
	if e != nil {
		t.Fatal(e)
	}
	defer g.Close()
	for _, body := range []string{toolCall, strings.ReplaceAll(toolCall, "list_projects", "forbidden"), `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`} {
		r, e := gatewayRequest(context.Background(), g, 0, body)
		if e != nil {
			t.Fatal(e)
		}
		data, _ := io.ReadAll(r.Body)
		r.Body.Close()
		if strings.Contains(body, "tools/list") && strings.Contains(string(data), "forbidden") {
			t.Fatal("ungranted tool disclosed")
		}
	}
	if calls.Load() != 1 {
		t.Fatalf("invalid local calls %d", calls.Load())
	}
	g.SetAdmission(func() bool { return false })
	r, e := gatewayRequest(context.Background(), g, 0, toolCall)
	if e != nil {
		t.Fatal(e)
	}
	r.Body.Close()
	if calls.Load() != 1 {
		t.Fatal("local handler bypassed admission")
	}
}
