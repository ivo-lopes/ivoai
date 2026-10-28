package nativecodex

import (
	"context"
	"time"

	"github.com/ivo-lopes/ivoai/internal/core"
)

// Receipt is host correlation, not a new grant. Empty thread/turn fields occur
// only before the official server has created those identities.
type Receipt struct {
	SessionID         string
	TaskID            string
	FrontendSessionID string
	ThreadID          string
	TurnID            string
	CallID            string
}

// Options is diagnostic-only and requires an explicit synthetic loopback
// provider. Native worker admission is unverified; real-provider execution is
// rejected before host callbacks or executable probes. It requires the host's
// live admission, ownership and final
// validation. No environment, credentials, grants or scheduler are discovered.
// These trusted callbacks must honor context; Go callbacks are cooperative.
type Options struct {
	Enabled     bool
	SessionID   string
	Binary      string
	SHA256      string
	Directory   string
	Environment []string
	Registry    *Registry
	TurnTimeout time.Duration
	Authorize   func(context.Context, core.ExecutorRequest, Receipt) error
	BindThread  func(context.Context, string, bool) error
	Validate    func(context.Context, core.ExecutorRequest, Receipt) error
	// SyntheticProviderURL is mandatory and accepts only a loopback HTTP /v1
	// fixture. No real-provider mode or escape override is implemented.
	SyntheticProviderURL string
}

type Runner struct {
	options Options
	busy    chan struct{}
}

func New(options Options) *Runner {
	options.Environment = append([]string(nil), options.Environment...)
	return &Runner{options: options, busy: make(chan struct{}, 1)}
}

var _ core.ExecutorRunner = (*Runner)(nil)

func failed(class string) error { return &core.ExecutorFailure{Class: class, ExitCode: -1} }
