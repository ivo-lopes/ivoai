package app

import (
	"context"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestAdaptiveCodeFactoryAndProjectionAvailable(t *testing.T) {
	f := newAdaptiveFixture(t)
	f.a.Store.Paths.DataDir = filepath.Join(f.a.Store.Paths.StateDir, "data")
	if os.WriteFile(filepath.Join(f.root, "auth.go"), []byte("package fixture\nfunc Authenticate(token string) bool { return token == \"fixture-allowed\" }\n"), 0600) != nil {
		t.Fatal("fixture unavailable")
	}
	cmd := exec.Command("/usr/bin/git", "init", "-q", f.root)
	if cmd.Run() != nil {
		t.Fatal("Git unavailable")
	}
	v, _ := f.store.Get(f.id)
	p, err := f.a.CodeIntelligenceProvider(context.Background(), f.root, v.KnowledgeScopeID)
	if err != nil {
		t.Fatalf("Code factory: %v", err)
	}
	if p.ExecutionMode() != "direct_only" {
		t.Fatal("Direct mode missing")
	}
	// No model, schema inspection or shell tools should be needed to admit the
	// host's supported builtin reader for this repository.
	k, err := f.a.prepareAutoPromptKnowledge(context.Background(), f.cfg, nil, "Consulte code_inspect para localizar Authenticate", "codex", t.TempDir(), f.root, nil)
	if err != nil {
		t.Fatalf("projection unavailable: %v", err)
	}
	defer k.close()
	if k.external == nil {
		t.Fatal("native code gateway not projected")
	}
	// The prepared local endpoint has its own provider. Assert actual successful
	// data instead of treating a listed schema as execution.
	k.external.SetToolAdmission(func(server, tool string) (bool, bool) {
		return server == nativeCodeMCP && tool == "code_inspect", false
	})
	k.external.SetAdmission(func() bool { return true })
	k.external.RequirePolicyJSON()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	client := mcp.NewClient(&mcp.Implementation{Name: "bounded-fixture", Version: "1"}, nil)
	s, err := client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: k.config.MCP.Servers[nativeCodeMCP].URL, HTTPClient: &http.Client{Transport: adaptiveCodeTransport{k.external.Token()}}, DisableStandaloneSSE: true}, nil)
	if err != nil {
		t.Fatal("local MCP connection failed", err)
	}
	defer s.Close()
	list, err := s.ListTools(ctx, nil)
	if err != nil || len(list.Tools) != 1 || list.Tools[0].Name != "code_inspect" {
		t.Fatal("bounded code schema unavailable", err)
	}
	type response struct {
		r *mcp.CallToolResult
		e error
	}
	done := make(chan response, 1)
	go func() {
		r, e := s.CallTool(ctx, &mcp.CallToolParams{Name: "code_inspect", Arguments: map[string]any{"operation": "ask", "query": "Authenticate"}})
		done <- response{r, e}
	}()
	for len(k.external.Pending()) == 0 && ctx.Err() == nil {
		time.Sleep(time.Millisecond)
	}
	if len(k.external.Pending()) != 1 {
		t.Fatal("read approval not exposed")
	}
	select {
	case <-done:
		t.Fatal("read executed without approval")
	default:
	}
	permission := k.external.Pending()[0]
	if permission.Description != "ivoai-code · code_inspect" || k.external.Reply(permission.ID, true) != nil {
		t.Fatal("read approval unavailable")
	}
	out := <-done
	if out.e != nil || out.r == nil || out.r.IsError {
		t.Fatal("admitted code call failed", out.e)
	}
	text := ""
	for _, block := range out.r.Content {
		if b, ok := block.(*mcp.TextContent); ok {
			text += b.Text
		}
	}
	if !strings.Contains(text, "Authenticate") {
		t.Fatal("exact source missing")
	}
}

type adaptiveCodeTransport struct{ token string }

func (a adaptiveCodeTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Set("Authorization", "Bearer "+a.token)
	return http.DefaultTransport.RoundTrip(r)
}
