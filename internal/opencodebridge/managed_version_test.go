package opencodebridge

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestManagedOpenCodeOldOrUnknownVersionFailsBeforeLeaseOrExecution(t *testing.T) {
	for _, version := range []string{"", "1.19.0", "1.19.0-ivoai.2", "1.19.0-ivoai.4"} {
		t.Run(version, func(t *testing.T) {
			root := t.TempDir()
			marker := filepath.Join(root, "executed")
			binary := fixtureExecutable(t, root, "old-opencode", "#!/bin/sh\ntouch '"+marker+"'\nexit 1\n")
			runtime, state := filepath.Join(root, "runtime"), filepath.Join(root, "state")
			_, err := StartManaged(context.Background(), ManagedOptions{NativeExecutor: true, OpenCodePath: binary, Version: version, Directory: root, RuntimeDir: runtime, StateDir: state})
			if err == nil || !strings.Contains(err.Error(), "MANAGED_OPENCODE_UPDATE_REQUIRED") {
				t.Fatal("unsupported runtime did not require a reviewed update", err)
			}
			for _, path := range []string{marker, runtime, state} {
				if _, err := os.Lstat(path); !os.IsNotExist(err) {
					t.Fatal("refused runtime changed state or executed", filepath.Base(path), err)
				}
			}
		})
	}
}
