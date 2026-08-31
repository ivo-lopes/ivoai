package cavemaneval_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/ivo-lopes/ivoai/internal/caveman"
	"github.com/ivo-lopes/ivoai/internal/cavemaneval"
)

const cavemanMCPAMD64SHA256 = "c5c9a850f388570e2b822ac86ac35ad0e9f2c8ec0162b966f5536013042c058d"

// TestPinnedCavemanMCPManualCanary is an opt-in maintenance test. Normal CI is
// hermetic and skips it; a reviewer may supply the already downloaded pinned
// asset without installing it globally or granting provider credentials.
func TestPinnedCavemanMCPManualCanary(t *testing.T) {
	binary := os.Getenv("IVOAI_CAVEMAN_MCP_BINARY")
	if binary == "" {
		t.Skip("IVOAI_CAVEMAN_MCP_BINARY is not set")
	}
	validate := func() error {
		body, err := os.ReadFile(binary)
		if err != nil {
			return err
		}
		digest := sha256.Sum256(body)
		if hex.EncodeToString(digest[:]) != cavemanMCPAMD64SHA256 {
			return errors.New("pinned Caveman MCP digest mismatch")
		}
		return nil
	}
	if err := validate(); err != nil {
		t.Fatal(err)
	}
	compressor := caveman.MCPCompressor{Binary: binary, RuntimeDir: t.TempDir(), Managed: true, Timeout: 20 * time.Second, IntegrityCheck: validate}
	metrics, err := cavemaneval.Run(context.Background(), cavemaneval.Options{Root: t.TempDir(), Provider: "caveman", Compressor: compressor, ContextBudget: 8 << 10})
	encoded, _ := json.Marshal(metrics)
	t.Logf("PINNED_CAVEMAN_MCP_METRICS=%s", encoded)
	if err != nil || !metrics.Passed() {
		t.Fatalf("pinned Caveman MCP canary failed: metrics=%+v err=%v", metrics, err)
	}
}
