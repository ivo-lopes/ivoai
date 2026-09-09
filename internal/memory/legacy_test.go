package memory

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLegacyCodexMigrationRequiresOwnershipAndPreservesPersonalAndSpool(t *testing.T) {
	for _, scenario := range []string{"managed", "personal_same_name", "personal_same_url", "unowned_component", "unowned_hooks", "new_router", "none"} {
		t.Run(scenario, func(t *testing.T) {
			root := t.TempDir()
			ownedHooks := filepath.Join(root, "ivoai", "hooks")
			configPath, hooksPath := filepath.Join(root, "config.toml"), filepath.Join(root, "hooks.json")
			base := "model = 'fixture'\n[mcp_servers.personal]\nurl = 'https://personal.example/mcp'\n"
			entry := "[mcp_servers.ai-memory]\nurl = 'http://127.0.0.1:49374/mcp'\ndefault_tools_approval_mode = 'approve'\n"
			switch scenario {
			case "personal_same_name":
				entry = "[mcp_servers.ai-memory]\nurl = 'https://personal.example/mcp'\n"
			case "personal_same_url":
				entry += "enabled_tools = ['memory_query']\n"
			case "none":
				entry = ""
			case "new_router":
				entry = "[mcp_servers.ivoai-memory]\nurl = 'http://127.0.0.1:9999/mcp/memory'\n"
			}
			original := base + entry
			if err := os.WriteFile(configPath, []byte(original), 0o600); err != nil {
				t.Fatal(err)
			}
			hookCommand := filepath.Join(ownedHooks, "codex", "session-start.sh")
			if scenario == "unowned_hooks" {
				hookCommand = "/personal/hooks/session-start.sh"
			}
			hooks, _ := json.Marshal(map[string]any{"hooks": []any{map[string]string{"command": "bash " + hookCommand}}})
			if err := os.WriteFile(hooksPath, hooks, 0o600); err != nil {
				t.Fatal(err)
			}
			spool := filepath.Join(root, "spool")
			os.WriteFile(spool, []byte("private queued event"), 0o600)
			m := LegacyCodexMigration{ConfigPath: configPath, HooksPath: hooksPath, OwnedHooksDir: ownedHooks, ReceiptPath: filepath.Join(root, "receipt.json"), PreviouslyManaged: scenario != "unowned_component"}
			changed, err := m.Apply()
			if err != nil {
				t.Fatal(err)
			}
			if changed != (scenario == "managed") {
				t.Fatalf("unexpected migration for %s", scenario)
			}
			after, _ := os.ReadFile(configPath)
			want := original
			if changed {
				want = base
			}
			if string(after) != want {
				t.Fatal("unrelated config changed")
			}
			reapplied, err := m.Apply()
			if err != nil || reapplied {
				t.Fatalf("not idempotent: %v", err)
			}
			queued, _ := os.ReadFile(spool)
			if string(queued) != "private queued event" {
				t.Fatal("spool changed")
			}
			if changed {
				receipt, _ := os.ReadFile(m.ReceiptPath)
				if strings.Contains(string(receipt), "private") || strings.Contains(string(receipt), "personal") {
					t.Fatal("unrelated data in migration receipt")
				}
			}
		})
	}
}
