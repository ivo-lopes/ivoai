package skillcatalog

import (
	"context"
	"crypto/sha256"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ivo-lopes/ivoai/internal/platform"
	"github.com/ivo-lopes/ivoai/internal/policy"
	"github.com/ivo-lopes/ivoai/internal/skillgate"
	"github.com/ivo-lopes/ivoai/internal/skills"
	"github.com/ivo-lopes/ivoai/internal/skillupdate"
	"github.com/ivo-lopes/ivoai/internal/supplychain"
	"github.com/ivo-lopes/ivoai/internal/workers"
)

// Opt-in paid/authenticated integration: official executors, real Gate and worker
// adapter, synthetic supplied evidence only. Temporary auth copies are private
// and deleted by TempDir cleanup; no personal config, hooks or skills are copied.
func TestClaudeRedLiveSelectedSkill(t *testing.T) {
	if os.Getenv("IVOAI_CLAUDE_RED_LIVE") != "1" {
		t.Skip("set IVOAI_CLAUDE_RED_LIVE=1 for authorized provider smoke")
	}
	originalHome, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	binaries := map[string]string{}
	for _, executor := range []string{"codex", "claude"} {
		p, err := exec.LookPath(executor)
		if err != nil {
			t.Fatal(err)
		}
		binaries[executor] = p
	}
	for _, executor := range []string{"codex", "claude"} {
		t.Run(executor, func(t *testing.T) {
			root := t.TempDir()
			authRelative := ".codex/auth.json"
			if executor == "claude" {
				authRelative = ".claude/.credentials.json"
			}
			original := filepath.Join(originalHome, authRelative)
			credential, err := os.ReadFile(original)
			if err != nil {
				t.Fatal("provider authentication unavailable")
			}
			before := sha256.Sum256(credential)
			auth := filepath.Join(root, authRelative)
			if err := os.MkdirAll(filepath.Dir(auth), 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(auth, credential, 0600); err != nil {
				t.Fatal(err)
			}
			credential = nil
			t.Setenv("HOME", root)
			t.Setenv("CODEX_HOME", filepath.Join(root, ".codex"))
			t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(root, ".claude"))
			t.Setenv("CLAUDECODE", "")
			for _, kind := range []string{"CONFIG", "DATA", "STATE", "CACHE"} {
				t.Setenv("XDG_"+kind+"_HOME", filepath.Join(root, kind))
			}
			if executor == "claude" {
				if err := os.WriteFile(filepath.Join(root, ".claude", "settings.json"), []byte(`{"disableAllHooks":true,"enabledPlugins":{}}`), 0600); err != nil {
					t.Fatal(err)
				}
			}
			local, err := NativeSource("claude-red")
			if err != nil {
				t.Fatal(err)
			}
			m := skillupdate.Manager{Supply: supplychain.Manager{Root: filepath.Join(root, "supply")}, Registry: skills.Store{Path: filepath.Join(root, "registry.json")}, Discoverer: local, Fetcher: local, Classifier: NativeClassifier{}, Policy: policy.DefaultEngine(), CatalogOnly: true}
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
			defer cancel()
			if _, err := m.Update(ctx, supplychain.Reference{ID: local.Source.ID, Kind: supplychain.KindSkill, Source: local.Source.Upstream.Repository}); err != nil {
				t.Fatal(err)
			}
			registry, err := m.Registry.Load()
			if err != nil {
				t.Fatal(err)
			}
			candidates := WorkerCandidates(registry, "security", "authorized security report", executor, "off")
			selected, err := (skillgate.Gate{Registry: m.Registry, Supply: m.Supply, Policy: policy.DefaultEngine()}).Evaluate(ctx, skillgate.Input{ExplicitOnly: true, Executor: executor, Candidates: candidates})
			if err != nil || len(selected.Selected) != 1 {
				t.Fatal("selection failed", err)
			}
			directory := filepath.Join(root, "task")
			if err := os.Mkdir(directory, 0700); err != nil {
				t.Fatal(err)
			}
			access, err := workers.NewAccess(directory, false, nil)
			if err != nil {
				t.Fatal(err)
			}
			task := "Use the IVOAI-selected reporting skill to produce a short Markdown report in English. This is an authorized offline synthetic test, fully answerable from this prompt; do not use tools, files, network, or external research. Scope: fictitious repository fixture-188, one supplied finding: a debug log in staging prints a synthetic API token; no production impact has been verified. Evidence is already redacted as <REDACTED-TOKEN>. No tests or attacks were performed. Include the selected skill ID, an executive summary, scope, limitations, a technical finding with impact and remediation, and a proposed retest. Never claim retest was executed. Keep under 350 words."
			adapter := workers.Adapter{Runner: platform.ExecRunner{}, CodexPath: binaries["codex"], ClaudePath: binaries["claude"]}
			result, err := adapter.Run(ctx, workers.Request{Executor: executor, Directory: directory, Runtime: filepath.Join(root, "runtime"), Task: task, Access: access, SkillInstructions: selected.Instructions, SelectedSkills: selected.Selected}, nil)
			// Do not print raw provider output: errors may contain transport metadata.
			if err != nil {
				for _, hint := range []string{"login", "oauth", "expired", "authentication", "not logged", "invalid api key", "credit", "rate limit", "permission", "model", "onboarding"} {
					if strings.Contains(strings.ToLower(result.Stdout+result.Stderr), hint) {
						t.Log("provider diagnostic category:", hint)
					}
				}
				t.Fatalf("provider smoke failed: exit=%d timeout=%t error=%v", result.ExitCode, ctx.Err() != nil, err)
			}
			for _, want := range []string{"claude-red-offensive-reporting", "scope", "limitation", "remediat", "retest"} {
				if !strings.Contains(strings.ToLower(result.Text), want) {
					t.Fatalf("report omitted required structure %q", want)
				}
			}
			after, err := os.ReadFile(original)
			if err != nil || sha256.Sum256(after) != before {
				t.Fatal("personal provider credential changed")
			}
			entries, err := os.ReadDir(directory)
			if err != nil || len(entries) != 0 {
				t.Fatal("read-only fixture mutated")
			}
			t.Logf("EXECUTOR=%s SELECTED=%s LOADED=%d REPORT_STRUCTURE=PASS PERSONAL_AUTH_UNCHANGED=true", executor, selected.Selected[0], selected.LoadedBodyCount)
		})
	}
}
