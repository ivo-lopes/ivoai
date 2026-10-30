package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ivo-lopes/ivoai/internal/codeintel"
	"github.com/ivo-lopes/ivoai/internal/core"
	"github.com/ivo-lopes/ivoai/internal/evidence"
	"github.com/ivo-lopes/ivoai/internal/workingcontext"
)

func TestGatewayExecutionModeSeparatesForgedManagedCache(t *testing.T) {
	t.Setenv("IVOAI_EXPERIMENTAL_EVIDENCE_RECOVERY", "0")
	for _, operation := range []string{"skeleton", "callers"} {
		t.Run(operation, func(t *testing.T) {
			ctx := context.Background()
			root := prefetchLiveRepository(t)
			a, sessions, id, task := prefetchLiveHost(t, root, "Inspect API mathpkg/scale.go")
			// Deliberately identical empty paths in both modes: only the explicit
			// execution-mode partition can prevent this forged/degraded full-mode
			// cache fixture from satisfying the restricted provider.
			managed := &codeintel.Provider{Root: root, Scope: "cs_prefetch_fixture", Permissions: "filesystem.read", CacheRoot: filepath.Join(a.Store.Paths.CacheDir, "code-intelligence"), Allowed: true}
			limited := managed.WithoutStructuralRuntime()
			query, err := codeintel.Normalize(core.CodeQuery{Operation: operation, File: "mathpkg/scale.go", Symbol: "Scale", BudgetBytes: 3072})
			if err != nil {
				t.Fatal(err)
			}
			files, err := codeintel.Inventory(ctx, root)
			if err != nil {
				t.Fatal(err)
			}
			snapshot, err := codeintel.Snapshot(root, files)
			if err != nil {
				t.Fatal(err)
			}
			identity, err := codeintel.Identity(root, managed.Scope, managed.Permissions)
			if err != nil {
				t.Fatal(err)
			}
			cfg, err := a.Store.Load()
			if err != nil {
				t.Fatal(err)
			}
			value, err := sessions.Get(id)
			if err != nil {
				t.Fatal(err)
			}
			artifacts, err := workingcontext.NewLocalStore(filepath.Join(a.Store.Paths.CacheDir, "working-context"), workingcontext.LocalOptions{})
			if err != nil {
				t.Fatal(err)
			}
			a.evidenceGateway, err = evidence.New(artifacts)
			if err != nil {
				t.Fatal(err)
			}
			canonical, err := filepath.EvalSymlinks(root)
			if err != nil {
				t.Fatal(err)
			}
			request := evidence.Request{Owner: workingcontext.Ownership{SessionID: id, TaskID: task.ID}, Source: "code", ReadOnly: true, BudgetBytes: query.BudgetBytes,
				Scope:       evidence.Scope{Organization: evidence.Digest(managed.Scope), Repository: evidence.Digest(canonical), Worktree: identity, Permissions: evidence.Digest(managed.Permissions, value.Tasks[0].AllowedMCPTools, value.Tasks[0].Skills, cfg.Skills)},
				QueryDigest: evidence.Digest(query, codeintel.Version, managed.Distribution, managed.Node, map[string]string{"execution_mode": "managed"}),
				Authorize:   func(context.Context) error { return nil }, CurrentGeneration: func(context.Context) (string, error) { return snapshot.Digest, nil }}
			const forged = "FORGED_MANAGED_CACHE_MUST_NOT_BE_DIRECT_EVIDENCE"
			if _, err = a.evidenceGateway.Retrieve(ctx, request, func(context.Context) (evidence.Document, error) {
				return evidence.Document{Raw: []byte(forged), MediaType: "text/plain; charset=utf-8", Basis: evidence.Extracted, SourceComplete: true, Sensitivity: workingcontext.SensitivityRestricted}, nil
			}); err != nil {
				t.Fatal(err)
			}
			primed, err := a.gatewayCodeEvidence(ctx, managed, id, task.ID, "", query)
			if err != nil || !strings.Contains(string(primed), forged) {
				t.Fatal("managed cache positive control was not primed")
			}
			inspections := 0
			limited.Observe = func(core.CodeResult, error) { inspections++ }
			body, err := a.gatewayCodeEvidence(ctx, limited, id, task.ID, "", query)
			if strings.Contains(string(body), forged) {
				t.Fatal("Direct-only reused a full-mode cache entry")
			}
			if operation == "callers" {
				if !errors.Is(err, codeintel.ErrStructuralUnavailable) || len(body) != 0 {
					t.Fatal("restricted mode recovered structural evidence")
				}
				if _, err := os.Stat(limited.CacheRoot); !os.IsNotExist(err) {
					t.Fatal("structural refusal mutated CodeIntel cache")
				}
			} else {
				if err != nil || inspections != 1 {
					t.Fatal("restricted mode skipped its own direct source inspection")
				}
				exact := assertExactCodeEnvelope(t, a, body, workingcontext.Ownership{SessionID: id, TaskID: task.ID})
				if !strings.Contains(exact.Text, "func Scale(x int) int { return x * 2 }") {
					t.Fatal("Direct recovery lost exact source")
				}
			}
		})
	}
}
