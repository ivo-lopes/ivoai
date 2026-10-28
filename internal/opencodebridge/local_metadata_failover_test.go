package opencodebridge

import (
	"context"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/ivo-lopes/ivoai/internal/core"
)

type localMetadataQuotaRunner struct {
	admitted    chan struct{}
	calls       atomic.Int32
	rateLimited bool
}

func (r *localMetadataQuotaRunner) Run(ctx context.Context, request ExecutorRequest, emit func(string) error) (ExecutorResult, error) {
	r.calls.Add(1)
	if request.Executor == "codex" {
		close(r.admitted)
		result := ExecutorResult{ConfigurationSource: core.LocalMetadataSource}
		if r.rateLimited {
			return result, fixtureNativeLimit{}
		}
		<-ctx.Done()
		return result, ctx.Err()
	}
	return ExecutorResult{ExecutorSessionID: "unexpected_alternate"}, emit("unexpected alternate execution")
}

func TestLocalMetadataFailureNeverSelectsOrRunsAlternate(t *testing.T) {
	for _, cause := range []string{"quota monitor", "rate limited error"} {
		for _, stream := range []bool{false, true} {
			name := cause + "/json"
			if stream {
				name = cause + "/stream"
			}
			t.Run(name, func(t *testing.T) {
				runner := &localMetadataQuotaRunner{admitted: make(chan struct{}), rateLimited: cause == "rate limited error"}
				var alternates, handoffs atomic.Int32
				options := Options{Runner: runner, Select: func(context.Context, string) (string, error) { return "codex", nil }, SelectAlternate: func(context.Context, string, []string) (string, error) { alternates.Add(1); return "claude", nil }, FailoverHandoff: func(string, string, string) string { handoffs.Add(1); return "unexpected delegation" }, Status: func() Status { return Status{} }}
				if cause == "quota monitor" {
					options.Monitor = func(ctx context.Context, executor string) string {
						if executor != "codex" {
							<-ctx.Done()
							return ""
						}
						select {
						case <-runner.admitted:
							return "quota revoked after Core metadata admission"
						case <-ctx.Done():
							return ""
						}
					}
				}
				bridge, err := Start(options)
				if err != nil {
					t.Fatal(err)
				}
				defer bridge.Close(context.Background())
				response := bridgeRequest(t, bridge, stream)
				body, _ := io.ReadAll(response.Body)
				response.Body.Close()
				if runner.calls.Load() != 1 || alternates.Load() != 0 || handoffs.Load() != 0 || strings.Contains(string(body), "unexpected alternate") {
					t.Fatalf("Core local delegated after %s: calls=%d alternates=%d handoffs=%d status=%d body=%s", cause, runner.calls.Load(), alternates.Load(), handoffs.Load(), response.StatusCode, body)
				}
				if !stream && response.StatusCode == http.StatusOK || !strings.Contains(string(body), `"error"`) {
					t.Fatalf("admitted local failure became success: %d %s", response.StatusCode, body)
				}
			})
		}
	}
}
