package quota

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestContinuityAccountReferenceOfficialMetadataOnly(t *testing.T) {
	for _, email := range []string{"fixture@example.invalid", "other@example.invalid"} {
		binary := filepath.Join(t.TempDir(), "codex")
		body := "#!/bin/sh\nread -r request\nprintf '%s\\n' '{\"id\":1,\"result\":{}}'\nread -r initialized\nread -r account\nprintf '%s\\n' '{\"id\":2,\"result\":{\"account\":{\"type\":\"chatgpt\",\"email\":\"" + email + "\"}}}'\n"
		if err := os.WriteFile(binary, []byte(body), 0700); err != nil {
			t.Fatal(err)
		}
		got, err := AccountReference(context.Background(), binary, "codex")
		if err != nil || got != identityReference("codex", email) || strings.Contains(got, email) {
			t.Fatal("invalid opaque account reference", err)
		}
	}
	if identityReference("codex", "fixture@example.invalid") == identityReference("claude", "fixture@example.invalid") {
		t.Fatal("provider identities crossed")
	}
	if got, err := AccountReference(context.Background(), "not-used", "unsupported"); err != nil || got != "" {
		t.Fatal("fabricated unknown-provider proof")
	}
}
