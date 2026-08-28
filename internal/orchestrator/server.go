// Package orchestrator implements the session-local ivoai-orchestrator MCP.
// It is deliberately stdio-only and cannot be exposed by the remote gateway.
package orchestrator

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/ivo-lopes/ivoai/internal/core"
	"github.com/ivo-lopes/ivoai/internal/observability"
	"github.com/ivo-lopes/ivoai/internal/quota"
	"github.com/ivo-lopes/ivoai/internal/routing"
	"github.com/ivo-lopes/ivoai/internal/session"
	"github.com/ivo-lopes/ivoai/internal/workers"
	"github.com/ivo-lopes/ivoai/internal/workingcontext"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

var rolePattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_-]{0,63}$`)

type Server struct {
	Store                 session.Store
	SessionID             string
	Adapter               WorkerAdapter
	Control               LifecycleControl
	Directory             string
	RuntimeDir            string
	ReviewExecutor        string
	Quota                 *quota.Manager
	CheckpointEnabled     bool
	BootstrapRequired     bool
	ProgressiveEscalation bool
	Parallelism           bool
	Weights               routing.Weights
	Registry              routing.Registry
	Overrides             map[string]map[routing.Tier]routing.ProfileOverride
	WorkingContext        workingcontext.ArtifactStore
	Compressor            workingcontext.RepresentationCompressor

	mu      sync.Mutex
	results map[string]workerResult
	order   []string
	cancels map[string]context.CancelFunc
	plans   map[string]*runtimePlan
	runCtx  context.Context
	notify  chan struct{}
}

type WorkerAdapter interface {
	Run(context.Context, workers.Request, func(workers.Observation)) (workers.Result, error)
}

type LifecycleControl interface {
	RegisterLifecycle(context.Context, string, string) (string, error)
	CancelLifecycle(context.Context, string) error
}

type workerResult struct {
	Result   workingcontext.WorkerResult `json:"result"`
	State    string                      `json:"state"`
	ExitCode int                         `json:"exit_code,omitempty"`
}

func (s *Server) Run(ctx context.Context) error {
	if err := s.authorized(); err != nil {
		return err
	}
	s.initializeContext(ctx)
	return s.protocolServer().Run(ctx, &mcp.StdioTransport{})
}

func (s *Server) initialize() {
	s.initializeContext(context.Background())
}

func (s *Server) initializeContext(ctx context.Context) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.results == nil {
		s.results = map[string]workerResult{}
	}
	if s.cancels == nil {
		s.cancels = map[string]context.CancelFunc{}
	}
	if s.plans == nil {
		s.plans = map[string]*runtimePlan{}
	}
	if s.notify == nil {
		s.notify = make(chan struct{})
	}
	if s.runCtx == nil {
		s.runCtx = ctx
	}
	if s.WorkingContext == nil && filepath.IsAbs(s.RuntimeDir) {
		if local, err := workingcontext.NewLocalStore(filepath.Join(s.RuntimeDir, "working-context"), workingcontext.LocalOptions{}); err == nil {
			s.WorkingContext = local
		}
	}
	if value, err := s.Store.Get(s.SessionID); err == nil {
		for _, worker := range value.Workers {
			if len(worker.ResultRefs) == 0 || s.results[worker.ID].State != "" {
				continue
			}
			status := workingcontext.ResultCompleted
			if worker.State == session.StateFailed {
				status = workingcontext.ResultFailed
			}
			s.results[worker.ID] = workerResult{State: string(worker.State), Result: workingcontext.WorkerResult{Status: status, Summary: "Prior worker evidence remains available by ResultRef after primary failover.", Evidence: append([]workingcontext.ResultRef(nil), worker.ResultRefs...)}}
			s.order = append(s.order, worker.ID)
		}
	}
	if s.Weights == (routing.Weights{}) {
		s.Weights = routing.DefaultWeights()
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
	server.AddTool(&mcp.Tool{Name: "orchestration_result", Description: "Read a bounded structured WorkerResult; only opaque evidence references survive primary failover.", InputSchema: object(map[string]any{"worker_id": map[string]any{"type": "string"}}, "worker_id"), Annotations: read}, s.result)
	server.AddTool(&mcp.Tool{Name: "orchestration_artifact_read", Description: "Explicitly recover exact private worker evidence by opaque ResultRef for the active session.", InputSchema: object(map[string]any{"artifact_id": map[string]any{"type": "string", "pattern": `^artifact_[0-9a-f]{32}$`}}, "artifact_id"), Annotations: read}, s.artifactRead)
	server.AddTool(&mcp.Tool{Name: "orchestration_artifact_read_range", Description: "Explicitly recover a bounded byte range from private worker evidence by opaque ResultRef.", InputSchema: object(map[string]any{"artifact_id": map[string]any{"type": "string", "pattern": `^artifact_[0-9a-f]{32}$`}, "offset": map[string]any{"type": "integer", "minimum": 0}, "length": map[string]any{"type": "integer", "minimum": 1, "maximum": 1048576}}, "artifact_id", "offset", "length"), Annotations: read}, s.artifactReadRange)
	server.AddTool(&mcp.Tool{Name: "orchestration_cancel", Description: "Cancel a worker owned by this active session.", InputSchema: object(map[string]any{"worker_id": map[string]any{"type": "string"}}, "worker_id"), Annotations: write}, s.cancel)
	value, _ := s.Store.Get(s.SessionID)
	if value.Mode == session.ModeAuto {
		s.addAutomaticTools(server, read, write)
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
		projected := s.projectWorkerResult("", workerID, session.StateFailed, workers.Result{ExitCode: 1}, err, workingcontext.DefaultContextBudget)
		s.finish(workerID, session.StateFailed, 1, projected)
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
	projected := s.projectWorkerResult("", workerID, state, result, runErr, workingcontext.DefaultContextBudget)
	s.finish(workerID, state, exitCode, projected)
	_ = s.Control.CancelLifecycle(context.Background(), taskID)
	if runErr != nil {
		return nil, runErr
	}
	return toolResult(map[string]any{"worker_id": workerID, "executor": args.Executor, "model": result.Model, "headroom_used": result.HeadroomUsed, "result": projected})
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

func (s *Server) artifactRead(ctx context.Context, request *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	id, err := artifactIDArgument(request)
	if err != nil {
		return nil, err
	}
	if s.WorkingContext == nil {
		return nil, errors.New("working context artifact store is unavailable")
	}
	reader, ref, err := s.WorkingContext.Read(ctx, workingcontext.Ownership{SessionID: s.SessionID}, id)
	if err != nil {
		s.observeArtifactAccess(id, false, 0, err)
		return nil, err
	}
	body, err := io.ReadAll(reader)
	closeErr := reader.Close()
	if err != nil {
		return nil, err
	}
	if closeErr != nil {
		return nil, closeErr
	}
	s.observeArtifactAccess(id, false, int64(len(body)), nil)
	return toolResult(map[string]any{"ref": ref, "encoding": "base64", "data": base64.StdEncoding.EncodeToString(body)})
}

func (s *Server) artifactReadRange(ctx context.Context, request *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	var args struct {
		ArtifactID string `json:"artifact_id"`
		Offset     int64  `json:"offset"`
		Length     int64  `json:"length"`
	}
	if strictArguments(request, &args) != nil || args.Offset < 0 || args.Length < 1 || args.Length > 1<<20 {
		return nil, errors.New("valid bounded artifact range is required")
	}
	if s.WorkingContext == nil {
		return nil, errors.New("working context artifact store is unavailable")
	}
	body, ref, err := s.WorkingContext.ReadRange(ctx, workingcontext.Ownership{SessionID: s.SessionID}, args.ArtifactID, args.Offset, args.Length)
	if err != nil {
		s.observeArtifactAccess(args.ArtifactID, true, 0, err)
		return nil, err
	}
	s.observeArtifactAccess(args.ArtifactID, true, int64(len(body)), nil)
	return toolResult(map[string]any{"ref": ref, "offset": args.Offset, "length": len(body), "encoding": "base64", "data": base64.StdEncoding.EncodeToString(body)})
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

func (s *Server) finish(id string, state session.State, exitCode int, result workingcontext.WorkerResult) {
	now := time.Now().UTC()
	_, _ = s.Store.Update(s.SessionID, func(value *session.Session) error {
		if worker := findWorker(value, id); worker != nil {
			worker.State, worker.EndedAt, worker.ExitCode = state, &now, &exitCode
			worker.ResultRefs = append([]workingcontext.ResultRef(nil), result.Evidence...)
			eventState := observability.StateCompleted
			if state == session.StateFailed {
				eventState = observability.StateFailed
			}
			component := core.ComponentCodex
			if worker.Executor == "claude" {
				component = core.ComponentClaude
			}
			return session.AppendObservation(value, observability.Event{Category: observability.CategoryWorker, Operation: observability.OperationWorkerLifecycle, State: eventState, TaskID: worker.TaskID, WorkerID: id, Provider: worker.Executor, Executor: worker.Executor, Component: component, DurationMilliseconds: now.Sub(worker.StartedAt).Milliseconds()})
		}
		return nil
	})
	s.mu.Lock()
	if len(s.results) >= 8 && len(s.order) > 0 {
		delete(s.results, s.order[0])
		s.order = s.order[1:]
	}
	s.results[id] = workerResult{Result: result, State: string(state), ExitCode: exitCode}
	s.order = append(s.order, id)
	s.mu.Unlock()
}

func (s *Server) projectWorkerResult(taskID, workerID string, state session.State, raw workers.Result, runErr error, budget int) workingcontext.WorkerResult {
	status := workingcontext.ResultCompleted
	if state == session.StateFailed {
		status = workingcontext.ResultFailed
	}
	if errors.Is(runErr, context.Canceled) {
		status = workingcontext.ResultCancelled
	}
	projector := workingcontext.Projector{Store: s.WorkingContext, Compressor: s.Compressor, Observe: func(event observability.Event) {
		_, _ = s.Store.Update(s.SessionID, func(current *session.Session) error { return session.AppendObservation(current, event) })
	}}
	return projector.Project(context.Background(), workingcontext.ProjectionInput{Owner: workingcontext.Ownership{SessionID: s.SessionID, TaskID: taskID, WorkerID: workerID}, Raw: raw.Evidence(), MediaType: "application/vnd.ivoai.worker-evidence", Status: status, ExitCode: raw.ExitCode, Failure: runErr, ContextBudget: budget, Truncated: raw.Truncated, PayloadType: "worker_output", AssociationID: workerID})
}

func artifactIDArgument(request *mcp.CallToolRequest) (string, error) {
	var args struct {
		ArtifactID string `json:"artifact_id"`
	}
	if strictArguments(request, &args) != nil || len(args.ArtifactID) != 41 || !strings.HasPrefix(args.ArtifactID, "artifact_") {
		return "", errors.New("valid artifact_id is required")
	}
	return args.ArtifactID, nil
}

func (s *Server) observeArtifactAccess(id string, ranged bool, size int64, accessErr error) {
	operation, state, reason := observability.OperationArtifactStoreRead, observability.StateCompleted, observability.ReasonArtifactRecovered
	if ranged {
		operation = observability.OperationArtifactStoreRangeRead
	}
	if accessErr != nil {
		operation, state, reason = observability.OperationArtifactStoreDenied, observability.StateDenied, observability.ReasonAccessDenied
	}
	_, _ = s.Store.Update(s.SessionID, func(current *session.Session) error {
		return session.AppendObservation(current, observability.Event{Category: observability.CategoryWorkingContext, Operation: operation, State: state, Component: core.ComponentWorkingContext, ArtifactID: id, ArtifactBytes: size, RoutingReason: reason})
	})
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
