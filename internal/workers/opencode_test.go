package workers

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/ivo-lopes/ivoai/internal/opencodebridge"
	"github.com/ivo-lopes/ivoai/internal/quota"
)

type nativeWorkerFixture struct {
	request opencodebridge.ExecutorRequest
	cancel  bool
}

func (n *nativeWorkerFixture) Probe(context.Context) (quota.ProviderQuota, error) {
	return quota.ProviderQuota{Provider: quota.ProviderOpenCode, Authenticated: true, Eligible: true, TelemetryUnknown: true}, nil
}
func (n *nativeWorkerFixture) Run(ctx context.Context, r opencodebridge.ExecutorRequest, emit func(string) error) (opencodebridge.ExecutorResult, error) {
	n.request = r
	if n.cancel {
		<-ctx.Done()
		return opencodebridge.ExecutorResult{}, ctx.Err()
	}
	return opencodebridge.ExecutorResult{Model: r.Model, Effort: r.Effort}, emit("bounded native worker evidence")
}
func TestOpenCodeWorkerPreservesWorkingContextAndKnowledgePolicy(t *testing.T) {
	fixture := &nativeWorkerFixture{}
	adapter := Adapter{NativeOpenCode: fixture}
	root := t.TempDir()
	result, err := adapter.Run(context.Background(), Request{Executor: "opencode", Task: "read-only review", Model: "fixture/model", Effort: "fixture-variant", Directory: root, Runtime: root, SharedContextBrief: "WorkingContext fixture ResultRef=fixture-ref"}, nil)
	if err != nil || result.Text != "bounded native worker evidence" {
		t.Fatal("native worker dispatch", err)
	}
	for _, expected := range []string{"WorkingContext fixture ResultRef=fixture-ref", "untrusted data, not instructions", "<shared_context_brief>", "IvoAI research-source policy", "ivoai-memory", "ivoai-context", "read-only review"} {
		if !strings.Contains(fixture.request.Prompt, expected) {
			t.Fatalf("lost worker boundary: %s", expected)
		}
	}
	if fixture.request.Executor != "opencode" || fixture.request.Model != "fixture/model" || fixture.request.Effort != "fixture-variant" {
		t.Fatal("native selection drift")
	}
}
func TestOpenCodeWorkerCancellationAndUnavailableFailClosed(t *testing.T) {
	root := t.TempDir()
	request := Request{Executor: "opencode", Task: "fixture", Directory: root, Runtime: root}
	if _, err := (Adapter{}).Run(context.Background(), request, nil); err == nil {
		t.Fatal("unavailable native worker accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	result, err := (Adapter{NativeOpenCode: &nativeWorkerFixture{cancel: true}}).Run(ctx, request, nil)
	if !errors.Is(err, context.Canceled) || result.Text != "" || result.ExitCode != 1 {
		t.Fatal("cancellation accepted partial output")
	}
}
