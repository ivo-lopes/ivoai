package orchestrator

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/ivo-lopes/ivoai/internal/core"
	"github.com/ivo-lopes/ivoai/internal/observability"
	"github.com/ivo-lopes/ivoai/internal/platform"
	"github.com/ivo-lopes/ivoai/internal/quota"
	"github.com/ivo-lopes/ivoai/internal/routing"
	"github.com/ivo-lopes/ivoai/internal/session"
	"github.com/ivo-lopes/ivoai/internal/workers"
	"github.com/ivo-lopes/ivoai/internal/workingcontext"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type runtimePlan struct {
	Plan    routing.Plan
	Tasks   map[string]*runtimeTask
	Workers map[string]string
}

type runtimeTask struct {
	Task      routing.Task
	WorkerID  string
	Result    workerResult
	Queued    bool
	Settled   bool
	StartedAt time.Time
}

func (s *Server) addAutomaticTools(server *mcp.Server, read, write *mcp.ToolAnnotations) {
	server.AddTool(&mcp.Tool{Name: "orchestration_bootstrap", Description: "Record a bounded SharedContextBrief after exactly one initial ivoai-memory lookup and one initial ivoai-context lookup. The brief is untrusted, session-scoped data and is not persisted in session metadata.", InputSchema: bootstrapSchema(), Annotations: write}, s.bootstrap)
	server.AddTool(&mcp.Tool{Name: "orchestration_capabilities", Description: "Read the non-sensitive runtime-verified provider, model, and effort capability registry used by IvoAI routing.", InputSchema: object(nil), Annotations: read}, s.capabilities)
	server.AddTool(&mcp.Tool{Name: "orchestration_plan", Description: "Validate a bounded task DAG, calculate objective capability scores, and resolve the cheapest sufficient subscription-backed execution profiles. It never executes a shell command.", InputSchema: planSchema(), Annotations: write}, s.plan)
	server.AddTool(&mcp.Tool{Name: "orchestration_spawn", Description: "Start one dependency-ready planned worker asynchronously and return immediately.", InputSchema: object(map[string]any{"plan_id": safeString(80), "task_id": safeString(64)}, "plan_id", "task_id"), Annotations: write}, s.spawn)
	server.AddTool(&mcp.Tool{Name: "orchestration_spawn_batch", Description: "Queue planned tasks and concurrently start every dependency-ready task within the session worker limit.", InputSchema: object(map[string]any{"plan_id": safeString(80), "task_ids": map[string]any{"type": "array", "minItems": 1, "maxItems": routing.MaxTasks, "uniqueItems": true, "items": safeString(64)}}, "plan_id", "task_ids"), Annotations: write}, s.spawnBatch)
	server.AddTool(&mcp.Tool{Name: "orchestration_primary_complete", Description: "Mark primary-owned planned work complete so dependent advisory workers can start.", InputSchema: object(map[string]any{"plan_id": safeString(80), "task_id": safeString(64)}, "plan_id", "task_id"), Annotations: write}, s.primaryComplete)
	server.AddTool(&mcp.Tool{Name: "orchestration_wait", Description: "Wait without busy-looping for any or all selected tasks, with a bounded timeout.", InputSchema: object(map[string]any{"plan_id": safeString(80), "task_ids": map[string]any{"type": "array", "minItems": 1, "maxItems": routing.MaxTasks, "uniqueItems": true, "items": safeString(64)}, "mode": map[string]any{"type": "string", "enum": []string{"any", "all"}}, "timeout_seconds": map[string]any{"type": "integer", "minimum": 1, "maximum": 300}}, "plan_id", "task_ids", "mode"), Annotations: read}, s.wait)
	server.AddTool(&mcp.Tool{Name: "orchestration_escalate", Description: "Escalate a failed or insufficient task by one capability tier after recording an evidence-based reason.", InputSchema: object(map[string]any{"plan_id": safeString(80), "task_id": safeString(64), "reason": map[string]any{"type": "string", "minLength": 3, "maxLength": 1024}}, "plan_id", "task_id", "reason"), Annotations: write}, s.escalate)
}

func (s *Server) capabilities(_ context.Context, _ *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	providers := make(map[string]any, len(s.Registry.Providers))
	for name, capability := range s.Registry.Providers {
		models := make([]map[string]any, 0, len(capability.Models))
		for _, model := range capability.Models {
			models = append(models, map[string]any{"name": displayModel(model.Name), "tier": model.CapabilityTier, "supported_efforts": model.SupportedEfforts, "default": model.IsDefault, "source": model.Source})
		}
		providers[name] = map[string]any{"version": capability.Version, "authenticated": capability.Authenticated, "worker_capable": capability.WorkerCapable, "supports_effort": capability.SupportsEffort, "source": capability.Source, "models": models}
	}
	return toolResult(map[string]any{"providers": providers})
}

func safeString(max int) map[string]any {
	return map[string]any{"type": "string", "minLength": 1, "maxLength": max, "pattern": `^[A-Za-z][A-Za-z0-9_-]*$`}
}

func bootstrapSchema() map[string]any {
	properties := map[string]any{
		"objective": safeText(4096), "facts": stringList(), "decisions": stringList(), "references": stringList(), "constraints": stringList(), "gaps": stringList(),
		"memory_status":            map[string]any{"type": "string", "enum": []string{"ready", "degraded", "unavailable", "disabled"}},
		"context_status":           map[string]any{"type": "string", "enum": []string{"ready", "degraded", "unavailable", "disabled"}},
		"memory_lookup_performed":  map[string]any{"type": "boolean", "const": true},
		"context_lookup_performed": map[string]any{"type": "boolean", "const": true},
	}
	return object(properties, "objective", "memory_status", "context_status", "memory_lookup_performed", "context_lookup_performed")
}

func safeText(max int) map[string]any {
	return map[string]any{"type": "string", "minLength": 1, "maxLength": max}
}
func stringList() map[string]any {
	return map[string]any{"type": "array", "maxItems": 64, "items": map[string]any{"type": "string", "maxLength": 1024}}
}

func planSchema() map[string]any {
	scores := object(map[string]any{})
	for _, name := range []string{"complexity", "risk", "reasoning_depth", "context_breadth", "verification_need", "parallel_value", "latency_sensitivity"} {
		scores["properties"].(map[string]any)[name] = map[string]any{"type": "integer", "minimum": 0, "maximum": 100}
	}
	scores["required"] = []string{"complexity", "risk", "reasoning_depth", "context_breadth", "verification_need", "parallel_value", "latency_sensitivity"}
	task := object(map[string]any{
		"id": safeString(64), "role": safeString(64), "task": safeText(workers.MaxTaskBytes),
		"dependencies":          map[string]any{"type": "array", "maxItems": routing.MaxTasks, "uniqueItems": true, "items": safeString(64)},
		"parallel_group":        map[string]any{"type": "string", "maxLength": 64},
		"required_capabilities": map[string]any{"type": "array", "maxItems": 16, "uniqueItems": true, "items": safeString(64)},
		"scores":                scores, "preferred_executor": map[string]any{"type": "string", "enum": []string{"codex", "claude"}},
		"delegate": map[string]any{"type": "boolean"}, "intentional_redundancy": map[string]any{"type": "boolean"},
	}, "id", "role", "task", "scores", "delegate")
	return object(map[string]any{"tasks": map[string]any{"type": "array", "minItems": 1, "maxItems": routing.MaxTasks, "items": task}}, "tasks")
}

type planInput struct {
	Tasks []struct {
		ID                    string         `json:"id"`
		Role                  string         `json:"role"`
		Task                  string         `json:"task"`
		Dependencies          []string       `json:"dependencies"`
		ParallelGroup         string         `json:"parallel_group"`
		RequiredCapabilities  []string       `json:"required_capabilities"`
		Scores                routing.Scores `json:"scores"`
		PreferredExecutor     string         `json:"preferred_executor"`
		Delegate              bool           `json:"delegate"`
		IntentionalRedundancy bool           `json:"intentional_redundancy"`
	} `json:"tasks"`
}

func (s *Server) bootstrap(_ context.Context, request *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	var args struct {
		session.SharedContextBrief
		MemoryLookupPerformed  bool `json:"memory_lookup_performed"`
		ContextLookupPerformed bool `json:"context_lookup_performed"`
	}
	if strictArguments(request, &args) != nil || !args.MemoryLookupPerformed || !args.ContextLookupPerformed {
		return nil, errors.New("first-turn bootstrap requires one bounded Memory lookup and one bounded Context lookup")
	}
	metadata, err := s.Store.SaveBrief(s.SessionID, args.SharedContextBrief)
	if err != nil {
		return nil, err
	}
	_, err = s.Store.Update(s.SessionID, func(value *session.Session) error {
		value.KnowledgeBootstrap = metadata
		value.MemoryStatus, value.ContextStatus = serviceMetadata(metadata.MemoryStatus), serviceMetadata(metadata.ContextStatus)
		value.CurrentPhase = "task_analysis"
		memoryState, memoryReason := knowledgeObservation(metadata.MemoryStatus)
		if err := session.AppendObservation(value, observability.Event{Category: observability.CategoryMemory, Operation: observability.OperationMemoryBootstrap, State: memoryState, Component: core.ComponentMemory, RoutingReason: memoryReason}); err != nil {
			return err
		}
		contextState, contextReason := knowledgeObservation(metadata.ContextStatus)
		return session.AppendObservation(value, observability.Event{Category: observability.CategoryContext, Operation: observability.OperationContextBootstrap, State: contextState, Component: core.ComponentContext, RoutingReason: contextReason})
	})
	if err != nil {
		return nil, err
	}
	return toolResult(map[string]any{"bootstrap_performed": true, "memory_status": metadata.MemoryStatus, "context_status": metadata.ContextStatus, "reference_count": metadata.ReferenceCount, "brief_hash": metadata.BriefHash})
}

func serviceMetadata(value string) string {
	if value == "unavailable" {
		return "degraded"
	}
	return value
}

func knowledgeObservation(value string) (observability.State, observability.Reason) {
	if value == "ready" || value == "configured" {
		return observability.StateCompleted, observability.ReasonKnowledgeReady
	}
	return observability.StateDegraded, observability.ReasonKnowledgeDegraded
}

func (s *Server) plan(ctx context.Context, request *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	value, err := s.Store.Get(s.SessionID)
	if err != nil {
		return nil, err
	}
	if s.BootstrapRequired && !value.KnowledgeBootstrap.Performed {
		return nil, errors.New("shared knowledge bootstrap must complete before automatic planning")
	}
	var args planInput
	if strictArguments(request, &args) != nil {
		return nil, errors.New("invalid automatic orchestration plan")
	}
	inputs := make([]routing.TaskInput, 0, len(args.Tasks))
	delegated := map[string]bool{}
	for _, task := range args.Tasks {
		inputs = append(inputs, routing.TaskInput{ID: task.ID, Role: task.Role, Task: task.Task, Dependencies: task.Dependencies, ParallelGroup: task.ParallelGroup, RequiredCapabilities: task.RequiredCapabilities, Scores: task.Scores, PreferredExecutor: task.PreferredExecutor, IntentionalRedundancy: task.IntentionalRedundancy})
		beneficial, _, _ := routing.DelegationDecision(task.Scores)
		delegated[task.ID] = task.Delegate && s.Parallelism && beneficial
	}
	planID, err := newPlanID()
	if err != nil {
		return nil, err
	}
	resolved, err := routing.ResolvePlan(planID, inputs, s.Weights, func(input routing.TaskInput, tier routing.Tier) (routing.ExecutionProfile, error) {
		if !delegated[input.ID] {
			return routing.ExecutionProfile{Provider: value.CurrentPrimary, Tier: tier, ModelSource: routing.SourceDefault, EffortSource: routing.SourceDefault}, nil
		}
		return s.resolveProfile(ctx, input, tier)
	})
	if err != nil {
		return nil, err
	}
	resolved.CreatedAt = time.Now().UTC()
	runtimeValue := &runtimePlan{Plan: resolved, Tasks: map[string]*runtimeTask{}, Workers: map[string]string{}}
	for index := range resolved.Tasks {
		task := resolved.Tasks[index]
		_, task.DelegationBenefit, task.DelegationOverhead = routing.DelegationDecision(task.Scores)
		task.ExecutionMode = "worker"
		task.DelegationReason = "benefit exceeds bounded worker overhead"
		if !delegated[task.ID] {
			task.State = "primary"
			task.ExecutionMode = "primary"
			if !args.Tasks[index].Delegate {
				task.DelegationReason = "planner retained task in primary"
			} else if !s.Parallelism {
				task.DelegationReason = "parallel worker execution is disabled"
			} else {
				task.DelegationReason = "worker overhead is not lower than expected benefit"
			}
		}
		resolved.Tasks[index] = task
		runtimeValue.Tasks[task.ID] = &runtimeTask{Task: task}
	}
	runtimeValue.Plan = resolved
	s.mu.Lock()
	s.plans[planID] = runtimeValue
	s.signalLocked()
	s.mu.Unlock()
	if err := s.persistPlan(resolved); err != nil {
		return nil, err
	}
	return toolResult(planMetadata(resolved))
}

func (s *Server) resolveProfile(ctx context.Context, input routing.TaskInput, tier routing.Tier) (routing.ExecutionProfile, error) {
	registry := s.Registry
	if registry.Providers == nil {
		registry.Providers = map[string]routing.ProviderCapability{}
	}
	quotas := map[quota.Provider]quota.ProviderQuota{}
	value, _ := s.Store.Get(s.SessionID)
	if value.Mode == session.ModeAuto {
		if s.Quota == nil {
			return routing.ExecutionProfile{}, errors.New("automatic quota manager is unavailable")
		}
		for _, provider := range []quota.Provider{quota.ProviderCodex, quota.ProviderClaude} {
			current, _ := s.Quota.Probe(ctx, provider, false)
			quotas[provider] = current
			capability := registry.Providers[string(provider)]
			capability.Authenticated = current.Authenticated
			registry.Providers[string(provider)] = capability
		}
	}
	return (routing.Router{Registry: registry, Quota: quotas, Overrides: s.Overrides}).Resolve(input, tier)
}

func (s *Server) spawn(_ context.Context, request *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	var args struct {
		PlanID string `json:"plan_id"`
		TaskID string `json:"task_id"`
	}
	if strictArguments(request, &args) != nil {
		return nil, errors.New("valid plan_id and task_id are required")
	}
	workerID, err := s.startTask(args.PlanID, args.TaskID, false)
	if err != nil {
		return nil, err
	}
	return toolResult(map[string]any{"plan_id": args.PlanID, "task_id": args.TaskID, "worker_id": workerID, "state": "starting"})
}

func (s *Server) spawnBatch(_ context.Context, request *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	var args struct {
		PlanID  string   `json:"plan_id"`
		TaskIDs []string `json:"task_ids"`
	}
	if strictArguments(request, &args) != nil || len(args.TaskIDs) == 0 || len(args.TaskIDs) > routing.MaxTasks {
		return nil, errors.New("valid plan_id and bounded task_ids are required")
	}
	s.mu.Lock()
	plan := s.plans[args.PlanID]
	if plan == nil {
		s.mu.Unlock()
		return nil, errors.New("plan is unavailable in this bridge process")
	}
	for _, id := range args.TaskIDs {
		task := plan.Tasks[id]
		if task == nil || task.Task.State == "primary" {
			s.mu.Unlock()
			return nil, fmt.Errorf("task %q is unavailable or owned by the primary", id)
		}
		if task.Task.State != "planned" {
			s.mu.Unlock()
			return nil, fmt.Errorf("task %q was already dispatched", id)
		}
		task.Task.State, task.Queued = "queued", true
	}
	s.signalLocked()
	s.mu.Unlock()
	s.scheduleReady(args.PlanID)
	return toolResult(s.planStatus(args.PlanID))
}

func (s *Server) primaryComplete(_ context.Context, request *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	var args struct {
		PlanID string `json:"plan_id"`
		TaskID string `json:"task_id"`
	}
	if strictArguments(request, &args) != nil {
		return nil, errors.New("valid plan_id and task_id are required")
	}
	s.mu.Lock()
	plan := s.plans[args.PlanID]
	if plan == nil || plan.Tasks[args.TaskID] == nil || plan.Tasks[args.TaskID].Task.State != "primary" {
		s.mu.Unlock()
		return nil, errors.New("primary-owned task is unavailable")
	}
	plan.Tasks[args.TaskID].Task.State = string(session.StateCompleted)
	s.signalLocked()
	s.mu.Unlock()
	s.persistRuntimePlan(args.PlanID)
	s.scheduleReady(args.PlanID)
	return toolResult(map[string]any{"plan_id": args.PlanID, "task_id": args.TaskID, "state": session.StateCompleted})
}

func (s *Server) startTask(planID, taskID string, allowQueued bool) (string, error) {
	if s.Adapter == nil || s.Control == nil {
		return "", errors.New("parallel worker runtime is unavailable")
	}
	s.mu.Lock()
	plan := s.plans[planID]
	if plan == nil {
		s.mu.Unlock()
		return "", errors.New("plan is unavailable in this bridge process")
	}
	task := plan.Tasks[taskID]
	if task == nil || task.Task.State == "primary" {
		s.mu.Unlock()
		return "", errors.New("task is unavailable or owned by the primary")
	}
	if task.Task.State != "planned" && !(allowQueued && task.Task.State == "queued") {
		s.mu.Unlock()
		return "", errors.New("task was already dispatched")
	}
	if !s.dependenciesCompleteLocked(plan, task.Task.Dependencies) {
		s.mu.Unlock()
		return "", errors.New("task dependencies are not complete")
	}
	if s.activeWorkersLocked() >= s.maxWorkers() {
		s.mu.Unlock()
		return "", errors.New("session worker limit reached")
	}
	workerID, err := newWorkerID()
	if err != nil {
		s.mu.Unlock()
		return "", err
	}
	task.WorkerID, task.Task.State, task.Queued = workerID, "starting", false
	task.StartedAt = time.Now()
	plan.Workers[workerID] = taskID
	s.signalLocked()
	s.mu.Unlock()
	if err := s.appendWorker(task.Task, workerID); err != nil {
		s.mu.Lock()
		if current := s.plans[planID]; current != nil {
			if currentTask := current.Tasks[taskID]; currentTask != nil && currentTask.WorkerID == workerID {
				currentTask.WorkerID, currentTask.StartedAt = "", time.Time{}
				if allowQueued {
					currentTask.Task.State, currentTask.Queued = "queued", true
				} else {
					currentTask.Task.State = "planned"
				}
				delete(current.Workers, workerID)
			}
		}
		s.signalLocked()
		s.mu.Unlock()
		return "", err
	}
	go s.executeTask(planID, taskID, workerID)
	return workerID, nil
}

func (s *Server) executeTask(planID, taskID, workerID string) {
	s.mu.Lock()
	plan := s.plans[planID]
	task := plan.Tasks[taskID]
	value := task.Task
	ctx := s.runCtx
	if ctx == nil {
		ctx = context.Background()
	}
	workerCtx, cancel := context.WithCancel(ctx)
	s.cancels[workerID] = cancel
	s.mu.Unlock()

	taskLifecycle, err := s.Control.RegisterLifecycle(workerCtx, "worker", workerID)
	if err != nil {
		s.completeTask(planID, taskID, workerID, session.StateFailed, 1, workers.Result{}, false, err)
		return
	}
	_, _ = s.Store.Update(s.SessionID, func(current *session.Session) error {
		if worker := findWorker(current, workerID); worker != nil {
			worker.RufloTaskID = taskLifecycle
		}
		return nil
	})
	brief := ""
	if loaded, loadErr := s.Store.LoadBrief(s.SessionID); loadErr == nil {
		if encoded, encodeErr := json.Marshal(loaded); encodeErr == nil {
			brief = string(encoded)
		}
	}
	result, runErr := s.Adapter.Run(workerCtx, workers.Request{Executor: value.Profile.Provider, Task: value.Task, Model: value.Profile.Model, Effort: value.Profile.Effort, Profile: string(value.Tier), TaskWeight: value.CapabilityScore, SharedContextBrief: brief, ResultBudget: workers.ResultBudgetForTier(string(value.Tier)), Directory: s.Directory, Runtime: s.RuntimeDir}, func(observation workers.Observation) {
		s.mu.Lock()
		if currentPlan := s.plans[planID]; currentPlan != nil && currentPlan.Tasks[taskID] != nil {
			currentPlan.Tasks[taskID].Task.State = string(session.StateRunning)
			s.signalLocked()
		}
		s.mu.Unlock()
		_, _ = s.Store.Update(s.SessionID, func(current *session.Session) error {
			if worker := findWorker(current, workerID); worker != nil {
				worker.PID, worker.ProcessStart, worker.HeadroomUsed, worker.State = observation.PID, observation.ProcessStart, observation.HeadroomUsed, session.StateRunning
			}
			return nil
		})
		s.persistRuntimePlan(planID)
	})
	cancel()
	_ = s.Control.CancelLifecycle(context.Background(), taskLifecycle)
	state, exitCode := session.StateCompleted, result.ExitCode
	if runErr != nil {
		state = session.StateFailed
		if exitCode == 0 {
			exitCode = 1
		}
	}
	s.completeTask(planID, taskID, workerID, state, exitCode, result, result.HeadroomUsed, runErr)
}

func (s *Server) completeTask(planID, taskID, workerID string, state session.State, exitCode int, raw workers.Result, headroom bool, runErr error) {
	projected := s.projectWorkerResult(taskID, workerID, state, raw, runErr, workingcontext.ContextBudgetForTier(string(rawTier(s, planID, taskID))))
	s.finish(workerID, state, exitCode, projected)
	s.mu.Lock()
	delete(s.cancels, workerID)
	if plan := s.plans[planID]; plan != nil {
		if task := plan.Tasks[taskID]; task != nil {
			task.Task.State = string(state)
			task.Task.HeadroomUsed = headroom
			if !task.StartedAt.IsZero() {
				task.Task.DurationMilliseconds = time.Since(task.StartedAt).Milliseconds()
			}
			task.Result = workerResult{Result: projected, State: string(state), ExitCode: exitCode}
			if runErr != nil {
				task.Task.EscalationReason = boundedDiagnostic(runErr.Error())
			}
		}
	}
	s.signalLocked()
	s.mu.Unlock()
	s.persistRuntimePlan(planID)
	s.scheduleReady(planID)
	s.mu.Lock()
	if plan := s.plans[planID]; plan != nil {
		if task := plan.Tasks[taskID]; task != nil && task.WorkerID == workerID {
			task.Settled = true
		}
	}
	s.signalLocked()
	s.mu.Unlock()
}

func rawTier(s *Server, planID, taskID string) routing.Tier {
	s.mu.Lock()
	defer s.mu.Unlock()
	if plan := s.plans[planID]; plan != nil && plan.Tasks[taskID] != nil {
		return plan.Tasks[taskID].Task.Tier
	}
	return routing.TierBalanced
}

func boundedDiagnostic(value string) string {
	value = platform.Redact(value)
	value = strings.ReplaceAll(strings.ReplaceAll(value, "\n", " "), "\r", " ")
	if len(value) > 512 {
		value = value[:512]
	}
	return value
}

func (s *Server) wait(ctx context.Context, request *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	var args struct {
		PlanID         string   `json:"plan_id"`
		TaskIDs        []string `json:"task_ids"`
		Mode           string   `json:"mode"`
		TimeoutSeconds int      `json:"timeout_seconds"`
	}
	if strictArguments(request, &args) != nil || len(args.TaskIDs) == 0 || (args.Mode != "any" && args.Mode != "all") {
		return nil, errors.New("valid wait parameters are required")
	}
	if args.TimeoutSeconds == 0 {
		args.TimeoutSeconds = 120
	}
	if args.TimeoutSeconds < 1 || args.TimeoutSeconds > 300 {
		return nil, errors.New("wait timeout must be between 1 and 300 seconds")
	}
	timer := time.NewTimer(time.Duration(args.TimeoutSeconds) * time.Second)
	defer timer.Stop()
	for {
		s.mu.Lock()
		ready, err := s.waitReadyLocked(args.PlanID, args.TaskIDs, args.Mode)
		notify := s.notify
		s.mu.Unlock()
		if err != nil {
			return nil, err
		}
		if ready {
			return toolResult(s.planStatus(args.PlanID))
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-timer.C:
			return toolResult(map[string]any{"timed_out": true, "plan": s.planStatus(args.PlanID)})
		case <-notify:
		}
	}
}

func (s *Server) escalate(ctx context.Context, request *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if !s.ProgressiveEscalation {
		return nil, errors.New("progressive escalation is disabled")
	}
	var args struct {
		PlanID string `json:"plan_id"`
		TaskID string `json:"task_id"`
		Reason string `json:"reason"`
	}
	if strictArguments(request, &args) != nil || len(args.Reason) < 3 || len(args.Reason) > 1024 {
		return nil, errors.New("valid plan_id, task_id and bounded reason are required")
	}
	s.mu.Lock()
	plan := s.plans[args.PlanID]
	if plan == nil || plan.Tasks[args.TaskID] == nil {
		s.mu.Unlock()
		return nil, errors.New("planned task is unavailable")
	}
	task := plan.Tasks[args.TaskID]
	if task.Task.State != string(session.StateFailed) && task.Task.State != string(session.StateCompleted) {
		s.mu.Unlock()
		return nil, errors.New("only a completed or failed task can be escalated")
	}
	next := nextTier(task.Task.Tier)
	if next == task.Task.Tier {
		s.mu.Unlock()
		return nil, errors.New("task is already at MAX")
	}
	input := task.Task.TaskInput
	s.mu.Unlock()
	profile, err := s.resolveProfile(ctx, input, next)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	task.Task.Tier, task.Task.Profile, task.Task.State, task.Task.EscalationCount, task.Task.EscalationReason = next, profile, "planned", task.Task.EscalationCount+1, args.Reason
	task.WorkerID, task.Result, task.Settled = "", workerResult{}, false
	s.signalLocked()
	s.mu.Unlock()
	_, _ = s.Store.Update(s.SessionID, func(value *session.Session) error { value.EscalationCount++; return nil })
	s.persistRuntimePlan(args.PlanID)
	return toolResult(map[string]any{"task_id": args.TaskID, "tier": next, "profile": profile, "state": "planned", "reason": args.Reason})
}

func nextTier(value routing.Tier) routing.Tier {
	switch value {
	case routing.TierLight:
		return routing.TierBalanced
	case routing.TierBalanced:
		return routing.TierStrong
	case routing.TierStrong:
		return routing.TierMax
	default:
		return routing.TierMax
	}
}

func (s *Server) scheduleReady(planID string) {
	for {
		s.mu.Lock()
		plan := s.plans[planID]
		if plan == nil || s.activeWorkersLocked() >= s.maxWorkers() {
			s.mu.Unlock()
			return
		}
		ready := ""
		for _, planned := range plan.Plan.Tasks {
			task := plan.Tasks[planned.ID]
			if task.Queued && task.Task.State == "queued" && s.dependenciesCompleteLocked(plan, task.Task.Dependencies) {
				ready = planned.ID
				break
			}
		}
		s.mu.Unlock()
		if ready == "" {
			return
		}
		if _, err := s.startTask(planID, ready, true); err != nil {
			return
		}
	}
}

func (s *Server) dependenciesCompleteLocked(plan *runtimePlan, dependencies []string) bool {
	for _, dependency := range dependencies {
		task := plan.Tasks[dependency]
		if task == nil || task.Task.State != string(session.StateCompleted) {
			return false
		}
	}
	return true
}

func (s *Server) activeWorkersLocked() int {
	active := 0
	for _, plan := range s.plans {
		for _, task := range plan.Tasks {
			if task.Task.State == "starting" || task.Task.State == "running" {
				active++
			}
		}
	}
	return active
}

func (s *Server) maxWorkers() int {
	if !s.Parallelism {
		return 1
	}
	value, err := s.Store.Get(s.SessionID)
	if err != nil || value.MaxWorkers < 1 {
		return 1
	}
	if value.MaxWorkers > 3 {
		return 3
	}
	return value.MaxWorkers
}

func (s *Server) appendWorker(task routing.Task, workerID string) error {
	started := time.Now().UTC()
	_, err := s.Store.Update(s.SessionID, func(value *session.Session) error {
		value.Workers = append(value.Workers, session.Worker{ID: workerID, TaskID: task.ID, Role: task.Role, RequestedExecutor: task.PreferredExecutor, Executor: task.Profile.Provider, Model: session.ModelInfo{Name: displayModel(task.Profile.Model), Source: session.ModelSource(task.Profile.ModelSource)}, State: session.StateStarting, StartedAt: started, Tier: string(task.Tier), CapabilityScore: task.CapabilityScore, Effort: task.Profile.Effort, EffortSource: string(task.Profile.EffortSource)})
		return session.AppendObservation(value, observability.Event{Category: observability.CategoryWorker, Operation: observability.OperationWorkerLifecycle, State: observability.StateRunning, TaskID: task.ID, WorkerID: workerID, Provider: task.Profile.Provider, Executor: task.Profile.Provider, Component: providerCoreComponent(task.Profile.Provider), RoutingReason: observability.ReasonCapabilityMatch})
	})
	return err
}

func displayModel(value string) string {
	if value == "" {
		return "client-default"
	}
	return value
}

func (s *Server) persistPlan(plan routing.Plan) error {
	metadata := make([]session.TaskMetadata, 0, len(plan.Tasks))
	for _, task := range plan.Tasks {
		metadata = append(metadata, taskMetadata(task))
	}
	_, err := s.Store.Update(s.SessionID, func(value *session.Session) error {
		newPlan := value.PlanID != plan.ID
		value.PlanID, value.OptimizationStrategy, value.Tasks, value.CurrentPhase = plan.ID, plan.Strategy, metadata, "parallel_dispatch"
		if !newPlan {
			return nil
		}
		if err := session.AppendObservation(value, observability.Event{Category: observability.CategoryDAG, Operation: observability.OperationDAGPlan, State: observability.StateCompleted, Component: core.ComponentOrchestration, RoutingReason: observability.ReasonPolicyAllowed}); err != nil {
			return err
		}
		for _, task := range plan.Tasks {
			if err := session.AppendObservation(value, observability.Event{Category: observability.CategoryCapability, Operation: observability.OperationCapabilityResolve, State: observability.StateSelected, TaskID: task.ID, Provider: task.Profile.Provider, Executor: task.Profile.Provider, Component: providerCoreComponent(task.Profile.Provider), RoutingReason: observability.ReasonCapabilityMatch}); err != nil {
				return err
			}
		}
		return nil
	})
	return err
}

func providerCoreComponent(provider string) core.ComponentID {
	if provider == "claude" {
		return core.ComponentClaude
	}
	return core.ComponentCodex
}

func (s *Server) persistRuntimePlan(planID string) {
	s.mu.Lock()
	plan := s.plans[planID]
	if plan == nil {
		s.mu.Unlock()
		return
	}
	copyPlan := plan.Plan
	copyPlan.Tasks = make([]routing.Task, 0, len(plan.Plan.Tasks))
	for _, original := range plan.Plan.Tasks {
		copyPlan.Tasks = append(copyPlan.Tasks, plan.Tasks[original.ID].Task)
	}
	s.mu.Unlock()
	_ = s.persistPlan(copyPlan)
}

func taskMetadata(task routing.Task) session.TaskMetadata {
	return session.TaskMetadata{ID: task.ID, Role: task.Role, Dependencies: append([]string(nil), task.Dependencies...), ParallelGroup: task.ParallelGroup, CapabilityScore: task.CapabilityScore, Tier: string(task.Tier), Executor: task.Profile.Provider, Model: session.ModelInfo{Name: displayModel(task.Profile.Model), Source: session.ModelSource(task.Profile.ModelSource)}, Effort: task.Profile.Effort, EffortSource: string(task.Profile.EffortSource), State: session.State(task.State), DurationMilliseconds: task.DurationMilliseconds, HeadroomUsed: task.HeadroomUsed, IntentionalRedundancy: task.IntentionalRedundancy, Escalations: task.EscalationCount, EscalationReason: task.EscalationReason, ExecutionMode: task.ExecutionMode, DelegationBenefit: task.DelegationBenefit, DelegationOverhead: task.DelegationOverhead, DelegationReason: task.DelegationReason}
}

func planMetadata(plan routing.Plan) map[string]any {
	tasks := make([]map[string]any, 0, len(plan.Tasks))
	for _, task := range plan.Tasks {
		tasks = append(tasks, map[string]any{"id": task.ID, "role": task.Role, "dependencies": task.Dependencies, "parallel_group": task.ParallelGroup, "capability_score": task.CapabilityScore, "tier": task.Tier, "execution_profile": task.Profile, "execution_mode": task.ExecutionMode, "delegation_benefit": task.DelegationBenefit, "delegation_overhead": task.DelegationOverhead, "delegation_reason": task.DelegationReason, "state": task.State, "intentional_redundancy": task.IntentionalRedundancy})
	}
	return map[string]any{"plan_id": plan.ID, "strategy": plan.Strategy, "tasks": tasks}
}

func (s *Server) planStatus(planID string) map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	plan := s.plans[planID]
	if plan == nil {
		return map[string]any{"plan_id": planID, "state": "unavailable"}
	}
	tasks := []map[string]any{}
	for _, original := range plan.Plan.Tasks {
		task := plan.Tasks[original.ID]
		tasks = append(tasks, map[string]any{"task_id": task.Task.ID, "worker_id": task.WorkerID, "state": task.Task.State, "tier": task.Task.Tier, "profile": task.Task.Profile, "execution_mode": task.Task.ExecutionMode})
	}
	return map[string]any{"plan_id": planID, "tasks": tasks}
}

func (s *Server) waitReadyLocked(planID string, ids []string, mode string) (bool, error) {
	plan := s.plans[planID]
	if plan == nil {
		return false, errors.New("plan is unavailable in this bridge process")
	}
	complete := 0
	for _, id := range ids {
		task := plan.Tasks[id]
		if task == nil {
			return false, fmt.Errorf("unknown task %q", id)
		}
		if (task.Task.State == string(session.StateCompleted) || task.Task.State == string(session.StateFailed)) && (task.Task.ExecutionMode == "primary" || task.Settled) {
			complete++
		}
	}
	if mode == "any" {
		return complete > 0, nil
	}
	return complete == len(ids), nil
}

func (s *Server) signalLocked() {
	close(s.notify)
	s.notify = make(chan struct{})
}

func newPlanID() (string, error) {
	id, err := session.NewID()
	if err != nil {
		return "", err
	}
	return "plan_" + strings.TrimPrefix(id, "sess_"), nil
}

func strictArguments(request *mcp.CallToolRequest, target any) error {
	if request == nil || request.Params == nil {
		return errors.New("tool arguments are required")
	}
	decoder := json.NewDecoder(bytes.NewReader(request.Params.Arguments))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("multiple argument values are not allowed")
		}
		return err
	}
	return nil
}
