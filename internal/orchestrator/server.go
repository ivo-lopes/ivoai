// Package orchestrator implements the session-local ivoai-orchestrator MCP.
// It is deliberately stdio-only and cannot be exposed by the remote gateway.
package orchestrator

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/ivo-lopes/ivoai/internal/orchestration"
	"github.com/ivo-lopes/ivoai/internal/quota"
	"github.com/ivo-lopes/ivoai/internal/session"
	"github.com/ivo-lopes/ivoai/internal/workers"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

var rolePattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_-]{0,63}$`)

type Server struct {
	Store             session.Store
	SessionID         string
	Adapter           workers.Adapter
	Control           orchestration.ControlPlane
	Directory         string
	RuntimeDir        string
	ReviewExecutor    string
	Quota             *quota.Manager
	CheckpointEnabled bool

	mu      sync.Mutex
	results map[string]workerResult
	order   []string
	cancels map[string]context.CancelFunc
}

type workerResult struct {
	Text     string `json:"text,omitempty"`
	State    string `json:"state"`
	ExitCode int    `json:"exit_code,omitempty"`
}

func (s *Server) Run(ctx context.Context) error {
	if err := s.authorized(); err != nil {
		return err
	}
	s.initialize()
	return s.protocolServer().Run(ctx, &mcp.StdioTransport{})
}

func (s *Server) initialize() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.results == nil {
		s.results = map[string]workerResult{}
	}
	if s.cancels == nil {
		s.cancels = map[string]context.CancelFunc{}
	}
}

func (s *Server) protocolServer() *mcp.Server {
	server := mcp.NewServer(&mcp.Implementation{Name: "ivoai-orchestrator", Version: "1", Description: "Session-local delegation to official Codex and Claude Code workers"}, &mcp.ServerOptions{Instructions: "Delegate only bounded tasks needed by the active ivoai session. Ruflo coordinates lifecycle but never performs inference."})
	s.addTools(server)
	return server
}

func (s *Server) authorized() error {
	value, err := s.Store.Get(s.SessionID)
	if err != nil {
		return err
	}
	if !value.Active() || (value.Mode != session.ModeOrchestrated && value.Mode != session.ModeAuto) || value.SwarmID == "" || !value.RufloHealthy || !value.RufloSafeMode || value.ProviderExecution {
		return errors.New("orchestration bridge requires an active safe orchestrated session")
	}
	return nil
}

func (s *Server) addTools(server *mcp.Server) {
	read := &mcp.ToolAnnotations{ReadOnlyHint: true, DestructiveHint: boolp(false), OpenWorldHint: boolp(false), IdempotentHint: true}
	write := &mcp.ToolAnnotations{ReadOnlyHint: false, DestructiveHint: boolp(false), OpenWorldHint: boolp(false), IdempotentHint: false}
	server.AddTool(&mcp.Tool{Name: "orchestration_status", Description: "Return non-sensitive status for the active session and Ruflo swarm.", InputSchema: object(nil), Annotations: read}, s.status)
	server.AddTool(&mcp.Tool{Name: "orchestration_agents", Description: "List primary and worker lifecycle metadata; never returns prompts or credentials.", InputSchema: object(nil), Annotations: read}, s.agents)
	server.AddTool(&mcp.Tool{Name: "orchestration_delegate", Description: "Run a bounded task through an official Codex or Claude Code worker. Ruflo only records lifecycle.", InputSchema: object(map[string]any{
		"role":               map[string]any{"type": "string", "pattern": `^[A-Za-z][A-Za-z0-9_-]{0,63}$`},
		"task":               map[string]any{"type": "string", "minLength": 1, "maxLength": workers.MaxTaskBytes},
		"preferred_executor": map[string]any{"type": "string", "enum": []string{"codex", "claude"}},
		"model":              map[string]any{"type": "string", "minLength": 1, "maxLength": 128},
	}, "role", "task"), Annotations: write}, s.delegate)
	server.AddTool(&mcp.Tool{Name: "orchestration_result", Description: "Read a bounded worker result retained only in this bridge process.", InputSchema: object(map[string]any{"worker_id": map[string]any{"type": "string"}}, "worker_id"), Annotations: read}, s.result)
	server.AddTool(&mcp.Tool{Name: "orchestration_cancel", Description: "Cancel a worker owned by this active session.", InputSchema: object(map[string]any{"worker_id": map[string]any{"type": "string"}}, "worker_id"), Annotations: write}, s.cancel)
	value, _ := s.Store.Get(s.SessionID)
	if value.Mode == session.ModeAuto {
		server.AddTool(&mcp.Tool{Name: "orchestration_quota", Description: "Read authoritative or explicitly unavailable subscription quota telemetry. The model cannot change quota state.", InputSchema: object(nil), Annotations: read}, s.quotaStatus)
		if s.CheckpointEnabled {
			server.AddTool(&mcp.Tool{Name: "orchestration_checkpoint", Description: "Save a bounded, secret-free continuity checkpoint after a relevant completed turn. Do not include transcripts, prompts, credentials, tokens, or raw responses.", InputSchema: checkpointSchema(), Annotations: write}, s.checkpoint)
		}
	}
}

func (s *Server) status(_ context.Context, _ *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	value, err := s.Store.Get(s.SessionID)
	if err != nil {
		return nil, err
	}
	return toolResult(map[string]any{"session_id": value.SessionID, "mode": value.Mode, "state": value.State, "swarm_id": value.SwarmID, "swarm_state": value.SwarmState, "provider_execution": value.ProviderExecution, "workers": len(value.Workers), "max_workers": value.MaxWorkers})
}

func (s *Server) agents(_ context.Context, _ *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	value, err := s.Store.Get(s.SessionID)
	if err != nil {
		return nil, err
	}
	return toolResult(map[string]any{"primary": map[string]any{"executor": value.PrimaryExecutor, "model": value.PrimaryModel, "state": value.State}, "workers": value.Workers})
}

func (s *Server) delegate(ctx context.Context, request *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	var args struct {
		Role     string `json:"role"`
		Task     string `json:"task"`
		Executor string `json:"preferred_executor"`
		Model    string `json:"model"`
	}
	if err := json.Unmarshal(request.Params.Arguments, &args); err != nil || !rolePattern.MatchString(args.Role) || args.Task == "" || len(args.Task) > workers.MaxTaskBytes {
		return nil, errors.New("valid role and bounded task are required")
	}
	if args.Executor == "" {
		args.Executor = s.ReviewExecutor
	}
	if args.Executor != "codex" && args.Executor != "claude" {
		return nil, errors.New("preferred_executor must be codex or claude")
	}
	requestedExecutor := args.Executor
	fallbackReason := ""
	value, err := s.Store.Get(s.SessionID)
	if err != nil {
		return nil, err
	}
	if value.Mode == session.ModeAuto {
		if s.Quota == nil {
			return nil, errors.New("automatic session quota manager is unavailable")
		}
		decision, routeErr := s.Quota.Resolve(ctx, quota.Provider(args.Executor), args.Model, true)
		if routeErr != nil {
			return nil, routeErr
		}
		args.Executor = string(decision.Resolved)
		fallbackReason = decision.Reason
		if args.Executor != requestedExecutor {
			args.Model = ""
		}
		_, _ = s.Store.Update(s.SessionID, func(current *session.Session) error {
			if current.Quota == nil {
				current.Quota = map[quota.Provider]quota.ProviderQuota{}
			}
			current.Quota[decision.Quota.Provider] = decision.Quota
			return nil
		})
	}
	workerID, err := newWorkerID()
	if err != nil {
		return nil, err
	}
	started := time.Now().UTC()
	_, err = s.Store.Update(s.SessionID, func(value *session.Session) error {
		active := 0
		for _, worker := range value.Workers {
			if worker.State == session.StateStarting || worker.State == session.StateRunning || worker.State == session.StateStopping {
				active++
			}
		}
		if active >= value.MaxWorkers || active >= 3 {
			return errors.New("session worker limit reached")
		}
		value.Workers = append(value.Workers, session.Worker{ID: workerID, Role: args.Role, RequestedExecutor: requestedExecutor, Executor: args.Executor, FallbackReason: fallbackReason, Model: session.ResolveModel("", args.Model, args.Executor, ""), State: session.StateStarting, StartedAt: started})
		return nil
	})
	if err != nil {
		return nil, err
	}
	taskID, err := s.Control.RegisterLifecycle(ctx, "worker", workerID)
	if err != nil {
		s.finish(workerID, session.StateFailed, 1, "")
		return nil, err
	}
	_, _ = s.Store.Update(s.SessionID, func(value *session.Session) error {
		if worker := findWorker(value, workerID); worker != nil {
			worker.RufloTaskID = taskID
		}
		return nil
	})
	workerCtx, cancel := context.WithCancel(ctx)
	s.mu.Lock()
	s.cancels[workerID] = cancel
	s.mu.Unlock()
	result, runErr := s.Adapter.Run(workerCtx, workers.Request{Executor: args.Executor, Task: args.Task, Model: args.Model, Directory: s.Directory, Runtime: s.RuntimeDir}, func(observation workers.Observation) {
		_, _ = s.Store.Update(s.SessionID, func(value *session.Session) error {
			if worker := findWorker(value, workerID); worker != nil {
				worker.PID, worker.ProcessStart, worker.HeadroomUsed, worker.State = observation.PID, observation.ProcessStart, observation.HeadroomUsed, session.StateRunning
			}
			return nil
		})
	})
	if runErr != nil && value.Mode == session.ModeAuto && s.Quota != nil && quotaLimitError(args.Executor, runErr.Error()) {
		_ = s.Quota.MarkExhausted(quota.Provider(args.Executor), "official worker reported a subscription limit")
		decision, routeErr := s.Quota.Resolve(ctx, quota.Other(quota.Provider(args.Executor)), "", true)
		if routeErr == nil && decision.Resolved != quota.Provider(args.Executor) {
			previous := args.Executor
			args.Executor, args.Model = string(decision.Resolved), ""
			_, _ = s.Store.Update(s.SessionID, func(current *session.Session) error {
				if worker := findWorker(current, workerID); worker != nil {
					worker.Executor = args.Executor
					worker.Model = session.UnknownModel()
					worker.FallbackReason = previous + " worker reported a subscription limit"
				}
				return nil
			})
			result, runErr = s.Adapter.Run(workerCtx, workers.Request{Executor: args.Executor, Task: args.Task, Directory: s.Directory, Runtime: s.RuntimeDir}, func(observation workers.Observation) {
				_, _ = s.Store.Update(s.SessionID, func(current *session.Session) error {
					if worker := findWorker(current, workerID); worker != nil {
						worker.PID, worker.ProcessStart, worker.HeadroomUsed, worker.State = observation.PID, observation.ProcessStart, observation.HeadroomUsed, session.StateRunning
					}
					return nil
				})
			})
		}
	}
	cancel()
	s.mu.Lock()
	delete(s.cancels, workerID)
	s.mu.Unlock()
	exitCode, state := result.ExitCode, session.StateCompleted
	if runErr != nil {
		state = session.StateFailed
		if exitCode == 0 {
			exitCode = 1
		}
	}
	if value.Mode == session.ModeAuto && s.Quota != nil {
		refreshed, _ := s.Quota.Probe(context.Background(), quota.Provider(args.Executor), true)
		_, _ = s.Store.Update(s.SessionID, func(current *session.Session) error {
			if current.Quota == nil {
				current.Quota = map[quota.Provider]quota.ProviderQuota{}
			}
			current.Quota[refreshed.Provider] = refreshed
			return nil
		})
	}
	s.finish(workerID, state, exitCode, result.Text)
	_ = s.Control.CancelLifecycle(context.Background(), taskID)
	if runErr != nil {
		return nil, runErr
	}
	return toolResult(map[string]any{"worker_id": workerID, "executor": args.Executor, "model": result.Model, "headroom_used": result.HeadroomUsed, "result": result.Text})
}

func quotaLimitError(executor, message string) bool {
	if executor == "claude" {
		return quota.IsClaudeLimitError(message)
	}
	return quota.IsCodexLimitError(message)
}

func (s *Server) quotaStatus(ctx context.Context, _ *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	value, err := s.Store.Get(s.SessionID)
	if err != nil {
		return nil, err
	}
	if value.Mode != session.ModeAuto || s.Quota == nil {
		return nil, errors.New("quota routing is available only in automatic sessions")
	}
	result := map[string]quota.ProviderQuota{}
	for _, provider := range []quota.Provider{quota.ProviderCodex, quota.ProviderClaude} {
		current, _ := s.Quota.Probe(ctx, provider, false)
		result[string(provider)] = current
	}
	return toolResult(map[string]any{"providers": result, "policy": "quota manager has authority over provider selection"})
}

func (s *Server) checkpoint(_ context.Context, request *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	value, err := s.Store.Get(s.SessionID)
	if err != nil {
		return nil, err
	}
	if value.Mode != session.ModeAuto {
		return nil, errors.New("checkpoints are available only in automatic sessions")
	}
	var checkpoint session.Checkpoint
	if err := json.Unmarshal(request.Params.Arguments, &checkpoint); err != nil {
		return nil, errors.New("invalid checkpoint")
	}
	if err := s.Store.SaveCheckpoint(s.SessionID, checkpoint); err != nil {
		return nil, err
	}
	saved, err := s.Store.LoadCheckpoint(s.SessionID)
	if err != nil {
		return nil, err
	}
	_, _ = s.Store.Update(s.SessionID, func(current *session.Session) error {
		current.CheckpointAvailable = true
		current.CheckpointUpdatedAt = &saved.UpdatedAt
		current.ConsecutiveFailovers = 0
		return nil
	})
	return toolResult(map[string]any{"saved": true, "updated_at": saved.UpdatedAt, "ai_memory": value.MemoryStatus})
}

func (s *Server) result(_ context.Context, request *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	id, err := workerIDArgument(request)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	value, ok := s.results[id]
	s.mu.Unlock()
	if !ok {
		return nil, errors.New("worker result is unavailable in this bridge process")
	}
	return toolResult(value)
}

func (s *Server) cancel(_ context.Context, request *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	id, err := workerIDArgument(request)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	cancel, ok := s.cancels[id]
	s.mu.Unlock()
	if !ok {
		return nil, errors.New("worker is not active")
	}
	cancel()
	return toolResult(map[string]any{"worker_id": id, "cancelled": true})
}

func (s *Server) finish(id string, state session.State, exitCode int, text string) {
	now := time.Now().UTC()
	_, _ = s.Store.Update(s.SessionID, func(value *session.Session) error {
		if worker := findWorker(value, id); worker != nil {
			worker.State, worker.EndedAt, worker.ExitCode = state, &now, &exitCode
		}
		return nil
	})
	s.mu.Lock()
	if len(s.results) >= 8 && len(s.order) > 0 {
		delete(s.results, s.order[0])
		s.order = s.order[1:]
	}
	s.results[id] = workerResult{Text: text, State: string(state), ExitCode: exitCode}
	s.order = append(s.order, id)
	s.mu.Unlock()
}

func findWorker(value *session.Session, id string) *session.Worker {
	for index := range value.Workers {
		if value.Workers[index].ID == id {
			return &value.Workers[index]
		}
	}
	return nil
}

func workerIDArgument(request *mcp.CallToolRequest) (string, error) {
	var args struct {
		WorkerID string `json:"worker_id"`
	}
	if json.Unmarshal(request.Params.Arguments, &args) != nil || !strings.HasPrefix(args.WorkerID, "worker_") || len(args.WorkerID) != 39 {
		return "", errors.New("valid worker_id is required")
	}
	return args.WorkerID, nil
}

func newWorkerID() (string, error) {
	id, err := session.NewID()
	if err != nil {
		return "", err
	}
	return "worker_" + strings.TrimPrefix(id, "sess_"), nil
}

func object(properties map[string]any, required ...string) map[string]any {
	if properties == nil {
		properties = map[string]any{}
	}
	value := map[string]any{"type": "object", "additionalProperties": false, "properties": properties}
	if len(required) > 0 {
		value["required"] = required
	}
	return value
}

func checkpointSchema() map[string]any {
	properties := map[string]any{}
	for _, name := range []string{"objective", "next_step"} {
		properties[name] = map[string]any{"type": "string", "maxLength": 4096}
	}
	for _, name := range []string{"decisions", "completed", "files_changed", "important_checks", "outstanding", "blockers"} {
		properties[name] = map[string]any{"type": "array", "maxItems": 64, "items": map[string]any{"type": "string", "maxLength": 1024}}
	}
	return object(properties)
}

func toolResult(value any) (*mcp.CallToolResult, error) {
	body, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(body)}}, StructuredContent: value}, nil
}

func boolp(value bool) *bool { return &value }

func (s *Server) String() string { return fmt.Sprintf("ivoai-orchestrator(%s)", s.SessionID) }
