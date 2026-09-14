package memory

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ivo-lopes/ivoai/internal/platform"
)

func TestOwnedHookRepairPreservesForeignAndIsIdempotent(t *testing.T) {
	root := t.TempDir()
	old := filepath.Join(root, "old", "ivoai", "bin", "ai-memory")
	current := filepath.Join(root, "current", "ivoai", "bin", "ai-memory")
	if err := platform.AtomicWriteFile([]byte("#!/bin/sh\nexit 0\n"), current, 0700); err != nil {
		t.Fatal(err)
	}
	command := old + " --data-dir " + filepath.Join(root, "old", "ai-memory") + " hook --event stop --agent codex --project-strategy repo-root --auth-token fixture-canary"
	config := filepath.Join(root, "codex", "hooks.json")
	foreign := map[string]any{"type": "command", "command": "personal-hook --flag", "timeout": 42}
	doc := map[string]any{"personal": map[string]any{"keep": true}, "hooks": map[string]any{"Stop": []any{map[string]any{"matcher": "", "hooks": []any{map[string]any{"type": "command", "command": command}, foreign}}}}}
	body, _ := json.Marshal(doc)
	if err := platform.AtomicWritePrivate(body, config); err != nil {
		t.Fatal(err)
	}
	m := HookMaintenance{Agent: "codex", ConfigPath: config, Binary: current, HooksDir: filepath.Join(root, "hooks"), DataDir: filepath.Join(root, "data"), Managed: true, PreviousBinaries: []string{old}}
	before, err := m.Inspect(false)
	if err != nil || len(before) != 1 || before[0].State != "degraded" {
		t.Fatalf("initial health %v %v", before, err)
	}
	after, err := m.Inspect(true)
	if err != nil || len(after) != 1 || after[0].State != "healthy" || !after[0].Repaired {
		t.Fatalf("repair %v %v", after, err)
	}
	repaired, _ := os.ReadFile(config)
	if strings.Contains(string(repaired), "fixture-canary") || strings.Contains(string(repaired), old) {
		t.Fatal("legacy credential/path persisted")
	}
	var updated map[string]any
	if err := json.Unmarshal(repaired, &updated); err != nil {
		t.Fatal(err)
	}
	hooks := updated["hooks"].(map[string]any)["Stop"].([]any)[0].(map[string]any)["hooks"].([]any)
	got, _ := json.Marshal(hooks[1])
	want, _ := json.Marshal(foreign)
	if string(got) != string(want) || updated["personal"].(map[string]any)["keep"] != true {
		t.Fatal("foreign configuration changed")
	}
	second, err := m.Inspect(true)
	if err != nil || second[0].Repaired {
		t.Fatalf("not idempotent: %v %v", second, err)
	}
	again, _ := os.ReadFile(config)
	if string(again) != string(repaired) {
		t.Fatal("idempotent repair rewrote config")
	}
	if err := os.Remove(current); err != nil {
		t.Fatal(err)
	}
	degraded, err := m.Inspect(false)
	if err != nil || degraded[0].State != "degraded" {
		t.Fatal("missing binary reported healthy")
	}
	wrapper := filepath.Join(m.HooksDir, "codex", "ivoai-stop.sh")
	for i := 0; i < 3; i++ {
		out, err := exec.Command("/bin/sh", wrapper).CombinedOutput()
		if err != nil || len(out) != 0 {
			t.Fatal("optional unavailable hook flooded frontend")
		}
	}
}

func TestHookInstallEnvironmentExcludesAmbientCredentials(t *testing.T) {
	t.Setenv("AI_MEMORY_AUTH_TOKEN", "ambient-canary")
	t.Setenv("OPENAI_API_KEY", "provider-canary")
	t.Setenv("CODEX_HOME", t.TempDir())
	env := strings.Join(hookInstallEnv(""), "\n")
	if strings.Contains(env, "canary") || strings.Contains(env, "AUTH_TOKEN") || strings.Contains(env, "API_KEY") {
		t.Fatal("installer inherited credentials")
	}
	if !strings.Contains(env, "CODEX_HOME=") {
		t.Fatal("provider isolation home lost")
	}
}

func TestHookResolutionExit127Fixtures(t *testing.T) {
	root := t.TempDir()
	missing := filepath.Join(root, "missing")
	script := filepath.Join(root, "bad-interpreter")
	if err := platform.AtomicWriteFile([]byte("#!"+missing+"\n"), script, 0700); err != nil {
		t.Fatal(err)
	}
	for name, target := range map[string]string{"target": missing, "interpreter": script, "path": "unresolvable-ivoai-fixture"} {
		t.Run(name, func(t *testing.T) {
			cmd := exec.Command("/bin/sh", "-c", "exec \"$1\"", "fixture", target)
			cmd.Env = []string{"PATH=" + root}
			err := cmd.Run()
			if err == nil || cmd.ProcessState.ExitCode() != 127 {
				t.Fatalf("expected 127, got %v", err)
			}
		})
	}
	_, executable, reason := executableHealth(script)
	if executable || reason != "interpreter_unavailable" {
		t.Fatal("missing interpreter not detected")
	}
}

func TestUnprovenHookPathIsNotRepaired(t *testing.T) {
	root := t.TempDir()
	binary := filepath.Join(root, "foreign", "ivoai", "bin", "ai-memory")
	command := binary + " --data-dir " + filepath.Join(root, "foreign", "ai-memory") + " hook --event stop --agent codex --project-strategy repo-root"
	body, _ := json.Marshal(map[string]any{"hooks": map[string]any{"Stop": []any{map[string]any{"hooks": []any{map[string]string{"type": "command", "command": command}}}}}})
	config := filepath.Join(root, "hooks.json")
	if err := platform.AtomicWritePrivate(body, config); err != nil {
		t.Fatal(err)
	}
	m := HookMaintenance{Agent: "codex", ConfigPath: config, Binary: filepath.Join(root, "owned", "ivoai", "bin", "ai-memory"), Managed: true}
	health, err := m.Inspect(true)
	if err != nil || len(health) != 1 || health[0].Repaired || health[0].Reason != "ownership_verification_required" {
		t.Fatal("unproven path adopted")
	}
	after, _ := os.ReadFile(config)
	if string(after) != string(body) {
		t.Fatal("foreign hook modified")
	}
}

func TestHookUpgradeRollbackReapply(t *testing.T) {
	root := t.TempDir()
	a := filepath.Join(root, "a", "ivoai", "bin", "ai-memory")
	b := filepath.Join(root, "b", "ivoai", "bin", "ai-memory")
	for _, target := range []string{a, b} {
		if err := platform.AtomicWriteFile([]byte("#!/bin/sh\nexit 0\n"), target, 0700); err != nil {
			t.Fatal(err)
		}
	}
	command := a + " --data-dir " + filepath.Join(root, "a", "ai-memory") + " hook --event stop --agent codex --project-strategy repo-root"
	body, _ := json.Marshal(map[string]any{"hooks": map[string]any{"Stop": []any{map[string]any{"hooks": []any{map[string]string{"type": "command", "command": command}}}}}})
	config := filepath.Join(root, "provider", "hooks.json")
	if err := platform.AtomicWritePrivate(body, config); err != nil {
		t.Fatal(err)
	}
	m := HookMaintenance{Agent: "codex", ConfigPath: config, Binary: a, HooksDir: filepath.Join(root, "hooks"), DataDir: filepath.Join(root, "data"), Managed: true}
	for _, target := range []string{a, b, a, b} {
		m.Binary = target
		health, err := m.Inspect(true)
		if err != nil || len(health) != 1 || health[0].State != "healthy" {
			t.Fatalf("migration: %v %v", health, err)
		}
		wrapper, err := platform.ReadRegularFile(filepath.Join(m.HooksDir, "codex", "ivoai-stop.sh"), 16<<10)
		if err != nil || !strings.Contains(string(wrapper), shellQuote(target)) {
			t.Fatal("stale managed target after migration")
		}
	}
}
