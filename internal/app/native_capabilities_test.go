package app

import (
	"context"
	"io"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ivo-lopes/ivoai/internal/config"
	"github.com/ivo-lopes/ivoai/internal/routing"
	"github.com/ivo-lopes/ivoai/internal/session"
	"github.com/ivo-lopes/ivoai/internal/skillcatalog"
	"github.com/ivo-lopes/ivoai/internal/skills"
	"github.com/ivo-lopes/ivoai/internal/workers"
)

func nativeCapabilityTestApp(t *testing.T) *App {
	t.Helper()
	root := t.TempDir()
	for _, key := range []string{"XDG_CONFIG_HOME", "XDG_DATA_HOME", "XDG_STATE_HOME", "XDG_CACHE_HOME"} {
		t.Setenv(key, filepath.Join(root, key))
	}
	a, err := New("fixture", strings.NewReader(""), io.Discard, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func TestNativeCapabilitiesPersistentSelectiveManagement(t *testing.T) {
	a := nativeCapabilityTestApp(t)
	ctx := context.Background()
	if err := a.NativeCapabilityAction(ctx, "update", ""); err != nil {
		t.Fatal(err)
	}
	rows, err := a.NativeCapabilities(ctx)
	if err != nil || len(rows) != 13 {
		t.Fatal("pack unavailable", err)
	}
	for _, row := range rows {
		if row.Status != "ready" {
			t.Fatalf("source %s: %s", row.ID, row.Status)
		}
		if row.ID == "i-have-adhd" && row.AutoSelect != "manual task request only" {
			t.Fatal("interaction profile advertised automatic selection")
		}
		if row.ID == "ponytail" && row.AutoSelect != "implementation only; policy gated" {
			t.Fatal("Ponytail auto scope missing from metadata")
		}
	}
	for _, action := range []string{"disable", "pin"} {
		if err := a.NativeCapabilityAction(ctx, action, "ponytail"); err != nil {
			t.Fatal(err)
		}
	}
	cfg, err := a.Store.Load()
	if err != nil || !cfg.Skills.Sources["ponytail"].Disabled || !cfg.Skills.Sources["ponytail"].Pinned || cfg.Skills.Sources["caveman-skills"].Disabled {
		t.Fatal("source isolation failed", err)
	}
	if err := a.ConfigSet("skills.ponytail", "off"); err != nil {
		t.Fatal(err)
	}
	if err := a.ConfigSet("skills.ponytail", "unsafe"); err == nil {
		t.Fatal("invalid Ponytail mode accepted")
	}
	cfg, _ = a.Store.Load()
	if cfg.Skills.ResolvedPonytail() != "off" {
		t.Fatal("invalid setting overwrote valid preference")
	}
	for _, action := range []string{"enable", "unpin"} {
		if err := a.NativeCapabilityAction(ctx, action, "ponytail"); err != nil {
			t.Fatal(err)
		}
	}
	if err := a.NativeCapabilityAction(ctx, "update", ""); err != nil {
		t.Fatal("reapply failed", err)
	}
}

func TestNativeQuarantineReusesRegistryAndReapply(t *testing.T) {
	a := nativeCapabilityTestApp(t)
	if err := a.Store.Ensure(); err != nil {
		t.Fatal(err)
	}
	manager, err := a.nativeManager("ponytail")
	if err != nil {
		t.Fatal(err)
	}
	if err := quarantineMissingNative(manager, "ponytail"); err != nil {
		t.Fatal(err)
	}
	registry, err := manager.Registry.Load()
	if err != nil || len(registry.Entries) != 1 || registry.Entries[0].Provenance.Integrity.Verified || registry.Entries[0].QuarantineReason != "native_materialization_failed" {
		t.Fatal("invalid quarantine metadata", err)
	}
	rows, err := a.NativeCapabilities(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range rows {
		if row.ID == "ponytail" && row.Status != "quarantined" {
			t.Fatal("quarantine not visible")
		}
	}
	if err := a.NativeCapabilityAction(context.Background(), "update", "ponytail"); err != nil {
		t.Fatal("verified reapply failed", err)
	}
	if err := quarantineMissingNative(manager, "ponytail"); err != nil {
		t.Fatal(err)
	}
	if err := manager.ValidateConsistency(context.Background(), "ponytail"); err != nil {
		t.Fatal("quarantine overwrote valid active revision", err)
	}
}

func TestNativePackPreservesUnownedArtifactIDCollision(t *testing.T) {
	a := nativeCapabilityTestApp(t)
	if err := a.Store.Ensure(); err != nil {
		t.Fatal(err)
	}
	manager, err := a.nativeManager("ponytail")
	if err != nil {
		t.Fatal(err)
	}
	entries, err := skillcatalog.NativeQuarantine("ponytail")
	if err != nil {
		t.Fatal(err)
	}
	entries[0].ID = "personal-custom-profile"
	entries[0].Lifecycle = skills.LifecycleStaged
	entries[0].QuarantineReason = ""
	if err := manager.Registry.Save(skills.Registry{Schema: skills.RegistrySchemaVersion, Entries: entries}); err != nil {
		t.Fatal(err)
	}
	if err := a.NativeCapabilityAction(context.Background(), "update", "ponytail"); err == nil {
		t.Fatal("unowned artifact identity adopted")
	}
	after, err := manager.Registry.Load()
	if err != nil || len(after.Entries) != 1 || after.Entries[0].ID != entries[0].ID || after.Entries[0].Lifecycle != skills.LifecycleStaged {
		t.Fatal("personal entry mutated", err)
	}
}

func TestNativeWorkerPonytailScopeAndSharedRegistry(t *testing.T) {
	a := nativeCapabilityTestApp(t)
	ctx := context.Background()
	if err := a.NativeCapabilityAction(ctx, "update", ""); err != nil {
		t.Fatal(err)
	}
	store := session.Store{Root: a.Store.Paths.SessionsDir}
	id, _ := session.NewID()
	now := time.Now().UTC()
	root := t.TempDir()
	if err := store.Create(session.Session{SessionID: id, StartedAt: now, UpdatedAt: now, Mode: session.ModeDirect, PrimaryExecutor: "codex", PrimaryModel: session.UnknownModel(), WorkingDirectory: root, MaxWorkers: 2, State: session.StateRunning, MemoryStatus: "disabled", ContextStatus: "disabled", ServerStatus: "not-connected"}); err != nil {
		t.Fatal(err)
	}
	for _, executor := range []string{"codex", "claude"} {
		for _, tc := range []struct {
			role, mode string
			want       bool
			risk       int
		}{
			{"implementation", "auto", true, 10}, {"research", "auto", false, 10}, {"documentation", "auto", false, 10}, {"security", "auto", false, 10}, {"synthesis", "auto", false, 10}, {"implementation", "off", false, 10}, {"implementation", "on", true, 10}, {"implementation", "auto", false, 90},
		} {
			t.Run(executor+"/"+tc.role+"/"+tc.mode+"/"+fmtRisk(tc.risk), func(t *testing.T) {
				cfg := config.Default()
				cfg.Skills.Ponytail = tc.mode
				task := routing.Task{TaskInput: routing.TaskInput{ID: "task", Role: tc.role, Task: "bounded fixture change", Acceptance: []string{"fixture unchanged outside scope"}, Scores: routing.Scores{Risk: tc.risk}}}
				request, err := a.prepareWorkerAccess(ctx, cfg, store, id, task, workers.Request{Executor: executor, Directory: root, Runtime: root, Task: task.Task})
				if err != nil {
					t.Fatal(err)
				}
				defer request.Release()
				found := false
				for _, id := range request.SelectedSkills {
					if id == "ponytail" {
						found = true
					}
				}
				if found != tc.want {
					t.Fatalf("Ponytail selected=%t want=%t", found, tc.want)
				}
				if len(request.SelectedSkills) > 1 {
					t.Fatal("unrelated skill broadcast")
				}
				_, mcps := request.Access.Metadata()
				if len(mcps) != 0 {
					t.Fatal("skill granted MCPs")
				}
			})
		}
	}
}

func fmtRisk(value int) string {
	if value >= 70 {
		return "high-risk"
	}
	return "normal-risk"
}
