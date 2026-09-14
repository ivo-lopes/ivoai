package app

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ivo-lopes/ivoai/internal/codexfrontend"
	"github.com/ivo-lopes/ivoai/internal/session"
)

func TestConversationAdoptionAndExplicitLauncherModes(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	t.Setenv("CODEX_HOME", filepath.Join(root, "personal-codex"))
	t.Setenv("CODEX_SQLITE_HOME", "")
	thread, _ := session.NewNativeUUID()
	response, _ := json.Marshal(map[string]any{"id": 2, "result": map[string]any{"thread": map[string]string{"id": thread, "cwd": root, "modelProvider": "openai"}}})
	marker := filepath.Join(root, "native-args")
	script := "#!/bin/sh\ncase \"$*\" in *app-server*)\nwhile IFS= read -r request; do\ncase \"$request\" in\n*'\"method\":\"initialize\"'*) printf '%s\\n' '{\"id\":1,\"result\":{}}' ;;\n*'\"method\":\"thread/read\"'*) printf '%s\\n' " + shellArgument(string(response)) + " ;;\nesac\ndone\nexit 0 ;;\nesac\nprintf '%s\\n' \"$@\" > " + shellArgument(marker) + "\nexit 0\n"
	a := autoTestApp(t, root, script, "#!/bin/sh\nexit 0\n")
	if _, err := a.SessionAdoptCodex(context.Background(), thread, false); err == nil {
		t.Fatal("adoption without confirmation")
	}
	v, err := a.SessionAdoptCodex(context.Background(), thread, true)
	if err != nil {
		t.Fatal(err)
	}
	again, err := a.SessionAdoptCodex(context.Background(), thread, true)
	if err != nil || again.SessionID != v.SessionID {
		t.Fatal("adoption duplicated mapping", err)
	}
	if err := a.SessionResumeMode(context.Background(), v.SessionID, "direct", true); err != nil {
		t.Fatal(err)
	}
	args, _ := os.ReadFile(marker)
	if !strings.Contains(string(args), "resume\n"+thread+"\n") {
		t.Fatal("direct did not use exact native resume")
	}
	a.StartCodexNative = func(_ context.Context, options codexfrontend.Options) (nativeCodexFrontend, error) {
		if options.ResumeThreadID != thread || options.NativeState == nil {
			t.Fatal("orchestrated resume lost adopted native state")
		}
		return fixtureNativeFrontend{}, nil
	}
	if err := a.SessionResumeMode(context.Background(), v.SessionID, "orchestrated", true); err != nil {
		t.Fatal(err)
	}
	if err := a.SessionStart(context.Background(), "codex", session.ModeDirect, []string{"resume", thread, "--model", "explicit-fixture-model"}); err != nil {
		t.Fatal(err)
	}
	args, _ = os.ReadFile(marker)
	if !strings.Contains(string(args), "resume\n"+thread+"\n--model\nexplicit-fixture-model\n") {
		t.Fatal("direct launcher lost exact ID or explicit model override")
	}
	values, err := a.SessionList()
	if err != nil || len(values) != 1 || values[0].SessionID != v.SessionID || values[0].Mode != session.ModeDirect || values[0].FrontendSessionID != thread {
		t.Fatal("launcher portability duplicated or replaced identity", err)
	}
}
