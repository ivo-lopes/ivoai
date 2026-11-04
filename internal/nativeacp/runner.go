// Package nativeacp adapts the official Antigravity ACP interface to Core.
// It contains no scheduler, credential reader, provider API or chat renderer.
package nativeacp

import (
	"context"
	"encoding/json"
	"time"

	"github.com/ivo-lopes/ivoai/internal/core"
)

// Receipt is correlation, never authority. All callbacks remain Core-owned.
type Receipt struct {
	SessionID, FrontendSessionID, NativeSessionID string
}

// MCPServer is an exact, host-owned ACP stdio projection. It is never populated
// from prompts, native output or personal MCP configuration.
type MCPServer struct {
	Type    string                `json:"type,omitempty"`
	Name    string                `json:"name"`
	Command string                `json:"command,omitempty"`
	Args    []string              `json:"args,omitempty"`
	Env     []EnvironmentVariable `json:"env,omitempty"`
	URL     string                `json:"url,omitempty"`
	Headers []EnvironmentVariable `json:"headers,omitempty"`
}
type EnvironmentVariable struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// Options is supplied by a trusted host, never decoded from model output.
// Approve must evaluate the exact proposed native operation using the host's
// existing grants. Nil denies every operation requiring approval. The adapter
// never returns an allow-always decision or discovers a provider credential.
type Options struct {
	Enabled                                                      bool
	SessionID, Binary, SHA256, Directory, RuntimeDir, ProfileDir string
	Environment                                                  []string
	MCPServers                                                   []MCPServer
	AllowedTools                                                 []string
	TurnTimeout                                                  time.Duration
	Authorize                                                    func(context.Context, core.ExecutorRequest, Receipt) error
	BindSession                                                  func(context.Context, string, bool) error
	Validate                                                     func(context.Context, core.ExecutorRequest, Receipt) error
	Approve                                                      func(context.Context, core.ExecutorRequest, Receipt, json.RawMessage) (bool, error)
	Started                                                      func(context.Context, Receipt, int) error
}

type Runner struct {
	options Options
	busy    chan struct{}
}

func New(options Options) *Runner {
	options.Environment = append([]string(nil), options.Environment...)
	if options.AllowedTools != nil {
		options.AllowedTools = append([]string{}, options.AllowedTools...)
	}
	options.MCPServers = append([]MCPServer(nil), options.MCPServers...)
	for i := range options.MCPServers {
		options.MCPServers[i].Args = append([]string(nil), options.MCPServers[i].Args...)
		options.MCPServers[i].Env = append([]EnvironmentVariable(nil), options.MCPServers[i].Env...)
		options.MCPServers[i].Headers = append([]EnvironmentVariable(nil), options.MCPServers[i].Headers...)
	}
	return &Runner{options: options, busy: make(chan struct{}, 1)}
}

var _ core.ExecutorRunner = (*Runner)(nil)

func failed(class string) error { return &core.ExecutorFailure{Class: class, ExitCode: -1} }
