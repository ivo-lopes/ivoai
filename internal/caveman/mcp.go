package caveman

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"time"

	"github.com/ivo-lopes/ivoai/internal/platform"
	"github.com/ivo-lopes/ivoai/internal/supplychain"
	"github.com/ivo-lopes/ivoai/internal/workingcontext"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	defaultMCPTimeout = 15 * time.Second
	maxMCPInput       = 16 << 20
	maxMCPOutput      = 2 << 20
)

// MCPCompressor invokes only the managed, local-only Caveman stdio tool. It is
// an internal representation service and is never registered with the primary.
type MCPCompressor struct {
	Binary     string
	RuntimeDir string
	SupplyRoot string
	Expected   supplychain.ResolvedSource
	Managed    bool
	Timeout    time.Duration
	// IntegrityCheck exists for hermetic tests. Production callers leave it nil
	// and must pass the canonical supply-chain validation below.
	IntegrityCheck func() error
}

type mcpCompressPayload struct {
	Compressed     string  `json:"compressed"`
	Ratio          float64 `json:"ratio"`
	TokensBefore   int     `json:"tokens_before"`
	TokensAfter    int     `json:"tokens_after"`
	Basis          string  `json:"basis"`
	RecoveryHandle *string `json:"recovery_handle"`
}

func (c MCPCompressor) Compact(ctx context.Context, request workingcontext.CompactRequest) (workingcontext.CompactResult, error) {
	if len(request.Input) > maxMCPInput {
		return workingcontext.CompactResult{}, errors.New("Caveman MCP input exceeds the artifact bound")
	}
	if err := c.validate(); err != nil {
		return workingcontext.CompactResult{}, err
	}
	if !filepath.IsAbs(c.RuntimeDir) {
		return workingcontext.CompactResult{}, errors.New("Caveman MCP requires an absolute session runtime")
	}
	runtimeRoot := filepath.Join(c.RuntimeDir, "caveman", "mcp")
	if err := platform.EnsurePrivateDir(runtimeRoot); err != nil {
		return workingcontext.CompactResult{}, err
	}
	timeout := c.Timeout
	if timeout <= 0 {
		timeout = defaultMCPTimeout
	}
	callCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	command := exec.CommandContext(callCtx, c.Binary)
	command.Env = []string{
		"HOME=" + runtimeRoot,
		"CAVEMAN_HOME=" + runtimeRoot,
		"CAVEMAN_MCP_EPHEMERAL=1",
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "ivoai-working-context", Version: "1"}, nil)
	session, err := client.Connect(callCtx, &mcp.CommandTransport{Command: command, TerminateDuration: time.Second}, nil)
	if err != nil {
		return workingcontext.CompactResult{}, fmt.Errorf("start Caveman MCP: %w", err)
	}
	defer session.Close()
	result, err := session.CallTool(callCtx, &mcp.CallToolParams{Name: "caveman_compress", Arguments: map[string]any{"input": string(request.Input), "content_type": normalizedCavemanType(request.PayloadType)}})
	if err != nil {
		return workingcontext.CompactResult{}, fmt.Errorf("call Caveman MCP: %w", err)
	}
	if result.IsError {
		return workingcontext.CompactResult{}, errors.New("Caveman MCP rejected compression")
	}
	body, err := textResult(result)
	if err != nil {
		return workingcontext.CompactResult{}, err
	}
	if len(body) > maxMCPOutput {
		return workingcontext.CompactResult{}, errors.New("Caveman MCP response exceeds the safe bound")
	}
	var payload mcpCompressPayload
	if err := json.Unmarshal([]byte(body), &payload); err != nil {
		return workingcontext.CompactResult{}, errors.New("Caveman MCP returned malformed compression metadata")
	}
	if len(payload.Compressed) > maxMCPOutput || payload.TokensBefore < 0 || payload.TokensAfter < 0 || !safeBasis(payload.Basis) {
		return workingcontext.CompactResult{}, errors.New("Caveman MCP returned invalid bounded metadata")
	}
	handle := ""
	if payload.RecoveryHandle != nil && strings.HasPrefix(*payload.RecoveryHandle, "ccr_") && len(*payload.RecoveryHandle) <= 128 {
		handle = *payload.RecoveryHandle
	}
	return workingcontext.CompactResult{Representation: payload.Compressed, TokensBefore: payload.TokensBefore, TokensAfter: payload.TokensAfter, Basis: payload.Basis, RecoveryHandle: handle}, nil
}

func (c MCPCompressor) validate() error {
	info, err := os.Lstat(c.Binary)
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || info.Mode().Perm() != 0o700 || !c.Managed {
		return errors.New("Caveman MCP managed binary is missing or unsafe")
	}
	if c.IntegrityCheck != nil {
		return c.IntegrityCheck()
	}
	if c.SupplyRoot == "" || c.Expected.ID != "caveman-mcp" {
		return errors.New("Caveman MCP provenance is unavailable")
	}
	active, root, err := (supplychain.Manager{Root: c.SupplyRoot}).Active("caveman-mcp")
	if err != nil || !reflect.DeepEqual(active, c.Expected) {
		return errors.New("Caveman MCP active provenance does not match the pinned source")
	}
	want := filepath.Join(root, filepath.FromSlash(active.PayloadPath))
	if filepath.Clean(want) != filepath.Clean(c.Binary) {
		return errors.New("Caveman MCP state path diverges from the active immutable object")
	}
	return nil
}

func textResult(result *mcp.CallToolResult) (string, error) {
	if len(result.Content) != 1 {
		return "", errors.New("Caveman MCP returned an unexpected result shape")
	}
	text, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		return "", errors.New("Caveman MCP returned non-text output")
	}
	return text.Text, nil
}

func normalizedCavemanType(value string) string {
	switch value {
	case "json", "log", "code", "diff", "search_result", "text":
		return value
	default:
		return "text"
	}
}

func safeBasis(value string) bool {
	return value == "" || value == "inferred" || value == "estimated" || value == "heuristic"
}

var _ workingcontext.RepresentationCompressor = MCPCompressor{}
