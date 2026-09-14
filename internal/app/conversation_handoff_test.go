package app

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ivo-lopes/ivoai/internal/config"
	"github.com/ivo-lopes/ivoai/internal/opencodebridge"
	"github.com/ivo-lopes/ivoai/internal/promptgate"
	"github.com/ivo-lopes/ivoai/internal/quota"
	"github.com/ivo-lopes/ivoai/internal/session"
)

func TestConversationHandoffExplicitBothProvidersAndNativeResume(t *testing.T) {
	for _, sourceProvider := range []string{"codex", "claude"} {
		t.Run(sourceProvider, func(t *testing.T) {
			root := t.TempDir()
			marker := filepath.Join(root, "provider-arguments")
			script := "#!/bin/sh\nprintf '%s\\n' \"$@\" > " + shellArgument(marker) + "\n"
			a := sessionTestApp(t, root, appExecutable(t, root, "codex", script), appExecutable(t, root, "claude", script), appExecutable(t, root, "ruflo", "#!/bin/sh\nexit 0\n"))
			store := session.Store{Root: a.Store.Paths.SessionsDir}
			id, _ := session.NewID()
			native, _ := session.NewNativeUUID()
			now := time.Now().UTC()
			source := session.Session{SessionID: id, StartedAt: now, UpdatedAt: now, Mode: session.ModeDirect, State: session.StateCompleted, PrimaryExecutor: sourceProvider, ExecutorSessionID: native, WorkingDirectory: root, PrimaryModel: session.UnknownModel(), MaxWorkers: 2, ContextStatus: "disabled", MemoryStatus: "disabled", ServerStatus: "not-connected"}
			if err := store.Create(source); err != nil {
				t.Fatal(err)
			}
			if err := store.SaveCheckpoint(id, session.Checkpoint{Objective: "Finish fixture documentation", Completed: []string{"fixture inspected"}, Acceptance: []string{"documentation describes fixture"}}); err != nil {
				t.Fatal(err)
			}
			to := "claude"
			if sourceProvider == "claude" {
				to = "codex"
			}
			if err := a.SessionHandoff(context.Background(), id, to, false); err == nil {
				t.Fatal("silent handoff")
			}
			if _, err := os.Stat(marker); !os.IsNotExist(err) {
				t.Fatal("provider ran before confirmation")
			}
			if err := a.SessionHandoff(context.Background(), id, to, true); err != nil {
				t.Fatal(err)
			}
			values, err := store.List()
			if err != nil || len(values) != 2 {
				t.Fatal("handoff family missing", err)
			}
			var destination session.Session
			for _, v := range values {
				if v.SessionID != id {
					destination = v
				}
			}
			if destination.Mode != session.ModeDirect || destination.PrimaryExecutor != to || destination.Lineage == nil || destination.Lineage.SourceSession != id || destination.ExecutorSessionID == native {
				t.Fatal("handoff confused with resume")
			}
			body, _ := json.Marshal(destination)
			if strings.Contains(string(body), "Finish fixture documentation") || strings.Contains(string(body), "Bounded HandoffBrief") {
				t.Fatal("prompt entered session metadata")
			}
			if err := a.SessionResume(context.Background(), id); err != nil {
				t.Fatal(err)
			}
			args, _ := os.ReadFile(marker)
			flag := "resume\n"
			if sourceProvider == "claude" {
				flag = "--resume\n"
			}
			if !strings.Contains(string(args), flag+native) {
				t.Fatal("same-provider resume not native")
			}
			values, _ = store.List()
			if len(values) != 2 {
				t.Fatal("resume duplicated logical session")
			}
		})
	}
}

func TestConversationRecoveryPromptRemainsStructured(t *testing.T) {
	for _, prompt := range []string{recoveryPrompt, handoffInput(session.HandoffBrief{Objective: "Finish fixture report", Acceptance: []string{"return the verified result"}})} {
		if result := promptgate.Assess(prompt); !result.Ready {
			t.Fatalf("control input failed normal admission: %v", result.Missing)
		}
	}
}

func TestConversationOpenCodeHandoffIsExplicitNewNativeDomain(t *testing.T) {
	for _, sourceProvider := range []string{"codex", "opencode"} {
		t.Run(sourceProvider, func(t *testing.T) {
			root := t.TempDir()
			marker := filepath.Join(root, "destination-args")
			script := "#!/bin/sh\nprintf '%s\\n' \"$@\" > " + shellArgument(marker) + "\n"
			a := sessionTestApp(t, root, appExecutable(t, root, "codex", script), appExecutable(t, root, "claude", script), appExecutable(t, root, "ruflo", "#!/bin/sh\nexit 0\n"))
			state, _ := a.Store.LoadState()
			state.Components["opencode"] = config.ComponentState{Installed: true, Managed: true, Version: "fixture", Path: appExecutable(t, root, "opencode", script)}
			if err := a.Store.SaveState(state); err != nil {
				t.Fatal(err)
			}
			store := session.Store{Root: a.Store.Paths.SessionsDir}
			id, _ := session.NewID()
			native, _ := session.NewNativeUUID()
			now := time.Now().UTC()
			if err := store.Create(session.Session{SessionID: id, StartedAt: now, UpdatedAt: now, Mode: session.ModeDirect, State: session.StateCompleted, PrimaryExecutor: sourceProvider, ExecutorSessionID: native, WorkingDirectory: root, PrimaryModel: session.UnknownModel(), MaxWorkers: 2, ContextStatus: "disabled", MemoryStatus: "disabled", ServerStatus: "not-connected"}); err != nil {
				t.Fatal(err)
			}
			if err := store.SaveCheckpoint(id, session.Checkpoint{Objective: "Finish fixture documentation", Acceptance: []string{"Document bounded handoff"}}); err != nil {
				t.Fatal(err)
			}
			to := "opencode"
			if sourceProvider == "opencode" {
				to = "codex"
			}
			if err := a.SessionHandoffDestination(context.Background(), id, to, "direct", "", false); err == nil {
				t.Fatal("handoff not confirmed")
			}
			if _, err := os.Stat(marker); !os.IsNotExist(err) {
				t.Fatal("provider ran before confirmation")
			}
			if err := a.SessionHandoffDestination(context.Background(), id, to, "direct", "", true); err != nil {
				t.Fatal(err)
			}
			values, err := store.List()
			if err != nil || len(values) != 2 {
				t.Fatal("missing explicit lineage", err)
			}
			for _, v := range values {
				if v.SessionID == id {
					continue
				}
				if v.Mode != session.ModeDirect || v.PrimaryExecutor != to || v.ExecutorSessionID == native || v.Lineage == nil || v.Lineage.SourceSession != id {
					t.Fatal("handoff pretended same-native resume")
				}
			}
			args, _ := os.ReadFile(marker)
			if to == "opencode" && !strings.Contains(string(args), "--prompt\nObjective:") {
				t.Fatal("OpenCode handoff prompt treated as project path")
			}
		})
	}
}

func TestConversationOpenCodeDirectOrchestratedHandoffClassification(t *testing.T) {
	for _, c := range []struct {
		from, to          string
		mode, destination session.Mode
		frontend          string
	}{
		{"opencode", "codex", session.ModeDirect, session.ModeAuto, "opencode"},
		{"codex", "opencode", session.ModeAuto, session.ModeDirect, ""},
	} {
		t.Run(c.from+"_to_"+c.to, func(t *testing.T) {
			root := t.TempDir()
			a := autoTestApp(t, root, "#!/bin/sh\ntrue\n", "#!/bin/sh\ntrue\n")
			a.QuotaManager = &quota.Manager{Store: quota.Store{Root: a.Store.Paths.QuotaDir}, Probes: map[quota.Provider]quota.Probe{
				quota.ProviderCodex:  probeFunc(func(context.Context) (quota.ProviderQuota, error) { return available(quota.ProviderCodex), nil }),
				quota.ProviderClaude: probeFunc(func(context.Context) (quota.ProviderQuota, error) { return available(quota.ProviderClaude), nil }),
			}}
			a.StartOpenCodeManaged = func(_ context.Context, options opencodebridge.ManagedOptions) (managedOpenCodeFrontend, error) {
				return handoffManagedFixture{fakeManagedOpenCode: fakeManagedOpenCode{environment: options.Environment}, submit: func(ctx context.Context, _, _, prompt string) error {
					if !promptgate.Assess(prompt).Ready || !strings.Contains(prompt, "Bounded HandoffBrief") {
						t.Fatal("handoff lost bounded structured admission")
					}
					_, err := options.Bridge.SubmitTurn(ctx, "oc_fixture", "handoff_fixture", prompt, "auto", "")
					return err
				}}, nil
			}
			store := session.Store{Root: a.Store.Paths.SessionsDir}
			id, _ := session.NewID()
			native, _ := session.NewNativeUUID()
			now := time.Now().UTC()
			source := session.Session{SessionID: id, StartedAt: now, UpdatedAt: now, Mode: c.mode, State: session.StateCompleted, PrimaryExecutor: c.from, ExecutorSessionID: native, WorkingDirectory: root, PrimaryModel: session.UnknownModel(), MaxWorkers: 2, ContextStatus: "disabled", MemoryStatus: "disabled", ServerStatus: "not-connected"}
			if c.mode == session.ModeAuto {
				source.Auto = true
				source.Coordinator = "native"
				source.InitialPlanner = c.from
				source.CurrentPrimary = c.from
				source.SwarmID = "native_" + id
				source.SetFrontend("opencode", "ses_fixture")
			}
			if err := store.Create(source); err != nil {
				t.Fatal(err)
			}
			if err := store.SaveCheckpoint(id, session.Checkpoint{Objective: "Finish fixture documentation", Acceptance: []string{"Document the explicit handoff"}}); err != nil {
				t.Fatal(err)
			}
			if err := a.SessionHandoffDestination(context.Background(), id, c.to, string(c.destination), c.frontend, true); err != nil {
				t.Fatal(err)
			}
			values, err := store.List()
			if err != nil || len(values) != 2 {
				t.Fatal("missing handoff lineage", err)
			}
			for _, v := range values {
				if v.SessionID == id {
					continue
				}
				if v.Mode != c.destination || v.PrimaryExecutor != c.to || v.Lineage == nil || v.Lineage.SourceSession != id || v.ExecutorSessionID == native {
					t.Fatal("provider-changing handoff misclassified as native resume")
				}
				if c.frontend != "" && v.Frontend != c.frontend {
					t.Fatal("destination frontend lost")
				}
			}
		})
	}
}

type handoffManagedFixture struct {
	fakeManagedOpenCode
	submit func(context.Context, string, string, string) error
}

func (f handoffManagedFixture) SubmitPrompt(ctx context.Context, id, cwd, prompt string) error {
	return f.submit(ctx, id, cwd, prompt)
}
