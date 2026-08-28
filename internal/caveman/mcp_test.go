package caveman

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ivo-lopes/ivoai/internal/workingcontext"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestMCPCompressorUsesManagedLocalStdio(t *testing.T) {
	if os.Getenv("IVOAI_CAVEMAN_MCP_HELPER") == "1" {
		server := mcp.NewServer(&mcp.Implementation{Name: "caveman", Version: "dev"}, nil)
		server.AddTool(&mcp.Tool{Name: "caveman_compress", InputSchema: map[string]any{"type": "object"}}, func(_ context.Context, _ *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			payload, _ := json.Marshal(mcpCompressPayload{Compressed: "compact fixture", Ratio: .25, TokensBefore: 40, TokensAfter: 10, Basis: "inferred"})
			return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(payload)}}}, nil
		})
		if err := server.Run(context.Background(), &mcp.StdioTransport{}); err != nil {
			os.Exit(2)
		}
		os.Exit(0)
	}
	root := t.TempDir()
	binary := filepath.Join(root, "caveman-mcp")
	script := fmt.Sprintf("#!/bin/sh\nIVOAI_CAVEMAN_MCP_HELPER=1 exec %q -test.run=^TestMCPCompressorUsesManagedLocalStdio$\n", os.Args[0])
	if err := os.WriteFile(binary, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	compressor := MCPCompressor{Binary: binary, RuntimeDir: root, Managed: true, Timeout: 3 * time.Second, IntegrityCheck: func() error { return nil }}
	result, err := compressor.Compact(context.Background(), workingcontext.CompactRequest{Input: []byte(strings.Repeat("row ", 100)), PayloadType: "log", Budget: 1024})
	if err != nil {
		t.Fatal(err)
	}
	if result.Representation != "compact fixture" || result.TokensBefore != 40 || result.TokensAfter != 10 || result.Basis != "inferred" {
		t.Fatalf("result=%+v", result)
	}
	if _, err := os.Stat(filepath.Join(root, "caveman", "mcp", "ccr.db")); !os.IsNotExist(err) {
		t.Fatal("ephemeral MCP wrote a durable recovery database")
	}
}

func TestMCPCompressionMetadataFailsClosed(t *testing.T) {
	if safeBasis("provider_reported") || normalizedCavemanType("unknown") != "text" {
		t.Fatal("unverified metadata was accepted")
	}
	if _, err := textResult(&mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: "one"}, &mcp.TextContent{Text: "two"}}}); err == nil {
		t.Fatal("multiple response bodies were accepted")
	}
}
