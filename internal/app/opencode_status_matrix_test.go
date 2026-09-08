package app

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/ivo-lopes/ivoai/internal/config"
	"github.com/ivo-lopes/ivoai/internal/knowledgerouter"
	"github.com/ivo-lopes/ivoai/internal/quota"
	"github.com/ivo-lopes/ivoai/internal/serverpool"
	"github.com/ivo-lopes/ivoai/internal/session"
)

func TestOpenCodeServerStatusMatrixUsesObservedHealth(t *testing.T) {
	for count := 0; count <= 3; count++ {
		for _, restricted := range []bool{false, true} {
			t.Run(fmt.Sprintf("count%d/restricted%t", count, restricted), func(t *testing.T) {
				cfg := config.Default()
				cfg.OpenCode.PermissionMode = "full"
				knowledge := sessionKnowledge{healthMu: &sync.RWMutex{}, health: map[string]string{}}
				for i := 0; i < count; i++ {
					alias := fmt.Sprintf("source-%d", i)
					p := config.ServerProfile{ID: fmt.Sprintf("srv_%d", i), Alias: alias, Purpose: alias, Enabled: true, Status: "connected"}
					cfg.Connections.Servers[alias] = p
					knowledge.health[alias] = "healthy"
					if i == 1 {
						knowledge.health[alias] = "down"
					}
					if !restricted || i == 0 {
						knowledge.selection.Groups = append(knowledge.selection.Groups, serverpool.SourceGroup{Profiles: []config.ServerProfile{p}})
					}
				}
				router, err := knowledgerouter.Start(knowledgerouter.Options{Selection: knowledge.selection})
				if err != nil {
					t.Fatal(err)
				}
				defer router.Close(context.Background())
				knowledge.router = router
				a := &App{Version: "test"}
				status := a.openCodeAutoStatus(session.Store{Root: filepath.Join(t.TempDir(), "sessions")}, "missing", cfg, knowledge, restricted, map[quota.Provider]quota.ProviderQuota{}, sharedKnowledgeCompressionPolicy{}, nil)
				if status.ConfiguredCount != count || status.PermissionMode != "full" {
					t.Fatal("configuration projection drift")
				}
				connected := count
				if count > 1 {
					connected--
				}
				if status.ConnectedCount != connected {
					t.Fatal("observed health drift", status.ConnectedCount)
				}
				for i, s := range status.Servers {
					if s.Selected != (!restricted || i == 0) {
						t.Fatal("scope drift")
					}
					if s.Health == "down" && s.AuthState == "authenticated" {
						t.Fatal("unverified authentication promoted")
					}
				}
				knowledge.health = map[string]string{}
				status = a.openCodeAutoStatus(session.Store{}, "missing", cfg, knowledge, restricted, nil, sharedKnowledgeCompressionPolicy{}, nil)
				if status.ConnectedCount != 0 {
					t.Fatal("persisted connected status treated as live health")
				}
			})
		}
	}
}

func TestOpenCodeStatusDoesNotPromoteStaleEvidence(t *testing.T) {
	knowledge := sessionKnowledge{healthMu: &sync.RWMutex{}, health: map[string]string{"A": "healthy"}, healthObserved: map[string]time.Time{"A": time.Now().Add(-2 * time.Minute)}}
	if got := knowledge.healthFor("A", "unknown"); got != "stale" {
		t.Fatal("stale upstream observation promoted", got)
	}
	quotas := map[quota.Provider]quota.ProviderQuota{quota.ProviderCodex: available(quota.ProviderCodex), quota.ProviderClaude: available(quota.ProviderClaude)}
	probeErrors := map[quota.Provider]error{quota.ProviderCodex: errors.New("fixture probe failed"), quota.ProviderClaude: errors.New("fixture probe failed")}
	a := &App{}
	status := a.openCodeAutoStatus(session.Store{}, "missing", config.Default(), knowledge, false, quotas, sharedKnowledgeCompressionPolicy{}, probeErrors)
	if status.CodexAuth != "stale / not verified" || status.ClaudeAuth != "stale / not verified" || status.CodexQuota != "N/A" || status.ClaudeQuota != "N/A" {
		t.Fatal("failed auth/quota probes promoted cached evidence")
	}
}
