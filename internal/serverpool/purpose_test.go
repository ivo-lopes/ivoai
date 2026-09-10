package serverpool

import (
	"testing"

	"github.com/ivo-lopes/ivoai/internal/config"
)

func TestPurposeSelectionDoesNotImplicitlyFederate(t *testing.T) {
	pool, err := New(map[string]config.ServerProfile{
		"company-a": {ID: "srv_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", URL: "https://a.example.invalid", Purpose: "voicecorp", Enabled: true, Status: "connected"},
		"company-b": {ID: "srv_bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", URL: "https://b.example.invalid", Purpose: "mindsite", Enabled: true, Status: "connected"},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		prompt string
		want   int
	}{
		{"Analise somente Voicecorp.", 1},
		{"Mindsite inventory", 1},
		{"Compare Voicecorp e Mindsite", 2},
		{"Leia VERSION do diretório local", 0},
	} {
		selected, err := pool.ResolvePurposes(PurposeAuto, nil, pool.MentionedPurposes(tc.prompt))
		if err != nil || len(selected.Groups) != tc.want {
			t.Fatalf("selection groups=%d, want=%d, err=%v", len(selected.Groups), tc.want, err)
		}
	}
	selected, err := pool.ResolvePurposes(PurposeAuto, []string{"mindsite"}, []string{"voicecorp"})
	if err != nil || len(selected.Groups) != 1 || selected.Groups[0].Purpose != "mindsite" {
		t.Fatal("explicit selection lost authority", err)
	}
	selected, err = pool.ResolvePurposes(ExplicitOnly, nil, []string{"voicecorp"})
	if err != nil || len(selected.Groups) != 0 {
		t.Fatal("explicit-only inherited purpose hints", err)
	}
	selected, err = pool.ResolvePurposes(AllEnabled, nil, nil)
	if err != nil || len(selected.Groups) != 2 {
		t.Fatal("compatibility federation lost", err)
	}
}
