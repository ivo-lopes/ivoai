// Package cavemaneval provides a deterministic, provider-neutral canary corpus
// for the compression and WorkingContext boundary. It never invokes a provider,
// network service, or credential store on its own.
package cavemaneval

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"time"

	"github.com/ivo-lopes/ivoai/internal/core"
	"github.com/ivo-lopes/ivoai/internal/observability"
	"github.com/ivo-lopes/ivoai/internal/workingcontext"
)

type Scenario struct {
	Name          string
	PayloadType   string
	MediaType     string
	Raw           []byte
	Status        workingcontext.ResultStatus
	Fidelity      core.CompressionFidelity
	RequiredFacts []string
}

type Options struct {
	Root          string
	Provider      string
	Compressor    workingcontext.RepresentationCompressor
	ContextBudget int
	Cycles        int
}

type Metrics struct {
	Provider                   string        `json:"provider"`
	TokenBasis                 string        `json:"token_basis"`
	Scenarios                  int           `json:"scenarios"`
	BytesBefore                int64         `json:"bytes_before"`
	BytesInContext             int64         `json:"bytes_in_context"`
	EstimatedTokensBefore      int64         `json:"estimated_tokens_before"`
	EstimatedTokensInContext   int64         `json:"estimated_tokens_in_context"`
	ContextRatio               float64       `json:"context_ratio"`
	Latency                    time.Duration `json:"latency"`
	BypassCount                int           `json:"bypass_count"`
	DegradedCount              int           `json:"degraded_count"`
	ArtifactMismatchCount      int           `json:"artifact_mismatch_count"`
	ExactRequiredMismatchCount int           `json:"exact_required_mismatch_count"`
	SemanticMismatchCount      int           `json:"semantic_mismatch_count"`
	ObservabilityLeakCount     int           `json:"observability_leak_count"`
}

func (m Metrics) Passed() bool {
	return m.ArtifactMismatchCount == 0 && m.ExactRequiredMismatchCount == 0 && m.SemanticMismatchCount == 0 && m.ObservabilityLeakCount == 0
}

func Run(ctx context.Context, options Options) (Metrics, error) {
	if !filepath.IsAbs(options.Root) {
		return Metrics{}, errors.New("canary root must be absolute")
	}
	if options.Provider != "direct" && options.Provider != "headroom" && options.Provider != "caveman" {
		return Metrics{}, errors.New("unsupported canary provider")
	}
	cycles := options.Cycles
	if cycles <= 0 {
		cycles = 1
	}
	if cycles > 32 {
		return Metrics{}, errors.New("canary cycles exceed the bounded limit")
	}
	store, err := workingcontext.NewLocalStore(filepath.Join(options.Root, "artifacts"), workingcontext.LocalOptions{})
	if err != nil {
		return Metrics{}, err
	}
	metrics := Metrics{Provider: options.Provider, TokenBasis: "heuristic_4_bytes"}
	started := time.Now()
	corpus := Corpus()
	for cycle := 0; cycle < cycles; cycle++ {
		for index, scenario := range corpus {
			if err := ctx.Err(); err != nil {
				return metrics, err
			}
			owner := ownerFor(cycle, index)
			events := []observability.Event{}
			result := (workingcontext.Projector{Store: store, Compressor: options.Compressor, Observe: func(event observability.Event) {
				events = append(events, event)
			}}).Project(ctx, workingcontext.ProjectionInput{
				Owner: owner, Raw: scenario.Raw, MediaType: scenario.MediaType, Status: scenario.Status,
				ContextBudget: options.ContextBudget, PayloadType: scenario.PayloadType, Fidelity: scenario.Fidelity,
				AssociationID: fmt.Sprintf("call_%d_%d", cycle, index),
			})
			metrics.Scenarios++
			metrics.BytesBefore += int64(len(scenario.Raw))
			metrics.BytesInContext += int64(len(result.Summary))
			if result.Degraded {
				metrics.DegradedCount++
			}
			if result.Fidelity != core.CompressionCompressible {
				metrics.BypassCount++
			}
			if len(result.Evidence) != 1 {
				metrics.ArtifactMismatchCount++
				if result.Fidelity == core.CompressionExactRequired {
					metrics.ExactRequiredMismatchCount++
				}
				continue
			}
			reader, _, readErr := store.Read(ctx, owner, result.Evidence[0].Artifact.ID)
			var recovered []byte
			if readErr == nil {
				recovered, readErr = io.ReadAll(reader)
				_ = reader.Close()
			}
			if readErr != nil || sha256.Sum256(recovered) != sha256.Sum256(scenario.Raw) {
				metrics.ArtifactMismatchCount++
				if result.Fidelity == core.CompressionExactRequired {
					metrics.ExactRequiredMismatchCount++
				}
			}
			for _, fact := range scenario.RequiredFacts {
				if !strings.Contains(result.Summary, fact) {
					metrics.SemanticMismatchCount++
				}
			}
			encoded, _ := json.Marshal(events)
			for _, forbidden := range forbiddenObservabilityFragments(scenario.Raw) {
				if bytes.Contains(bytes.ToLower(encoded), bytes.ToLower(forbidden)) {
					metrics.ObservabilityLeakCount++
				}
			}
		}
	}
	metrics.Latency = time.Since(started)
	metrics.EstimatedTokensBefore = estimateTokens(metrics.BytesBefore)
	metrics.EstimatedTokensInContext = estimateTokens(metrics.BytesInContext)
	if metrics.BytesBefore > 0 {
		metrics.ContextRatio = float64(metrics.BytesInContext) / float64(metrics.BytesBefore)
	}
	_, _ = store.GC(context.Background())
	return metrics, nil
}

func Corpus() []Scenario {
	large := "FACT=large-output-preserved\n" + strings.Repeat("repeatable canary payload row\n", 25_000)
	repetitive := "FACT=repetition-preserved\n" + strings.Repeat("same same same same\n", 5_000)
	longText := "FACT=long-text-preserved\n" + strings.Repeat("A bounded deterministic paragraph for compression evaluation. ", 2_000)
	highEntropy := "FACT=entropy-preserved\n" + deterministicHex(8_192)
	return []Scenario{
		{Name: "json", PayloadType: "json", MediaType: "application/json", Raw: []byte(`{"status":"ok","count":3,"fact":"json-preserved"}`), RequiredFacts: []string{"json-preserved"}},
		{Name: "jsonl", PayloadType: "json", MediaType: "application/x-ndjson", Raw: []byte("{\"fact\":\"jsonl-preserved\",\"row\":1}\n{\"row\":2}\n"), RequiredFacts: []string{"jsonl-preserved"}},
		{Name: "yaml", PayloadType: "text", Raw: []byte("status: ok\nfact: yaml-preserved\nitems:\n  - one\n"), RequiredFacts: []string{"yaml-preserved"}},
		{Name: "logs", PayloadType: "log", Raw: []byte("INFO ready\nWARN retry\nFACT=log-preserved\n"), RequiredFacts: []string{"log-preserved"}},
		{Name: "stack-trace", PayloadType: "stack_trace", Raw: []byte("panic: stack-preserved\nmain.go:42\n"), RequiredFacts: []string{"stack-preserved"}},
		{Name: "go", PayloadType: "code", Raw: []byte("package main\n// FACT=go-preserved\nfunc main() {}\n"), RequiredFacts: []string{"go-preserved"}},
		{Name: "python", PayloadType: "code", Raw: []byte("# FACT=python-preserved\nprint('ok')\n"), RequiredFacts: []string{"python-preserved"}},
		{Name: "shell", PayloadType: "code", Raw: []byte("#!/bin/sh\n# FACT=shell-preserved\nprintf ok\n"), RequiredFacts: []string{"shell-preserved"}},
		{Name: "typescript", PayloadType: "code", Raw: []byte("// FACT=typescript-preserved\nexport const ok = true;\n"), RequiredFacts: []string{"typescript-preserved"}},
		{Name: "diff", PayloadType: "diff", Raw: []byte("diff --git a/a b/a\n+FACT=diff-preserved\n"), RequiredFacts: []string{"diff-preserved"}},
		{Name: "git-output", PayloadType: "text", Raw: []byte("5308be1 FACT=git-preserved\n"), RequiredFacts: []string{"git-preserved"}},
		{Name: "search", PayloadType: "search_result", Raw: []byte("path/file.go:42 FACT=search-preserved\n"), RequiredFacts: []string{"search-preserved"}},
		{Name: "mcp-tool", PayloadType: "context_response", MediaType: "application/json", Raw: []byte(`{"result":{"fact":"mcp-preserved"}}`), RequiredFacts: []string{"mcp-preserved"}},
		{Name: "table", PayloadType: "text", Raw: []byte("name | state\nFACT=table-preserved\ncanary | ready\n"), RequiredFacts: []string{"table-preserved"}},
		{Name: "test-failure", PayloadType: "test_failure", Status: workingcontext.ResultFailed, Raw: []byte("TEST FAILED: test-failure-preserved\nexit status 1\n"), RequiredFacts: []string{"test-failure-preserved"}},
		{Name: "compiler-failure", PayloadType: "build_failure", Status: workingcontext.ResultFailed, Raw: []byte("main.go:9: compiler-failure-preserved\nexit status 2\n"), RequiredFacts: []string{"compiler-failure-preserved"}},
		{Name: "long-text", PayloadType: "text", Raw: []byte(longText), RequiredFacts: []string{"long-text-preserved"}},
		{Name: "large-output", PayloadType: "log", Raw: []byte(large), RequiredFacts: []string{"large-output-preserved"}},
		{Name: "repetitive", PayloadType: "text", Raw: []byte(repetitive), RequiredFacts: []string{"repetition-preserved"}},
		{Name: "high-entropy", PayloadType: "text", Raw: []byte(highEntropy), RequiredFacts: []string{"entropy-preserved"}},
		{Name: "mixed", PayloadType: "text", Raw: []byte("FACT=mixed-preserved\n{\"ok\":true}\nplain text\nTRACE line:7\n"), RequiredFacts: []string{"mixed-preserved"}},
		{Name: "empty-tool", PayloadType: "memory_response", MediaType: "application/json", Raw: []byte(`{"result":{"content":[]},"fact":"empty-preserved"}`), RequiredFacts: []string{"empty-preserved"}},
	}
}

func ownerFor(cycle, index int) workingcontext.Ownership {
	value := fmt.Sprintf("%032x", cycle*1000+index+1)
	return workingcontext.Ownership{SessionID: "sess_" + value, TaskID: fmt.Sprintf("scenario_%d", index)}
}

func estimateTokens(value int64) int64 {
	if value <= 0 {
		return 0
	}
	return (value + 3) / 4
}

func deterministicHex(size int) string {
	var builder strings.Builder
	for index := 0; builder.Len() < size; index++ {
		digest := sha256.Sum256([]byte(fmt.Sprintf("ivoai-caveman-canary-%d", index)))
		builder.WriteString(hex.EncodeToString(digest[:]))
	}
	return builder.String()[:size]
}

func forbiddenObservabilityFragments(raw []byte) [][]byte {
	digest := sha256.Sum256(raw)
	values := [][]byte{raw, digest[:]}
	if len(raw) > 64 {
		values = append(values, raw[:64])
	}
	return values
}
