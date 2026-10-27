package evidence

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/ivo-lopes/ivoai/internal/core"
)

type viewProviderFunc func(context.Context, string, []byte) ([]byte, error)

func (f viewProviderFunc) Transform(c context.Context, filter string, raw []byte) ([]byte, error) {
	return f(c, filter, raw)
}

func inventoryOptions() OutputViewOptions {
	return OutputViewOptions{Enabled: true, AllowDerived: true, Kind: "inventory", Filter: "git-status"}
}

func viewFixture(t *testing.T) (*gatewayFixture, Packet, []byte) {
	t.Helper()
	f := newGatewayFixture(t)
	f.request.Source = "tool_read"
	doc := exactDocument()
	doc.Raw = []byte(strings.Repeat(" M src/example.go\n", 20))
	return f, mustRetrieve(t, f, f.request, doc), bytes.Clone(doc.Raw)
}

func TestOutputViewPreservesExactArtifactAndDoesNotRefetch(t *testing.T) {
	f, packet, raw := viewFixture(t)
	puts := f.store.puts.Load()
	provider := viewProviderFunc(func(ctx context.Context, filter string, input []byte) ([]byte, error) {
		if !bytes.Equal(input, raw) || filter != "git-status" {
			t.Fatal("provider did not receive exact stored snapshot")
		}
		input[0] = 'X' // A provider cannot mutate the original host snapshot.
		return []byte("20 modified paths\n"), nil
	})
	view, err := f.gateway.ViewOutput(context.Background(), f.request, packet.Ref, provider, inventoryOptions())
	if err != nil || !view.Derived || view.Representation != "derived_non_authoritative" || !view.Untrusted || view.Raw != packet.Ref || view.Raw.Fidelity != core.CompressionExactRequired || view.Text != "20 modified paths\n" {
		t.Fatalf("unexpected view: %+v %v", view, err)
	}
	exact, err := f.gateway.Recover(context.Background(), f.request, view.Raw)
	if err != nil || !bytes.Equal(exact, raw) || f.store.puts.Load() != puts {
		t.Fatal("view changed or replaced stored artifact")
	}
}

func TestOutputViewBypassPoliciesNeverInvokeProvider(t *testing.T) {
	for _, kind := range []string{"disabled", "no_consent", "compression_stack", "diff", "error", "stack_trace", "test_report", "unknown_filter", "inferred", "incomplete", "code", "wrong_media"} {
		t.Run(kind, func(t *testing.T) {
			f := newGatewayFixture(t)
			f.request.Source = "tool_read"
			doc := exactDocument()
			opts := inventoryOptions()
			switch kind {
			case "disabled":
				opts.Enabled = false
			case "no_consent":
				opts.AllowDerived = false
			case "compression_stack":
				opts.CompressionApplied = true
			case "unknown_filter":
				opts.Filter = "git-diff"
			case "inferred":
				doc.Basis = Inferred
			case "incomplete":
				doc.SourceComplete = false
			case "code":
				f.request.Source = "code"
			case "wrong_media":
				doc.MediaType = "text/x-diff"
			default:
				opts.Kind = kind
			}
			packet := mustRetrieve(t, f, f.request, doc)
			provider := viewProviderFunc(func(context.Context, string, []byte) ([]byte, error) {
				t.Fatal("policy bypass invoked provider")
				return nil, nil
			})
			view, err := f.gateway.ViewOutput(context.Background(), f.request, packet.Ref, provider, opts)
			if err != nil || view.Derived || view.Text != string(doc.Raw) {
				t.Fatalf("bypass lost exact evidence: %+v %v", view, err)
			}
		})
	}
}

func TestOutputViewUnavailableRejectedAndBudgetFallbacks(t *testing.T) {
	for _, scenario := range []string{"unavailable", "error", "same_size", "larger", "empty", "over_budget", "invalid_utf8", "control", "secret"} {
		t.Run(scenario, func(t *testing.T) {
			f, packet, raw := viewFixture(t)
			f.request.BudgetBytes = 32
			var p OutputViewProvider = viewProviderFunc(func(context.Context, string, []byte) ([]byte, error) {
				switch scenario {
				case "error":
					return nil, errors.New("private stderr must never escape")
				case "same_size":
					return raw, nil
				case "larger":
					return append(bytes.Clone(raw), 'x'), nil
				case "over_budget":
					return []byte(strings.Repeat("x", 33)), nil
				case "invalid_utf8":
					return []byte{0xff}, nil
				case "control":
					return []byte("\x1b[31mvalue"), nil
				case "secret":
					return []byte(`{"password":"hidden"}`), nil
				default:
					return nil, nil
				}
			})
			if scenario == "unavailable" {
				p = nil
			}
			view, err := f.gateway.ViewOutput(context.Background(), f.request, packet.Ref, p, inventoryOptions())
			if err != nil || view.Derived || view.Text != "" || view.Representation != "reference_only" || strings.Contains(view.Diagnostic, "private") {
				t.Fatalf("unsafe fallback: %+v %v", view, err)
			}
			exact, err := f.gateway.Recover(context.Background(), f.request, view.Raw)
			if err != nil || !bytes.Equal(exact, raw) {
				t.Fatal("fallback lost exact snapshot")
			}
		})
	}
}

func TestOutputViewRechecksBeforeAnyEmission(t *testing.T) {
	for _, scenario := range []string{"revoked", "changed", "cancelled", "parent_deadline", "error_then_revoked", "timeout_then_revoked", "timeout_then_changed"} {
		t.Run(scenario, func(t *testing.T) {
			f, packet, _ := viewFixture(t)
			ctx, cancel := context.WithCancel(context.Background())
			if scenario == "parent_deadline" {
				cancel()
				ctx, cancel = context.WithTimeout(context.Background(), 100*time.Millisecond)
			}
			defer cancel()
			provider := viewProviderFunc(func(providerCtx context.Context, _ string, _ []byte) ([]byte, error) {
				switch scenario {
				case "revoked", "error_then_revoked", "timeout_then_revoked":
					f.allowed.Store(false)
				case "changed", "timeout_then_changed":
					f.generation.Store(Digest("new-generation"))
				case "cancelled":
					cancel()
				case "parent_deadline":
					<-providerCtx.Done()
					return nil, providerCtx.Err()
				}
				if scenario == "error_then_revoked" {
					return nil, errors.New("unavailable")
				}
				if strings.HasPrefix(scenario, "timeout_then_") {
					return nil, context.DeadlineExceeded
				}
				return []byte("view"), nil
			})
			view, err := f.gateway.ViewOutput(ctx, f.request, packet.Ref, provider, inventoryOptions())
			if err == nil || view != (OutputView{}) {
				t.Fatalf("revocation leaked view/fallback: %+v %v", view, err)
			}
			if scenario == "cancelled" && !errors.Is(err, context.Canceled) || scenario == "parent_deadline" && !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("parent cancellation/deadline not preserved: %v", err)
			}
		})
	}
}

func TestOutputViewProviderOwnTimeoutFallsBackAfterRecovery(t *testing.T) {
	for _, scenario := range []string{"own_deadline", "own_cancel", "gateway_transform_budget"} {
		t.Run(scenario, func(t *testing.T) {
			f, packet, raw := viewFixture(t)
			before := f.store.reads.Load()
			provider := viewProviderFunc(func(ctx context.Context, _ string, _ []byte) ([]byte, error) {
				switch scenario {
				case "own_cancel":
					return nil, context.Canceled
				case "gateway_transform_budget":
					<-ctx.Done()
					return []byte("late derived result must be discarded"), nil
				default:
					return nil, context.DeadlineExceeded
				}
			})
			view, err := f.gateway.ViewOutput(context.Background(), f.request, packet.Ref, provider, inventoryOptions())
			diagnostic := "provider_timeout"
			if scenario == "own_cancel" {
				diagnostic = "provider_failed"
			}
			if err != nil || view.Derived || view.Text != string(raw) || view.Representation != "exact" || view.Diagnostic != diagnostic || f.store.reads.Load() != before+2 {
				t.Fatalf("provider timeout failed to recover Direct: %+v %v reads=%d", view, err, f.store.reads.Load()-before)
			}
		})
	}
}

func TestOutputViewOneKiBBudgetRetainsReferenceOnly(t *testing.T) {
	f := newGatewayFixture(t)
	f.request.Source = "tool_read"
	// Retain the original failed AB/BA preflight's 1 KiB fixture budget;
	// the later 32 KiB experiment does not change production limits.
	f.request.BudgetBytes = 1024
	doc := exactDocument()
	doc.Raw = []byte(strings.Repeat("src/example.go:12: referenced_symbol\n", 50))
	packet := mustRetrieve(t, f, f.request, doc)
	if len(doc.Raw) != 1850 || packet.Representation != "reference_only" || packet.Text != "" {
		t.Fatal("preflight setup did not retain the 1 KiB budget")
	}
	for _, scenario := range []string{"disabled", "provider_timeout"} {
		t.Run(scenario, func(t *testing.T) {
			opts := inventoryOptions()
			opts.Enabled = scenario != "disabled"
			provider := viewProviderFunc(func(context.Context, string, []byte) ([]byte, error) { return nil, context.DeadlineExceeded })
			view, err := f.gateway.ViewOutput(context.Background(), f.request, packet.Ref, provider, opts)
			if err != nil || view.Text != "" || view.Derived || view.ViewBytes != 0 || view.RawBytes != 1850 || view.Representation != "reference_only" {
				t.Fatalf("1 KiB fallback emitted raw text: %+v %v", view, err)
			}
			if scenario == "provider_timeout" && view.Diagnostic != "provider_timeout" {
				t.Fatal("missing fixed timeout diagnostic")
			}
			exact, err := f.gateway.Recover(context.Background(), f.request, view.Raw)
			if err != nil || !bytes.Equal(exact, doc.Raw) {
				t.Fatal("1 KiB budget damaged exact recovery")
			}
		})
	}
}

func TestOutputViewRejectsForeignOrForgedReferenceBeforeProvider(t *testing.T) {
	for _, field := range []string{"owner", "scope", "generation", "seal", "artifact", "mutable_request"} {
		t.Run(field, func(t *testing.T) {
			f, packet, _ := viewFixture(t)
			switch field {
			case "owner":
				f.request.Owner.TaskID = "other-task"
			case "scope":
				f.request.Scope.Permissions = Digest("other-permissions")
			case "generation":
				f.generation.Store(Digest("different-generation"))
			case "seal":
				packet.Ref.Seal = Digest("forged")
			case "artifact":
				packet.Ref.Result.Artifact.ID = "other-artifact"
			case "mutable_request":
				f.request.ReadOnly = false
			}
			provider := viewProviderFunc(func(context.Context, string, []byte) ([]byte, error) {
				t.Fatal("unadmitted provider invoked")
				return nil, nil
			})
			if _, err := f.gateway.ViewOutput(context.Background(), f.request, packet.Ref, provider, inventoryOptions()); err == nil {
				t.Fatal("invalid reference admitted")
			}
		})
	}
}
