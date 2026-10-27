package evidence

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/ivo-lopes/ivoai/internal/platform"
)

// OutputViewProvider only transforms a snapshot. It receives no command,
// executor, authorization callback, artifact store or capability to replay it.
type OutputViewProvider interface {
	Transform(context.Context, string, []byte) ([]byte, error)
}

// OutputViewOptions is host-owned policy, never inferred from tool output.
// The zero value disables views; activation is programmatic, not a CLI flag.
// Only inventory views are admitted by this first experimental slice. Errors,
// diffs, stack traces and test reports require their exact representation.
type OutputViewOptions struct {
	Enabled            bool
	AllowDerived       bool
	Kind               string
	Filter             string
	CompressionApplied bool
}

// OutputView is an ephemeral, untrusted presentation of an existing artifact.
// Raw always names the exact stored snapshot. A derived Text is not sufficient
// evidence of correctness and never replaces the artifact or its fidelity.
type OutputView struct {
	Raw            Ref    `json:"raw"`
	Text           string `json:"text,omitempty"`
	Representation string `json:"representation"`
	Derived        bool   `json:"derived"`
	Untrusted      bool   `json:"untrusted"`
	Diagnostic     string `json:"diagnostic"`
	RawBytes       int    `json:"raw_bytes"`
	ViewBytes      int    `json:"view_bytes"`
}

// ViewOutput cannot fetch or reexecute a tool operation: the caller must first
// persist its completed output and obtain a sealed Ref through the Gateway.
// Every return of evidence repeats exact recovery, including Direct bypasses.
// Revocation, parent-context cancellation and generation changes fail closed.
// An optional provider's own timeout/failure falls back after exact recovery.
func (g *Gateway) ViewOutput(ctx context.Context, r Request, ref Ref, p OutputViewProvider, options OutputViewOptions) (OutputView, error) {
	if err := ctx.Err(); err != nil {
		return OutputView{}, err
	}
	raw, err := g.Recover(ctx, r, ref)
	if err != nil {
		return OutputView{}, err
	}
	view := OutputView{Raw: ref, Untrusted: true, RawBytes: len(raw), Representation: "exact", Diagnostic: "disabled"}
	if len(raw) <= r.BudgetBytes {
		view.Text, view.ViewBytes = string(raw), len(raw)
	} else {
		view.Representation = "reference_only"
	}
	eligible := options.Enabled && options.AllowDerived && !options.CompressionApplied && options.Kind == "inventory" && r.Source == "tool_read" && ref.Basis == Extracted && ref.SourceComplete && ref.Result.Artifact.MediaType == "text/plain" && viewFilter(options.Filter)
	if options.Enabled && !eligible {
		view.Diagnostic = "policy_bypass"
	}
	if eligible && p == nil {
		view.Diagnostic = "provider_unavailable"
	}
	if eligible && p != nil {
		bounded, cancel := context.WithTimeout(ctx, 2*time.Second)
		candidate, transformErr := p.Transform(bounded, options.Filter, bytes.Clone(raw))
		deadlineErr := bounded.Err()
		cancel()
		if err := ctx.Err(); err != nil {
			return OutputView{}, err
		}
		view.Diagnostic = "provider_failed"
		if errors.Is(deadlineErr, context.DeadlineExceeded) || errors.Is(transformErr, context.DeadlineExceeded) {
			view.Diagnostic = "provider_timeout"
		} else if deadlineErr == nil && transformErr == nil {
			view.Diagnostic = "view_rejected"
			if len(candidate) > 0 && len(candidate) < len(raw) && len(candidate) <= r.BudgetBytes && safeView(candidate) {
				view.Text, view.ViewBytes = string(candidate), len(candidate)
				view.Representation, view.Derived, view.Diagnostic = "derived_non_authoritative", true, "applied"
			}
		}
	}
	if err := ctx.Err(); err != nil {
		return OutputView{}, err
	}
	current, err := g.Recover(ctx, r, ref)
	if err != nil {
		return OutputView{}, err
	}
	if !bytes.Equal(raw, current) {
		return OutputView{}, ErrChanged
	}
	if err := ctx.Err(); err != nil {
		return OutputView{}, err
	}
	return view, nil
}

func viewFilter(filter string) bool {
	return filter == "git-status" || filter == "git-log" || filter == "grep"
}

func safeView(body []byte) bool {
	if !utf8.Valid(body) || sensitiveAssignment.Match(body) || privateKey.Match(body) || platform.Redact(string(body)) != string(body) {
		return false
	}
	return !strings.ContainsFunc(string(body), func(r rune) bool { return r < 32 && r != '\n' && r != '\t' || r == 127 })
}
