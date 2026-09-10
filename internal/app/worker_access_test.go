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
	"github.com/ivo-lopes/ivoai/internal/secrets"
	"github.com/ivo-lopes/ivoai/internal/session"
	"github.com/ivo-lopes/ivoai/internal/workers"
)

func TestNativeWorkerProjectionHasIndependentSourceAndSkillScope(t *testing.T) {
	root := t.TempDir()
	for _, key := range []string{"XDG_CONFIG_HOME", "XDG_DATA_HOME", "XDG_STATE_HOME", "XDG_CACHE_HOME"} {
		t.Setenv(key, filepath.Join(root, key))
	}
	a, err := New("fixture", strings.NewReader(""), io.Discard, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	for _, alias := range []string{"company-a", "company-b"} {
		id := "srv_" + strings.ReplaceAll(alias, "-", "_")
		cfg.Connections.Servers[alias] = config.ServerProfile{ID: id, Alias: alias, Purpose: alias, URL: "http://127.0.0.1:1", ContextMCPURL: "http://127.0.0.1:1/context", Enabled: true, Status: "connected"}
		if err := (secrets.Store{Path: a.Store.Paths.Secrets}).Set(id, secrets.ClientCredential{Token: "fixture-upstream-" + alias}); err != nil {
			t.Fatal(err)
		}
	}
	store := session.Store{Root: a.Store.Paths.SessionsDir}
	id, _ := session.NewID()
	now := time.Now().UTC()
	v := session.Session{SessionID: id, StartedAt: now, UpdatedAt: now, Mode: session.ModeDirect, PrimaryExecutor: "codex", PrimaryModel: session.UnknownModel(), WorkingDirectory: root, MaxWorkers: 2, State: session.StateRunning, KnowledgeSources: []string{"company-a"}, MemoryStatus: "disabled", ContextStatus: "configured", ServerStatus: "connected"}
	if err := store.Create(v); err != nil {
		t.Fatal(err)
	}
	request := workers.Request{Executor: "codex", Directory: root, Runtime: root, Task: "bounded fixture"}
	for _, tc := range []struct {
		name                  string
		sources, mcps, skills []string
		denied                bool
	}{
		{name: "local worker"},
		{name: "selected source", sources: []string{"company-a"}, mcps: []string{"ivoai-context"}},
		{name: "cross-purpose", sources: []string{"company-b"}, mcps: []string{"ivoai-context"}, denied: true},
		{name: "implicit federation denied", mcps: []string{"ivoai-context"}, denied: true},
		{name: "unregistered external", mcps: []string{"plane"}, denied: true},
		{name: "control plane denied", mcps: []string{"ivoai-orchestrator"}, denied: true},
		{name: "missing required skill", skills: []string{"missing"}, denied: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			task := routing.Task{TaskInput: routing.TaskInput{ID: "fixture", Role: "research", KnowledgeSources: tc.sources, AllowedMCPs: tc.mcps, Skills: tc.skills}}
			got, err := a.prepareWorkerAccess(context.Background(), cfg, store, id, task, request)
			if (err != nil) != tc.denied {
				t.Fatalf("denied=%v error=%v", tc.denied, err)
			}
			if tc.denied {
				return
			}
			if got.Release == nil || got.Access == nil {
				t.Fatal("missing lifecycle or policy")
			}
			defer got.Release()
			write, grants := got.Access.Metadata()
			if write || len(grants) != len(tc.mcps) {
				t.Fatal("unrequested worker grant")
			}
			for _, name := range tc.mcps {
				if len(grants[name]) == 0 {
					t.Fatal("missing explicit capability")
				}
			}
			got.Release() // idempotent cancellation/normal cleanup
		})
	}
	if len(cfg.Connections.Servers) != 2 {
		t.Fatal("operator profiles changed")
	}
}
