package app

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ivo-lopes/ivoai/internal/config"
	"github.com/ivo-lopes/ivoai/internal/routing"
	"github.com/ivo-lopes/ivoai/internal/session"
	"github.com/ivo-lopes/ivoai/internal/workers"
)

func TestClaudeRedDiagnosticsExplainIndividualPolicyAndRejections(t *testing.T) {
	a := nativeCapabilityTestApp(t)
	var out bytes.Buffer
	a.Out = &out
	if err := a.NativeCapabilityAction(context.Background(), "update", "claude-red"); err != nil {
		t.Fatal(err)
	}
	if err := a.PrintNativeCapabilities(context.Background(), "claude-red"); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"claude-red-offensive-reporting", "capabilities=filesystem.read", "missing_frontmatter", "policy=ALLOW", "policy=DENY", "executors=codex,claude", "MIT"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("diagnostic missing %q", want)
		}
	}
	for _, executor := range []string{"codex", "claude"} {
		result, err := a.evaluateSessionSkills(context.Background(), executor, t.TempDir(), []string{"authorized security report"})
		if err != nil || len(result.Selected) != 0 {
			t.Fatal("native body broadcast into direct/primary", err)
		}
	}
}

func TestClaudeRedWorkerScopeAndDisabledSource(t *testing.T) {
	a := nativeCapabilityTestApp(t)
	ctx := context.Background()
	if err := a.NativeCapabilityAction(ctx, "update", ""); err != nil {
		t.Fatal(err)
	}
	store := session.Store{Root: a.Store.Paths.SessionsDir}
	id, err := session.NewID()
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	root := t.TempDir()
	if err := store.Create(session.Session{SessionID: id, StartedAt: now, UpdatedAt: now, Mode: session.ModeOrchestrated, SwarmID: "fixture-swarm", PrimaryExecutor: "codex", PrimaryModel: session.UnknownModel(), WorkingDirectory: root, MaxWorkers: 2, State: session.StateRunning, MemoryStatus: "disabled", ContextStatus: "disabled", ServerStatus: "not-connected"}); err != nil {
		t.Fatal(err)
	}
	for _, executor := range []string{"codex", "claude"} {
		for _, disabled := range []bool{false, true} {
			cfg := config.Default()
			cfg.Skills.Ponytail = "off"
			cfg.Skills.Sources = map[string]config.SkillSourcePolicy{"claude-red": {Disabled: disabled}}
			task := routing.Task{TaskInput: routing.TaskInput{ID: "report", Role: "security", Task: "authorized security report from synthetic redacted findings", Acceptance: []string{"scope and limitations; no operational testing"}}}
			request, err := a.prepareWorkerAccess(ctx, cfg, store, id, task, workers.Request{Executor: executor, Directory: root, Runtime: filepath.Join(root, "runtime"), Task: task.Task})
			if err != nil {
				t.Fatal(err)
			}
			if disabled && len(request.SelectedSkills) != 0 || !disabled && (len(request.SelectedSkills) != 1 || request.SelectedSkills[0] != "claude-red-offensive-reporting") {
				t.Fatal("source preference or selection bypassed")
			}
			write, mcps := request.Access.Metadata()
			if write || len(mcps) != 0 {
				t.Fatal("skill escalated worker access")
			}
			request.Release()
		}
	}
}
