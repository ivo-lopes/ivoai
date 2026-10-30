package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ivo-lopes/ivoai/internal/codeintel"
	"github.com/ivo-lopes/ivoai/internal/config"
	"github.com/ivo-lopes/ivoai/internal/core"
	"github.com/ivo-lopes/ivoai/internal/workingcontext"
)

func TestDirectProviderFactoryWithoutOptionalRuntime(t *testing.T) {
	root := prefetchLiveRepository(t)
	a, store, id, task := prefetchLiveHost(t, root, "Inspect API mathpkg/scale.go")
	provider, err := a.CodeIntelligenceProvider(context.Background(), root, "cs_prefetch_fixture")
	if err != nil || provider == nil {
		t.Fatal("authorized builtin Direct unavailable without optional Graft/Node")
	}
	body, err := a.preparePrimaryCodeEvidence(context.Background(), store, id, task, 4094, a.CodeIntelligenceProvider)
	if err != nil {
		t.Fatal("public factory failed admitted Direct evidence")
	}
	owner := workingcontext.Ownership{SessionID: id, TaskID: task.ID}
	value := assertExactCodeEnvelope(t, a, body, owner)
	if !strings.Contains(value.Text, "func Scale(x int) int { return x * 2 }") {
		t.Fatal("public factory lost exact source bytes")
	}
	assertCodeOwnerIsolation(t, a, workingcontext.Ownership{SessionID: id, TaskID: "other"}, value.Artifact.ID)
	if provider.Root != root || provider.Scope != "cs_prefetch_fixture" || provider.Permissions != "filesystem.read" || provider.CacheRoot != filepath.Join(a.Store.Paths.CacheDir, "code-intelligence") || !provider.Allowed {
		t.Fatal("builtin Direct changed host scope or permissions")
	}
}

func TestDirectProviderFactoryGatewayExactWithoutOptionalRuntime(t *testing.T) {
	t.Setenv("IVOAI_EXPERIMENTAL_EVIDENCE_GATEWAY", "1")
	TestDirectProviderFactoryWithoutOptionalRuntime(t)
}

func TestDirectFactoryDoesNotTreatIntegrityFailuresAsAbsence(t *testing.T) {
	for _, kind := range []string{"claimed_install_missing", "partial_state", "malformed_state", "active_without_state", "unsafe_supply_root"} {
		t.Run(kind, func(t *testing.T) {
			root := prefetchLiveRepository(t)
			a, _, _, _ := prefetchLiveHost(t, root, "Inspect API mathpkg/scale.go")
			state := config.State{Schema: config.StateSchemaVersion, Components: map[string]config.ComponentState{}}
			switch kind {
			case "claimed_install_missing":
				state.Components["graft"] = config.ComponentState{Installed: true, Managed: true, Version: codeintel.Version, Path: "/unavailable/runtime"}
			case "partial_state":
				state.Components["graft"] = config.ComponentState{Version: codeintel.Version}
			case "active_without_state":
				var err error
				a, root, _, _, _ = verifiedCodeRuntimeFixture(t)
				state, err = a.Store.LoadState()
				if err != nil {
					t.Fatal(err)
				}
				delete(state.Components, "graft")
			case "unsafe_supply_root":
				if err := os.Symlink(t.TempDir(), filepath.Join(a.Store.Paths.DataDir, "supply-chain")); err != nil {
					t.Fatal(err)
				}
			}
			if err := a.Store.SaveState(state); err != nil {
				t.Fatal(err)
			}
			if kind == "malformed_state" {
				if err := os.WriteFile(a.Store.Paths.State, []byte("[malformed"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := a.CodeIntelligenceProvider(context.Background(), root, "cs_fixture"); err == nil {
				t.Fatal("untrusted or inconsistent runtime silently became optional absence")
			}
		})
	}
}

func TestVerifiedGraftWithoutNodeAllowsOnlyDirect(t *testing.T) {
	for _, kind := range []string{"missing", "symlink", "ancestor_symlink", "non_executable"} {
		t.Run(kind, func(t *testing.T) {
			a, root, spec, _, node := verifiedCodeRuntimeFixture(t)
			if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package fixture\nfunc ExactName() int { return 42 }\n"), 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.Remove(node); err != nil {
				t.Fatal(err)
			}
			if kind == "symlink" {
				if err := os.Symlink("/unavailable/node", node); err != nil {
					t.Fatal(err)
				}
			}
			if kind == "ancestor_symlink" {
				if err := os.Remove(filepath.Dir(node)); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink("/unavailable/bin", filepath.Dir(node)); err != nil {
					t.Fatal(err)
				}
			}
			if kind == "non_executable" {
				if err := os.WriteFile(node, []byte("fixture"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			p, err := a.codeIntelligenceProvider(context.Background(), root, "cs_fixture", spec)
			if kind != "missing" {
				if err == nil {
					t.Fatal("unsafe Node became optional absence")
				}
				return
			}
			if err != nil || p.ExecutionMode() != "direct_only" {
				t.Fatal("proven absent optional Node blocked Direct")
			}
			r, err := p.Inspect(context.Background(), core.CodeQuery{Operation: "ask", Query: "ExactName"})
			if err != nil || r.ProviderVersion != "direct" || !strings.Contains(r.Text, "return 42") {
				t.Fatal("missing Node lost exact builtin inspection")
			}
			if _, err := p.Query(context.Background(), core.CodeQuery{Operation: "map"}); err != codeintel.ErrStructuralUnavailable {
				t.Fatal("missing Node allowed structural retrieval")
			}
		})
	}
}
