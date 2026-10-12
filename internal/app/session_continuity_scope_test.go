package app

import (
	"testing"

	"github.com/ivo-lopes/ivoai/internal/config"
)

func TestConversationScopeTracksConfiguredOrganization(t *testing.T) {
	cfg := config.Default()
	cfg.Connections.Servers = map[string]config.ServerProfile{"alpha": {ID: "srv_alpha", URL: "https://alpha.example", Enabled: true, Status: "connected", Purpose: "alpha"}, "beta": {ID: "srv_beta", URL: "https://beta.example", Enabled: true, Status: "connected", Purpose: "beta"}}
	both, err := conversationScopeID("/fixture", cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	alpha, err := conversationScopeID("/fixture", cfg, []string{"alpha"})
	if err != nil {
		t.Fatal(err)
	}
	if both == alpha {
		t.Fatal("explicit organization selector ignored")
	}
	p := cfg.Connections.Servers["beta"]
	p.Enabled = false
	cfg.Connections.Servers["beta"] = p
	only, err := conversationScopeID("/fixture", cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	if only == both || only != alpha {
		t.Fatal("configured organizations not reflected in scope")
	}
	p = cfg.Connections.Servers["alpha"]
	p.Status = "offline"
	cfg.Connections.Servers["alpha"] = p
	offline, err := conversationScopeID("/fixture", cfg, nil)
	if err != nil || offline != only {
		t.Fatal("health transition changed organization identity", err)
	}
	p.URL = "https://different.example"
	cfg.Connections.Servers["alpha"] = p
	changed, err := conversationScopeID("/fixture", cfg, nil)
	if err != nil || changed == only {
		t.Fatal("changed endpoint reused native history", err)
	}
}

func TestConversationScopeRejectsUnverifiedHistory(t *testing.T) {
	for _, old := range []string{"", "ks_old", "cs_v1_other"} {
		if err := verifyConversationScope(old, "cs_v1_current"); err == nil {
			t.Fatalf("accepted %q", old)
		}
	}
	if err := verifyConversationScope("cs_v1_current", "cs_v1_current"); err != nil {
		t.Fatal(err)
	}
}

func TestConversationEmptySourcesRemainScopedToProject(t *testing.T) {
	cfg := config.Default()
	first, err := conversationScopeID("/first", cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	again, err := conversationScopeID("/first", cfg, nil)
	if err != nil || first == "" || first != again {
		t.Fatal("empty source scope is not stable", err)
	}
	second, err := conversationScopeID("/second", cfg, nil)
	if err != nil || first == second {
		t.Fatal("empty source scopes crossed projects", err)
	}
	if err := verifyConversationScope("", first); err == nil {
		t.Fatal("unknown history treated as known empty scope")
	}
}
