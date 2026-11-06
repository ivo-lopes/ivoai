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

// Exact baseline and reviewed repaired-build pins; never accept an ambient proxy.
var cavemanProxyAMD64Pins = map[string]string{
	"44436d69705ad9968563c181cd11d396d6ae5eeccd8dbf228f6225d92dfadcb1": "2.1.0",
	"848649eb185ce350ac91f1805f83160d719374a2fabef7c04abccafe0480d859": "4.0.2-ivoai.1",
}

// TestPinnedCavemanProxyManualCanary is opt-in and never uses provider auth.
// It validates the reviewed binary, structured probe, loopback readiness and
// cleanup for every currently supported direct executor.
func TestPinnedCavemanProxyManualCanary(t *testing.T) {
	binary := os.Getenv("IVOAI_CAVEMAN_PROXY_BINARY")
	if binary == "" {
		t.Skip("IVOAI_CAVEMAN_PROXY_BINARY is not set")
	}
	version := ""
	validate := func() error {
		body, err := os.ReadFile(binary)
		if err != nil {
			return err
		}
		digest := sha256.Sum256(body)
		var ok bool
		version, ok = cavemanProxyAMD64Pins[hex.EncodeToString(digest[:])]
		if !ok {
			return errors.New("pinned Caveman proxy digest mismatch")
		}
		return nil
	}
	if err := validate(); err != nil {
		t.Fatal(err)
	}
	provider := caveman.Provider{
		Binary: binary, Managed: true, Runner: platform.ExecRunner{}, StartupTimeout: 5 * time.Second,
		Expected: supplychain.ResolvedSource{ID: "caveman", LogicalVersion: version}, IntegrityCheck: validate,
	}
	status := provider.Probe(context.Background())
	if !status.Available || status.Health != core.HealthHealthy || status.Provenance.Version != "bin-v"+version {
		t.Fatalf("structured proxy probe=%+v", status)
	}
	for _, executor := range []core.ComponentID{core.ComponentCodex, core.ComponentClaude} {
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
