package caveman_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ivo-lopes/ivoai/internal/caveman"
	"github.com/ivo-lopes/ivoai/internal/core"
	"github.com/ivo-lopes/ivoai/internal/platform"
	"github.com/ivo-lopes/ivoai/internal/supplychain"
)

const cavemanProxyAMD64SHA256 = "d883b9ab4b559e0c1935335c0e24400deb5c61d5e247f1ca239c4149f57885b0"

// TestPinnedCavemanProxyManualCanary is opt-in and never uses provider auth.
// It validates the reviewed binary, structured probe, loopback readiness and
// cleanup for every currently supported direct executor.
func TestPinnedCavemanProxyManualCanary(t *testing.T) {
	binary := os.Getenv("IVOAI_CAVEMAN_PROXY_BINARY")
	if binary == "" {
		t.Skip("IVOAI_CAVEMAN_PROXY_BINARY is not set")
	}
	validate := func() error {
		body, err := os.ReadFile(binary)
		if err != nil {
			return err
		}
		digest := sha256.Sum256(body)
		if hex.EncodeToString(digest[:]) != cavemanProxyAMD64SHA256 {
			return errors.New("pinned Caveman proxy digest mismatch")
		}
		return nil
	}
	provider := caveman.Provider{
		Binary: binary, Managed: true, Runner: platform.ExecRunner{}, StartupTimeout: 5 * time.Second,
		Expected: supplychain.ResolvedSource{ID: "caveman", LogicalVersion: "1.1.3"}, IntegrityCheck: validate,
	}
	status := provider.Probe(context.Background())
	if !status.Available || status.Health != core.HealthHealthy || status.Provenance.Version != "1.1.3" {
		t.Fatalf("structured proxy probe=%+v", status)
	}
	for _, executor := range []core.ComponentID{core.ComponentCodex, core.ComponentClaude, core.ComponentOpenCode} {
		t.Run(string(executor), func(t *testing.T) {
			runtimeRoot := filepath.Join(t.TempDir(), "session")
			if err := os.Mkdir(runtimeRoot, 0o700); err != nil {
				t.Fatal(err)
			}
			lease, err := provider.Prepare(context.Background(), core.CompressionRequest{Executor: executor, DirectPath: "/bin/true", RuntimeDir: runtimeRoot, Fidelity: core.CompressionCompressible, Environment: []string{"PATH=/usr/bin:/bin"}})
			if err != nil || !lease.Decision().Used || lease.Decision().Provider != "caveman" {
				t.Fatalf("executor=%s decision=%+v err=%v", executor, lease, err)
			}
			if err := lease.Close(context.Background()); err != nil {
				t.Fatal(err)
			}
			entries, err := os.ReadDir(filepath.Join(runtimeRoot, "caveman"))
			if err != nil || len(entries) != 0 {
				t.Fatalf("executor=%s runtime not cleaned: entries=%d err=%v", executor, len(entries), err)
			}
		})
	}
}
