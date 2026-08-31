package cavemaneval

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/ivo-lopes/ivoai/internal/workingcontext"
)

type semanticCompressor struct {
	provider string
	fail     bool
}

func (c semanticCompressor) Compact(_ context.Context, request workingcontext.CompactRequest) (workingcontext.CompactResult, error) {
	if c.fail {
		return workingcontext.CompactResult{Provider: c.provider}, errors.New("controlled canary failure")
	}
	facts := []string{}
	for _, line := range strings.Split(string(request.Input), "\n") {
		for _, marker := range []string{"FACT=", `"fact":"`, "fact: ", "panic: ", "TEST FAILED:", "main.go:9:"} {
			if strings.Contains(line, marker) {
				facts = append(facts, strings.TrimSpace(line))
				break
			}
		}
	}
	representation := strings.Join(facts, "\n")
	if representation == "" {
		representation = "bounded representation"
	}
	return workingcontext.CompactResult{Representation: representation, Provider: c.provider, TokensBefore: len(request.Input) / 4, TokensAfter: len(representation) / 4, Basis: "estimated"}, nil
}

func TestDeterministicCanaryCorpusPreservesEvidenceAndFacts(t *testing.T) {
	for _, provider := range []string{"direct", "headroom", "caveman"} {
		t.Run(provider, func(t *testing.T) {
			var compressor workingcontext.RepresentationCompressor
			if provider != "direct" {
				compressor = semanticCompressor{provider: provider}
			}
			metrics, err := Run(context.Background(), Options{Root: t.TempDir(), Provider: provider, Compressor: compressor, ContextBudget: 8 << 10, Cycles: 2})
			if err != nil || !metrics.Passed() || metrics.Scenarios != len(Corpus())*2 || metrics.EstimatedTokensBefore == 0 || metrics.TokenBasis != "heuristic_4_bytes" {
				t.Fatalf("metrics=%+v err=%v", metrics, err)
			}
			if provider != "direct" && metrics.BytesInContext >= metrics.BytesBefore {
				t.Fatalf("fixture compression did not reduce bounded context: %+v", metrics)
			}
			encoded, _ := json.Marshal(metrics)
			t.Logf("HERMETIC_%s_METRICS=%s", strings.ToUpper(provider), encoded)
		})
	}
}

func TestCanaryDetectsSilentSemanticCorruption(t *testing.T) {
	compressor := semanticCompressor{provider: "caveman"}
	metrics, err := Run(context.Background(), Options{Root: t.TempDir(), Provider: "caveman", Compressor: corruptingCompressor{compressor}, ContextBudget: 8 << 10})
	if err != nil || metrics.SemanticMismatchCount == 0 || metrics.ArtifactMismatchCount != 0 {
		t.Fatalf("corruption was not distinguished from exact evidence: metrics=%+v err=%v", metrics, err)
	}
}

type corruptingCompressor struct{ semanticCompressor }

func (c corruptingCompressor) Compact(context.Context, workingcontext.CompactRequest) (workingcontext.CompactResult, error) {
	return workingcontext.CompactResult{Representation: "silently corrupted", Provider: c.provider, Basis: "estimated"}, nil
}

func TestCanaryCompressionFailureUsesBoundedDirectProjection(t *testing.T) {
	metrics, err := Run(context.Background(), Options{Root: t.TempDir(), Provider: "caveman", Compressor: semanticCompressor{provider: "caveman", fail: true}, ContextBudget: 4096})
	if err != nil || metrics.ArtifactMismatchCount != 0 || metrics.ExactRequiredMismatchCount != 0 || metrics.DegradedCount == 0 {
		t.Fatalf("degraded fallback metrics=%+v err=%v", metrics, err)
	}
}

func TestCanaryLongSessionIsBoundedAndCancelable(t *testing.T) {
	metrics, err := Run(context.Background(), Options{Root: t.TempDir(), Provider: "caveman", Compressor: semanticCompressor{provider: "caveman"}, ContextBudget: 4096, Cycles: 6})
	if err != nil || !metrics.Passed() || metrics.Scenarios != len(Corpus())*6 {
		t.Fatalf("long-session metrics=%+v err=%v", metrics, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Run(ctx, Options{Root: t.TempDir(), Provider: "direct"}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation=%v", err)
	}
}
