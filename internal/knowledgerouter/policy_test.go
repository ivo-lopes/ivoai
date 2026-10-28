package knowledgerouter

import (
	"context"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/ivo-lopes/ivoai/internal/config"
)

func TestHostToolPolicyFiltersDiscoveryAndDeniesForgedWrites(t *testing.T) {
	source := newFakeSource(t, "fixture", "secret-fixture-token")
	source.response = []byte(`{"jsonrpc":"2.0","id":1,"result":{"tools":[{"name":"memory_read_page","inputSchema":{"type":"object"}},{"name":"memory_write_page","inputSchema":{"type":"object"}},{"name":"future_unknown","inputSchema":{"type":"object"}}]}}`)
	profiles := map[string]config.ServerProfile{"fixture": profile("fixture", "fixture", "", 0, source)}
	r := startTestRouter(t, profiles, []string{"fixture"}, map[string]*fakeSource{"fixture": source})
	var revoked atomic.Bool
	r.SetToolPolicy(func(_ context.Context, kind, tool string) bool {
		return !revoked.Load() && kind == "memory" && tool == "memory_read_page"
	})
	body, status := callRouter(t, r, "/mcp/memory", `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)
	if status != http.StatusOK || !strings.Contains(string(body), "memory_read_page") || strings.Contains(string(body), "memory_write_page") || strings.Contains(string(body), "future_unknown") {
		t.Fatalf("inventory was not narrowed: status=%d", status)
	}
	before := source.requests.Load()
	for _, payload := range []string{
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"memory_write_page","arguments":{"path":"ignored"}}}`,
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"future_unknown"}}`,
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"memory_write_page","name":"memory_read_page"}}`,
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"Name":"memory_read_page"}}`,
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","Method":"tools/list","params":{"name":"memory_read_page"}}`,
		`{"jsonrpc":"2.0","id":1,"method":"resources/read","params":{"uri":"arbitrary"}}`,
	} {
		body, _ := callRouter(t, r, "/mcp/memory", payload)
		if !strings.Contains(string(body), `"error"`) || source.requests.Load() != before {
			t.Fatal("forged or ambiguous RPC reached upstream")
		}
	}
	_, status = callRouter(t, r, "/memory/sessions/test/events", `{}`)
	if status != http.StatusForbidden || source.requests.Load() != before {
		t.Fatal("REST endpoint bypassed admitted tool capability")
	}
	source.response = nil
	body, status = callRouter(t, r, "/mcp/memory", `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"memory_read_page","arguments":{"query":"bounded"}}}`)
	if status != 200 || !strings.Contains(string(body), "fixture:projects/foo") || source.requests.Load() != before+1 {
		t.Fatal("permitted read failed")
	}
	revoked.Store(true)
	body, _ = callRouter(t, r, "/mcp/memory", `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"memory_read_page"}}`)
	if !strings.Contains(string(body), `"error"`) || source.requests.Load() != before+1 {
		t.Fatal("revoked capability reached upstream")
	}
}

func TestHostToolPolicyRechecksSingleAndFederatedResults(t *testing.T) {
	for _, federated := range []bool{false, true} {
		t.Run(map[bool]string{false: "single", true: "federated"}[federated], func(t *testing.T) {
			a := newFakeSource(t, "alpha", "token-a")
			profiles := map[string]config.ServerProfile{"alpha": profile("alpha", "alpha", "", 0, a)}
			sources := map[string]*fakeSource{"alpha": a}
			selectors := []string{"alpha"}
			if federated {
				b := newFakeSource(t, "beta", "token-b")
				profiles["beta"] = profile("beta", "beta", "", 0, b)
				sources["beta"] = b
				selectors = append(selectors, "beta")
			}
			r := startTestRouter(t, profiles, selectors, sources)
			var checks atomic.Int32
			r.SetToolPolicy(func(context.Context, string, string) bool { return checks.Add(1) == 1 })
			body, _ := callRouter(t, r, "/mcp/context", `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"context_search"}}`)
			if !strings.Contains(string(body), `"error"`) || strings.Contains(string(body), "projects/foo") || a.requests.Load() != 1 {
				t.Fatal("result escaped after host policy changed")
			}
		})
	}
}
