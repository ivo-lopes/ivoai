package app

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ivo-lopes/ivoai/internal/codeintel"
	"github.com/ivo-lopes/ivoai/internal/config"
	"github.com/ivo-lopes/ivoai/internal/routing"
	"github.com/ivo-lopes/ivoai/internal/session"
	"github.com/ivo-lopes/ivoai/internal/workingcontext"
)

func TestPrimaryCodeEvidenceUsesScopedExactArtifactAndAdmission(t *testing.T) {
	a := autoTestApp(t, t.TempDir(), "#!/bin/sh\nexit 0\n", "#!/bin/sh\nexit 0\n")
	root := t.TempDir()
	if err := exec.Command("git", "init", "-q", root).Run(); err != nil {
		t.Fatal(err)
	}
	source := "package fixture\nfunc Authorized() {}\n"
	if err := os.WriteFile(filepath.Join(root, "fixture.go"), []byte(source), 0600); err != nil {
		t.Fatal(err)
	}
	id, _ := session.NewID()
	now := time.Now().UTC()
	store := session.Store{Root: a.Store.Paths.SessionsDir}
	value := session.Session{SessionID: id, StartedAt: now, UpdatedAt: now, Mode: session.ModeAuto, Auto: true, CurrentPrimary: "codex", InitialPlanner: "codex", PrimaryExecutor: "codex", WorkingDirectory: root, KnowledgeScopeID: "cs_fixture", PrimaryModel: session.UnknownModel(), MaxWorkers: 2, State: session.StateRunning, Coordinator: "native", SwarmID: "native_" + id, MemoryStatus: "disabled", ContextStatus: "disabled", ServerStatus: "not-connected", PlanID: "plan_0123456789abcdef0123456789abcdef", Decisions: []session.Decision{{ID: "plan_0123456789abcdef0123456789abcdef", Kind: "plan", State: "approved", CreatedAt: time.Now().UTC(), ResolvedAt: func() *time.Time { now := time.Now().UTC(); return &now }()}}, Tasks: []session.TaskMetadata{{ID: "inspect", Role: "research", Tier: "LIGHT", Model: session.UnknownModel(), State: session.StatePrimary, ExecutionMode: "primary"}}}
	if err := store.Create(value); err != nil {
		t.Fatal(err)
	}
	task := routing.Task{TaskInput: routing.TaskInput{ID: "inspect", Role: "research", Task: "Inspect API fixture.go"}, ExecutionMode: "primary", State: "primary"}
	calls := 0
	factory := func(_ context.Context, directory, scope string) (*codeintel.Provider, error) {
		calls++
		if directory != root || scope != "cs_fixture" {
			t.Fatal("scope changed")
		}
		return &codeintel.Provider{Root: directory, Scope: scope, Permissions: "filesystem.read", Allowed: true}, nil
	}
	body, err := a.preparePrimaryCodeEvidence(context.Background(), store, id, task, 4094, factory)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "Authorized") || !strings.Contains(string(body), "exact_required") || !strings.Contains(string(body), `"untrusted":true`) || len(body) > 4094 {
		t.Fatalf("invalid evidence: %s", body)
	}
	var evidence struct {
		Artifact workingcontext.ArtifactRef `json:"artifact"`
	}
	if err := json.Unmarshal(body, &evidence); err != nil {
		t.Fatal(err)
	}
	artifacts, err := workingcontext.NewLocalStore(filepath.Join(a.Store.Paths.CacheDir, "working-context"), workingcontext.LocalOptions{})
	if err != nil {
		t.Fatal(err)
	}
	reader, _, err := artifacts.Read(context.Background(), workingcontext.Ownership{SessionID: id, TaskID: task.ID}, evidence.Artifact.ID)
	if err != nil {
		t.Fatal(err)
	}
	exact, _ := io.ReadAll(reader)
	reader.Close()
	if !strings.Contains(string(exact), source) {
		t.Fatal("exact artifact missing")
	}
	for _, change := range []func(*routing.Task){func(task *routing.Task) { task.Task = "Explain institutional policy" }, func(task *routing.Task) { task.AllowedMCPTools = map[string][]string{nativeCodeMCP: {}} }, func(task *routing.Task) { task.Skills = []string{"absent-required-skill"} }} {
		denied := task
		change(&denied)
		if len(denied.Skills) > 0 {
			if _, err := store.Update(id, func(v *session.Session) error { v.Tasks[0].Skills = denied.Skills; return nil }); err != nil {
				t.Fatal(err)
			}
		}
		before := calls
		body, _ := a.preparePrimaryCodeEvidence(context.Background(), store, id, denied, 4094, factory)
		if len(body) != 0 || calls != before {
			t.Fatal("unrelated/restricted task reached provider")
		}
		if _, err := store.Update(id, func(v *session.Session) error { v.Tasks[0].Skills = nil; return nil }); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := store.Update(id, func(v *session.Session) error { v.Tasks[0].ExecutionMode = "worker"; return nil }); err != nil {
		t.Fatal(err)
	}
	before := calls
	if body, _ := a.preparePrimaryCodeEvidence(context.Background(), store, id, task, 4094, factory); len(body) != 0 || calls != before {
		t.Fatal("worker duplicated in primary")
	}
}

func TestPrimaryCodeTaskAdmissionChecksExactTask(t *testing.T) {
	cfg := config.Default()
	task := routing.Task{TaskInput: routing.TaskInput{ID: "target"}}
	value := session.Session{State: session.StateRunning, PlanID: "plan_0123456789abcdef0123456789abcdef", Decisions: []session.Decision{{ID: "plan_0123456789abcdef0123456789abcdef", Kind: "plan", State: "approved", CreatedAt: time.Now().UTC(), ResolvedAt: func() *time.Time { now := time.Now().UTC(); return &now }()}}, Tasks: []session.TaskMetadata{{ID: "other", State: session.StatePrimary, ExecutionMode: "primary"}, {ID: "target", State: session.StatePrimary, ExecutionMode: "primary", Dependencies: []string{"missing"}}}}
	if primaryCodeTaskAdmitted(value, cfg, task) {
		t.Fatal("other runnable task granted target")
	}
	value.Tasks[1].Dependencies = nil
	if !primaryCodeTaskAdmitted(value, cfg, task) {
		t.Fatal("ready task denied")
	}
	value.Tasks[1].AllowedMCPTools = map[string][]string{nativeCodeMCP: {"graft_repo_map"}}
	if primaryCodeTaskAdmitted(value, cfg, task) {
		t.Fatal("explicit capability restriction bypassed")
	}
}
