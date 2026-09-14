package agents

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestOpenCodeNativeInventoryIsBoundedMetadataAndProjectScoped(t *testing.T) {
	root := t.TempDir()
	body, _ := json.Marshal([]map[string]string{{"id": "ses_owned", "directory": root}, {"id": "ses_foreign", "directory": "/different"}, {"id": "invalid", "directory": root}})
	binary := filepath.Join(root, "opencode")
	if err := os.WriteFile(binary, []byte("#!/bin/sh\n[ \"$1 $2 $3 $4 $5 $6\" = 'session list --format json --max-count 100' ] || exit 3\nprintf '%s' '"+string(body)+"'\n"), 0700); err != nil {
		t.Fatal(err)
	}
	ids, err := OpenCodeSessionInventory(context.Background(), binary, root)
	if err != nil || len(ids) != 1 || !ids["ses_owned"] {
		t.Fatal("native inventory leaked scope or failed", err)
	}
}
