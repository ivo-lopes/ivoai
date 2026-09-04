package serverpool

import (
	"fmt"
	"testing"

	"github.com/ivo-lopes/ivoai/internal/config"
)

func profile(id, alias, purpose, group string, priority int) config.ServerProfile {
	return config.ServerProfile{ID: id, Alias: alias, URL: "https://" + alias + ".example.invalid", Purpose: purpose, RedundancyGroup: group, Priority: priority, Enabled: true, Status: "connected"}
}

func TestResolveSeparatesPurposeFromRedundancy(t *testing.T) {
	pool, err := New(map[string]config.ServerProfile{
		"voicecorp":  profile("srv_voicecorp", "voicecorp", "voicecorp", "", 10),
		"mindsite-2": profile("srv_mindsite2", "mindsite-2", "mindsite", "mindsite-production", 20),
		"mindsite-1": profile("srv_mindsite1", "mindsite-1", "mindsite", "mindsite-production", 10),
	})
	if err != nil {
		t.Fatal(err)
	}
	selection, err := pool.Resolve([]string{"mindsite", "voicecorp"})
	if err != nil {
		t.Fatal(err)
	}
	if len(selection.Groups) != 2 || selection.PurposeCount() != 2 {
		t.Fatalf("selection=%+v", selection)
	}
	for _, group := range selection.Groups {
		if group.Purpose == "mindsite" && (len(group.Profiles) != 2 || group.Profiles[0].Alias != "mindsite-1") {
			t.Fatalf("redundancy priority not deterministic: %+v", group)
		}
	}
	implicit, err := pool.Resolve(nil)
	if err != nil || len(implicit.Groups) != 2 || implicit.PurposeCount() != 2 {
		t.Fatalf("all-enabled selection=%+v err=%v", implicit, err)
	}
}

func TestOpaqueIdentitySurvivesAliasRename(t *testing.T) {
	pool, err := New(map[string]config.ServerProfile{"new-name": profile("srv_stable_identity", "new-name", "purpose", "", 0)})
	if err != nil {
		t.Fatal(err)
	}
	got, ok := pool.Get("new-name")
	if !ok || got.ID != "srv_stable_identity" {
		t.Fatalf("profile=%+v ok=%v", got, ok)
	}
}

func TestHostileAliasRejected(t *testing.T) {
	for _, alias := range []string{"../mindsite", "Mindsite", "A=B", "voice corp", "_hidden"} {
		if err := ValidateAlias(alias); err == nil {
			t.Fatalf("alias %q accepted", alias)
		}
	}
}

func TestResolveServerCountMatrix(t *testing.T) {
	empty, err := New(nil)
	if err != nil {
		t.Fatal(err)
	}
	selection, err := empty.Resolve(nil)
	if err != nil || len(selection.Groups) != 0 {
		t.Fatalf("zero servers selection=%+v err=%v", selection, err)
	}

	one, err := New(map[string]config.ServerProfile{
		"voicecorp": profile("srv_voicecorp", "voicecorp", "voicecorp", "", 10),
	})
	if err != nil {
		t.Fatal(err)
	}
	selection, err = one.Resolve(nil)
	if err != nil || len(selection.Groups) != 1 || selection.Groups[0].Purpose != "voicecorp" {
		t.Fatalf("one server selection=%+v err=%v", selection, err)
	}

	profiles := map[string]config.ServerProfile{
		"voicecorp": profile("srv_voicecorp", "voicecorp", "voicecorp", "", 10),
		"mindsite":  profile("srv_mindsite", "mindsite", "mindsite", "", 10),
	}
	two, err := New(profiles)
	if err != nil {
		t.Fatal(err)
	}
	selection, err = two.Resolve(nil)
	if err != nil || len(selection.Groups) != 2 {
		t.Fatalf("two enabled servers selection=%+v err=%v", selection, err)
	}
	selection, err = two.Resolve([]string{"voicecorp", "mindsite"})
	if err != nil || len(selection.Groups) != 2 {
		t.Fatalf("two explicit servers selection=%+v err=%v", selection, err)
	}

	profiles["research"] = profile("srv_research", "research", "research", "", 10)
	three, err := New(profiles)
	if err != nil {
		t.Fatal(err)
	}
	selection, err = three.Resolve(nil)
	if err != nil || len(selection.Groups) != 3 {
		t.Fatalf("three explicit servers selection=%+v err=%v", selection, err)
	}
	if selection.Groups[0].Purpose != "mindsite" || selection.Groups[1].Purpose != "research" || selection.Groups[2].Purpose != "voicecorp" {
		t.Fatalf("selection order is not deterministic: %+v", selection.Groups)
	}
}

func TestResolveWithoutSelectorsUsesOnlyEnabledConnectedProfiles(t *testing.T) {
	connected := profile("srv_connected", "connected", "connected", "", 10)
	disabled := profile("srv_disabled", "disabled", "disabled", "", 10)
	disabled.Enabled = false
	disconnected := profile("srv_disconnected", "disconnected", "disconnected", "", 10)
	disconnected.Status = "disconnected"
	pool, err := New(map[string]config.ServerProfile{
		"connected": connected, "disabled": disabled, "disconnected": disconnected,
	})
	if err != nil {
		t.Fatal(err)
	}
	selection, err := pool.Resolve(nil)
	if err != nil || len(selection.Groups) != 1 || selection.Groups[0].Purpose != "connected" {
		t.Fatalf("selection=%+v err=%v", selection, err)
	}
}

func TestExplicitSelectorRemainsRestrictiveWhenAllEnabledIsDefault(t *testing.T) {
	pool, err := New(map[string]config.ServerProfile{
		"company-a": profile("srv_company_a", "company-a", "company-a", "", 10),
		"company-b": profile("srv_company_b", "company-b", "company-b", "", 10),
		"company-c": profile("srv_company_c", "company-c", "company-c", "", 10),
	})
	if err != nil {
		t.Fatal(err)
	}
	selection, err := pool.Resolve([]string{"company-a", "company-c"})
	if err != nil || len(selection.Groups) != 2 {
		t.Fatalf("selection=%+v err=%v", selection, err)
	}
	for _, group := range selection.Groups {
		if group.Purpose == "company-b" {
			t.Fatal("unselected source leaked into restrictive selection")
		}
	}
}

func TestPoolRejectsCrossOriginAndURLUserinfo(t *testing.T) {
	base := profile("srv_voicecorp", "voicecorp", "voicecorp", "", 10)
	base.URL = "https://user:secret@voicecorp.example.invalid"
	if _, err := New(map[string]config.ServerProfile{"voicecorp": base}); err == nil {
		t.Fatal("URL userinfo accepted")
	}
	base.URL = "https://voicecorp.example.invalid"
	base.ContextMCPURL = "https://mindsite.example.invalid/mcp/context"
	if _, err := New(map[string]config.ServerProfile{"voicecorp": base}); err == nil {
		t.Fatal("cross-origin discovery endpoint accepted")
	}
}

func TestExplicitUnavailableSourceFails(t *testing.T) {
	for _, status := range []struct {
		enabled bool
		state   string
	}{{enabled: false, state: "connected"}, {enabled: true, state: "disconnected"}} {
		value := profile("srv_mindsite", "mindsite", "mindsite", "", 10)
		value.Enabled = status.enabled
		value.Status = status.state
		pool, err := New(map[string]config.ServerProfile{"mindsite": value})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Resolve([]string{"mindsite"}); err == nil {
			t.Fatalf("explicit unavailable source accepted: %+v", status)
		}
	}
}

func TestImplicitSelectionIsBounded(t *testing.T) {
	profiles := map[string]config.ServerProfile{}
	for index := 0; index <= MaxSelectedSources; index++ {
		alias := fmt.Sprintf("source-%d", index)
		profiles[alias] = profile("srv_"+alias, alias, alias, "", 10)
	}
	pool, err := New(profiles)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Resolve(nil); err == nil {
		t.Fatal("unbounded automatic source fan-out was accepted")
	}
}
