package serverpool

import (
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
	if _, err := pool.Resolve(nil); err == nil {
		t.Fatal("ambiguous implicit selection was accepted")
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
