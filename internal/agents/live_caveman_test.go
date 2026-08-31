package agents_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ivo-lopes/ivoai/internal/agents"
	"github.com/ivo-lopes/ivoai/internal/caveman"
	"github.com/ivo-lopes/ivoai/internal/platform"
	"github.com/ivo-lopes/ivoai/internal/supplychain"
)

const liveCavemanProxySHA256 = "d883b9ab4b559e0c1935335c0e24400deb5c61d5e247f1ca239c4149f57885b0"

// TestAuthenticatedExecutorsThroughPinnedCaveman is an explicit local canary.
// Normal tests skip it: it uses the official CLI's existing authentication and
// may consume subscription quota. It never reads an auth file or prints output.
func TestAuthenticatedExecutorsThroughPinnedCaveman(t *testing.T) {
	proxy := os.Getenv("IVOAI_CAVEMAN_PROXY_BINARY")
	if proxy == "" || os.Getenv("IVOAI_LIVE_CAVEMAN") != "1" {
		t.Skip("explicit live Caveman canary is disabled")
	}
	validate := func() error {
		body, err := os.ReadFile(proxy)
		if err != nil {
			return err
		}
		digest := sha256.Sum256(body)
		if hex.EncodeToString(digest[:]) != liveCavemanProxySHA256 {
			return errors.New("pinned Caveman proxy digest mismatch")
		}
		return nil
	}
	for _, test := range []struct {
		name string
		path string
		args []string
	}{
		{name: "codex", path: os.Getenv("IVOAI_LIVE_CODEX_PATH"), args: []string{"exec", "--skip-git-repo-check", "--sandbox", "read-only", "--color", "never", "-"}},
		{name: "claude", path: os.Getenv("IVOAI_LIVE_CLAUDE_PATH"), args: []string{"--print", "--output-format", "text", "--permission-mode", "plan"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			if test.path == "" {
				t.Skip("authenticated executor path is unavailable")
			}
			ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
			defer cancel()
			provider := caveman.Provider{Binary: proxy, Managed: true, Runner: platform.ExecRunner{}, StartupTimeout: 5 * time.Second, Expected: supplychain.ResolvedSource{ID: "caveman", LogicalVersion: "1.1.3"}, IntegrityCheck: validate}
			var stdout, stderr bytes.Buffer
			runtimeRoot := filepath.Join(t.TempDir(), "session")
			if err := os.Mkdir(runtimeRoot, 0o700); err != nil {
				t.Fatal(err)
			}
			runtime := agents.Runtime{Runner: platform.ExecRunner{}, AgentPath: test.path, Compression: provider, RuntimeDir: runtimeRoot, In: strings.NewReader("Reply with exactly IVOAI_CAVEMAN_CANARY_OK and nothing else.\n"), Out: &stdout, Err: &stderr}
			if err := runtime.Launch(ctx, test.name, test.args, true); err != nil {
				t.Fatalf("live %s canary failed: %v", test.name, err)
			}
			if !strings.Contains(stdout.String(), "IVOAI_CAVEMAN_CANARY_OK") {
				t.Fatalf("live %s canary did not return the bounded marker", test.name)
			}
			if strings.Contains(strings.ToLower(stdout.String()+stderr.String()), "authorization: bearer") {
				t.Fatal("live canary output exposed an authorization header")
			}
		})
	}
}
