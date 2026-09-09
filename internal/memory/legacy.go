package memory

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	"github.com/ivo-lopes/ivoai/internal/platform"
	"github.com/pelletier/go-toml/v2"
)

// LegacyCodexMigration only recognizes the exact unauthenticated HTTP entry
// emitted by the old IVOAI setup AND an IVOAI-owned hook installation. A name,
// loopback URL, or an installed ai-memory binary alone is not ownership proof.
// Unrecognized/customized entries are preserved for explicit operator review.
type LegacyCodexMigration struct {
	ConfigPath        string
	HooksPath         string
	OwnedHooksDir     string
	ReceiptPath       string
	PreviouslyManaged bool
}

func (m LegacyCodexMigration) Apply() (bool, error) {
	if !m.PreviouslyManaged || !filepath.IsAbs(m.OwnedHooksDir) {
		return false, nil
	}
	body, err := platform.ReadRegularFile(m.ConfigPath, 1<<20)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	var doc map[string]any
	if err := toml.Unmarshal(body, &doc); err != nil {
		return false, errors.New("legacy MCP configuration is invalid")
	}
	servers, _ := doc["mcp_servers"].(map[string]any)
	entry, _ := servers["ai-memory"].(map[string]any)
	if len(entry) != 2 || entry["url"] != "http://127.0.0.1:49374/mcp" || entry["default_tools_approval_mode"] != "approve" {
		return false, nil
	}
	hooks, err := platform.ReadRegularFile(m.HooksPath, 1<<20)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	var hookDoc any
	if json.Unmarshal(hooks, &hookDoc) != nil || !ownedHookReference(hookDoc, filepath.Join(m.OwnedHooksDir, "codex", "session-start.sh")) {
		return false, nil
	}
	// Restrict automatic migration to canonical block-form TOML. Preserve all
	// surrounding bytes; validate the semantic delta before the atomic write.
	lines := strings.SplitAfter(string(body), "\n")
	var kept strings.Builder
	inside, found := false, false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "[") {
			inside = trimmed == "[mcp_servers.ai-memory]" || trimmed == "[mcp_servers.\"ai-memory\"]" || trimmed == "[mcp_servers.'ai-memory']"
			found = found || inside
		}
		if !inside {
			kept.WriteString(line)
		}
	}
	if !found {
		return false, nil
	}
	var updated map[string]any
	if toml.Unmarshal([]byte(kept.String()), &updated) != nil {
		return false, errors.New("legacy MCP migration could not preserve TOML")
	}
	delete(servers, "ai-memory")
	if len(servers) == 0 {
		delete(doc, "mcp_servers")
	}
	if !reflect.DeepEqual(doc, updated) {
		return false, errors.New("legacy MCP migration would alter unrelated configuration")
	}
	// Receipt contains only the two known public fields, never the shared config,
	// hook commands, spool, auth store, or provider credentials.
	receipt, _ := json.Marshal(map[string]any{"schema": 1, "migration": "ivoai-126", "ownership": "legacy_exact_entry_and_ivoai_hooks", "removed_entry": entry})
	if err := platform.AtomicWritePrivate(receipt, m.ReceiptPath); err != nil {
		return false, err
	}
	current, err := platform.ReadRegularFile(m.ConfigPath, 1<<20)
	if err != nil {
		return false, err
	}
	if !bytes.Equal(body, current) {
		return false, errors.New("Codex configuration changed during migration; retry setup")
	}
	if err := platform.AtomicWriteFile([]byte(kept.String()), m.ConfigPath, 0o600); err != nil {
		return false, err
	}
	return true, nil
}

func ownedHookReference(value any, script string) bool {
	switch v := value.(type) {
	case map[string]any:
		if command, ok := v["command"].(string); ok {
			for _, expected := range []string{script, "bash " + script, "sh " + script, "bash \"" + script + "\"", "sh \"" + script + "\""} {
				if command == expected {
					return true
				}
			}
		}
		for _, child := range v {
			if ownedHookReference(child, script) {
				return true
			}
		}
	case []any:
		for _, child := range v {
			if ownedHookReference(child, script) {
				return true
			}
		}
	}
	return false
}
