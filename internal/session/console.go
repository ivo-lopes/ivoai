package session

import (
	"github.com/ivo-lopes/ivoai/internal/observability"
	"github.com/ivo-lopes/ivoai/internal/platform"
	"strings"
)

// ConsoleSnapshot is a projection of the existing core store, not another
// session manager. No objective, transcript, filesystem path, auth reference,
// checkpoint body or arbitrary diagnostic can be represented here.
type ConsoleSnapshot struct {
	Schema     int            `json:"schema"`
	SessionID  string         `json:"session_id"`
	Sequence   uint64         `json:"sequence"`
	Checkpoint bool           `json:"checkpoint"`
	PlanID     string         `json:"plan_id"`
	Tasks      []ConsoleTask  `json:"tasks"`
	Events     []ConsoleEvent `json:"events"`
}
type ConsoleTask struct {
	ID           string              `json:"id"`
	State        string              `json:"state"`
	Dependencies []string            `json:"dependencies"`
	Worktree     bool                `json:"worktree"`
	Integrated   bool                `json:"integrated"`
	MCPTools     map[string][]string `json:"mcp_tools"`
}
type ConsoleEvent struct {
	Sequence  uint64                  `json:"sequence"`
	Operation observability.Operation `json:"operation"`
	State     observability.State     `json:"state"`
	TaskID    string                  `json:"task_id,omitempty"`
}

func consoleLabel(s string) string {
	if len(s) > 128 || platform.Redact(s) != s {
		return "redacted"
	}
	for _, r := range s {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("._:-", r)) {
			return "redacted"
		}
	}
	return s
}

func (s Session) ConsoleSnapshot() ConsoleSnapshot {
	v := ConsoleSnapshot{Schema: 1, SessionID: consoleLabel(s.SessionID), Sequence: s.ObservationSequence, Checkpoint: s.CheckpointAvailable, PlanID: consoleLabel(s.PlanID), Tasks: []ConsoleTask{}, Events: []ConsoleEvent{}}
	if v.Sequence < uint64(len(s.Observability)) {
		v.Sequence = uint64(len(s.Observability))
	}
	start := 0
	if len(s.Observability) > MaxObservabilityEvents {
		start = len(s.Observability) - MaxObservabilityEvents
	}
	for i, e := range s.Observability {
		if i < start || e.Validate() != nil {
			continue
		}
		v.Events = append(v.Events, ConsoleEvent{Sequence: v.Sequence - uint64(len(s.Observability)) + uint64(i) + 1, Operation: e.Operation, State: e.State, TaskID: consoleLabel(e.TaskID)})
	}
	for i, t := range s.Tasks {
		if i >= 128 {
			break
		}
		task := ConsoleTask{ID: consoleLabel(t.ID), State: consoleLabel(string(t.State)), Dependencies: []string{}, MCPTools: map[string][]string{}}
		for j, d := range t.Dependencies {
			if j >= 128 {
				break
			}
			task.Dependencies = append(task.Dependencies, consoleLabel(d))
		}
		for server, tools := range t.AllowedMCPTools {
			if len(task.MCPTools) >= 32 {
				break
			}
			selected := []string{}
			for j, tool := range tools {
				if j >= 64 {
					break
				}
				selected = append(selected, consoleLabel(tool))
			}
			task.MCPTools[consoleLabel(server)] = selected
		}
		for _, w := range s.Workers {
			if w.TaskID == t.ID {
				task.Worktree = task.Worktree || w.WorktreePath != ""
				task.Integrated = task.Integrated || w.WorktreeIntegrated
			}
		}
		v.Tasks = append(v.Tasks, task)
	}
	return v
}

// Called inside the existing store transaction. Events describe state changes,
// not copies of input/output. Existing scheduler-specific events remain intact.
func appendConsoleTransitions(v *Session, state State, frontend, model string) error {
	appendEvent := func(category observability.Category, operation observability.Operation, state observability.State) error {
		return AppendObservation(v, observability.Event{Category: category, Operation: operation, State: state})
	}
	if state != v.State {
		observed := observability.StatePending
		switch v.State {
		case StateRunning, StateStarting:
			observed = observability.StateRunning
		case StateCompleted:
			observed = observability.StateCompleted
		case StateFailed:
			observed = observability.StateFailed
		case StateDegraded:
			observed = observability.StateDegraded
		case StateBlocked:
			observed = observability.StateBlocked
		}
		if err := appendEvent(observability.CategoryOrchestration, observability.OperationSessionLifecycle, observed); err != nil {
			return err
		}
	}
	if frontend != v.Frontend {
		if err := appendEvent(observability.CategoryOrchestration, observability.OperationFrontendSwitch, observability.StateSelected); err != nil {
			return err
		}
	}
	if model != v.EffectiveModel && v.EffectiveModel != "" {
		if err := appendEvent(observability.CategoryExecutor, observability.OperationModelSelect, observability.StateSelected); err != nil {
			return err
		}
	}
	return nil
}

func consoleWorktreeCounts(v Session) (trees, integrated int) {
	for _, w := range v.Workers {
		if w.WorktreePath != "" {
			trees++
		}
		if w.WorktreeIntegrated {
			integrated++
		}
	}
	return
}
func appendWorktreeTransitions(v *Session, trees, integrated int) error {
	nextTrees, nextIntegrated := consoleWorktreeCounts(*v)
	if nextTrees > trees {
		if err := AppendObservation(v, observability.Event{Category: observability.CategoryWorker, Operation: observability.OperationWorktreeLifecycle, State: observability.StateRunning}); err != nil {
			return err
		}
	}
	if nextIntegrated > integrated {
		return AppendObservation(v, observability.Event{Category: observability.CategoryWorker, Operation: observability.OperationWorktreeLifecycle, State: observability.StateCompleted})
	}
	return nil
}
