package app

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/ivo-lopes/ivoai/internal/codeintel"
	"github.com/ivo-lopes/ivoai/internal/components"
	"github.com/ivo-lopes/ivoai/internal/config"
	"github.com/ivo-lopes/ivoai/internal/connections"
	"github.com/ivo-lopes/ivoai/internal/routing"
	"github.com/ivo-lopes/ivoai/internal/session"
	"github.com/ivo-lopes/ivoai/internal/supplychain"
	"github.com/ivo-lopes/ivoai/internal/workers"
	"github.com/ivo-lopes/ivoai/internal/workingcontext"
)

func TestNativeCodeGrantRequiresRunnableApprovedPrimary(t *testing.T) {
	cfg := config.Default()
	value := session.Session{State: session.StateRunning, PlanID: "plan_fixture", Decisions: []session.Decision{{ID: "plan_fixture", Kind: "plan", State: "approved"}}, Tasks: []session.TaskMetadata{{ID: "primary", ExecutionMode: "primary", State: session.StatePrimary}}}
	for _, tool := range codeintel.ToolNames() {
		if allowed, _ := primaryMCPGrant(value, cfg, "ivoai-code", tool); !allowed {
			t.Fatalf("read-only native tool not available to approved primary: %s", tool)
		}
	}
	for _, mutate := range []func(*session.Session){
		func(v *session.Session) { v.Decisions = nil },
		func(v *session.Session) { v.Tasks[0].ExecutionMode = "worker" },
		func(v *session.Session) { v.Tasks[0].State = session.StateCompleted },
		func(v *session.Session) { v.Tasks[0].Dependencies = []string{"unfinished"} },
	} {
		copy := value
		copy.Tasks = append([]session.TaskMetadata(nil), value.Tasks...)
		mutate(&copy)
		if allowed, _ := primaryMCPGrant(copy, cfg, "ivoai-code", "code_inspect"); allowed {
			t.Fatal("native tool escaped primary admission")
		}
	}
	if allowed, _ := primaryMCPGrant(value, cfg, "ivoai-code", "graft_execute"); allowed {
		t.Fatal("unknown tool authorized")
	}
}

func TestNativeCodeProjectionIsProcessLocal(t *testing.T) {
	cfg := config.Default()
	cfg.MCP.Servers["ivoai-code"] = config.MCPServer{Enabled: true, Kind: "native", URL: "http://127.0.0.1:1234/mcp/0", SessionApproved: true, HeaderEnv: map[string]string{"Authorization": externalMCPTokenEnvironment}}
	for _, executor := range []string{"codex", "claude"} {
		args, err := processLocalExternalMCPArgs(executor, t.TempDir(), cfg)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(strings.Join(args, " "), "ivoai-code") {
			t.Fatalf("%s native code projection missing: %v", executor, args)
		}
	}
	if !connections.IsManagedMCPName("ivoai-code") {
		t.Fatal("native server name can be claimed by external registry")
	}
	a := autoTestApp(t, t.TempDir(), "#!/bin/sh\nexit 0\n", "#!/bin/sh\nexit 0\n")
	if err := a.MCPAdd("ivoai-code", "https://example.invalid/mcp"); err == nil {
		t.Fatal("reserved native name accepted")
	}
	if _, err := a.CodeIntelligenceProvider(context.Background(), t.TempDir(), "cs_fixture"); err == nil {
		t.Fatal("unverified native runtime accepted")
	}
}

func TestNativeCodeWorkerPrefetchIsExactScopedData(t *testing.T) {
	root := t.TempDir()
	a := autoTestApp(t, root, "#!/bin/sh\nexit 0\n", "#!/bin/sh\nexit 0\n")
	source := t.TempDir()
	if err := exec.Command("git", "init", "-q", source).Run(); err != nil {
		t.Fatal(err)
	}
	body := "package fixture\nfunc Authorized() {}\n"
	if err := os.WriteFile(filepath.Join(source, "fixture.go"), []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	provider := &codeintel.Provider{Root: source, Scope: "cs_fixture", Permissions: "filesystem.read", Allowed: true}
	id, _ := session.NewID()
	request := workers.Request{Directory: source, SharedContextBrief: `{"local_objective":"fixture"}`, SkillInstructions: "trusted gate instructions"}
	task := routing.Task{TaskInput: routing.TaskInput{ID: "task_fixture", Task: "Inspect API fixture.go"}}
	got, err := a.prefetchWorkerCode(context.Background(), provider, id, task, request)
	if err != nil {
		t.Fatal(err)
	}
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal([]byte(got.SharedContextBrief), &envelope); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(envelope["code_evidence"]), "exact_required") || !strings.Contains(string(envelope["code_evidence"]), "Authorized") {
		t.Fatalf("prefetch missing exact source: %s", got.SharedContextBrief)
	}
	if got.SkillInstructions != request.SkillInstructions {
		t.Fatal("source entered skill authority")
	}
	var evidence struct {
		Artifact workingcontext.ArtifactRef `json:"artifact"`
	}
	if err := json.Unmarshal(envelope["code_evidence"], &evidence); err != nil {
		t.Fatal(err)
	}
	store, err := workingcontext.NewLocalStore(filepath.Join(a.Store.Paths.CacheDir, "working-context"), workingcontext.LocalOptions{})
	if err != nil {
		t.Fatal(err)
	}
	reader, _, err := store.Read(context.Background(), workingcontext.Ownership{SessionID: id, TaskID: task.ID}, evidence.Artifact.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	exact, _ := io.ReadAll(reader)
	if !strings.Contains(string(exact), body) {
		t.Fatal("exact evidence missing")
	}
	for _, text := range []string{"Explain institutional policy", "verify compilation fixture.go"} {
		task.Task = text
		got, err := a.prefetchWorkerCode(context.Background(), provider, id, task, request)
		if err != nil || got.SharedContextBrief != request.SharedContextBrief {
			t.Fatal("unrelated task prefetched")
		}
	}
}

func TestWorkerCodePrefetchCannotBypassExactToolRestrictions(t *testing.T) {
	a := &App{}
	for _, grants := range [][]string{{}, {"graft_repo_map"}} {
		task := routing.Task{TaskInput: routing.TaskInput{Task: "Inspect API fixture.go", AllowedMCPTools: map[string][]string{nativeCodeMCP: grants}}}
		request := workers.Request{SharedContextBrief: `{"local_objective":"already known"}`}
		// Nil provider deliberately proves that a denied prefetch never reaches
		// source inspection, installation, indexing or artifact persistence.
		got, err := a.prefetchWorkerCode(context.Background(), nil, "session", task, request)
		if err != nil || got.SharedContextBrief != request.SharedContextBrief {
			t.Fatal("restricted task changed context")
		}
	}
}

func TestNativeCodeOpenCodeProjectionPreservesConfiguration(t *testing.T) {
	cfg := config.Default()
	cfg.MCP.Servers[nativeCodeMCP] = config.MCPServer{Enabled: true, Kind: "native", URL: "http://127.0.0.1:1234/mcp/0", HeaderEnv: map[string]string{"Authorization": externalMCPTokenEnvironment}}
	env, err := nativeCodeEnvironment([]string{`OPENCODE_CONFIG_CONTENT={"model":"fixture/model","mcp":{"other":{"type":"remote","url":"http://localhost:5678"}}}`}, cfg)
	if err != nil {
		t.Fatal(err)
	}
	body := environmentValue(env, "OPENCODE_CONFIG_CONTENT")
	for _, part := range []string{"fixture/model", "other", "ivoai-code", externalMCPTokenEnvironment} {
		if !strings.Contains(body, part) {
			t.Fatalf("configuration lost %s: %s", part, body)
		}
	}
}

func verifiedCodeRuntimeFixture(t *testing.T) (*App, string, components.Spec, string, string) {
	t.Helper()
	a := autoTestApp(t, t.TempDir(), "#!/bin/sh\nexit 0\n", "#!/bin/sh\nexit 0\n")
	var archive bytes.Buffer
	gz := gzip.NewWriter(&archive)
	tw := tar.NewWriter(gz)
	payload := "#!/bin/sh\necho 0.21.1-ivoai.1\n"
	if err := tw.WriteHeader(&tar.Header{Name: "graft-runtime", Mode: 0700, Size: int64(len(payload))}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write([]byte(payload)); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(archive.Bytes())
	spec := components.Spec{Name: "graft", Version: codeintel.Version, Revision: strings.Repeat("a", 64), PayloadFormat: "tar_gzip", PayloadPath: "graft-runtime", License: "MIT", SignatureStatus: "not_exposed", AttestationStatus: "not_exposed", TrustLevel: "checksum_only", Assets: map[string]components.Asset{runtime.GOOS + "/" + runtime.GOARCH: {URL: "https://example.invalid/fixture.tar.gz", SHA256: fmt.Sprintf("%x", digest)}}}
	resolved, err := components.ResolvedSource(spec)
	if err != nil {
		t.Fatal(err)
	}
	accept := supplychain.ValidatorFunc(func(context.Context, supplychain.ResolvedSource, string) error { return nil })
	manager := supplychain.Manager{Root: filepath.Join(a.Store.Paths.DataDir, "supply-chain"), Structural: accept, Policy: accept, Health: accept}
	staged, err := manager.StageArchive(context.Background(), resolved, bytes.NewReader(archive.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.Promote(staged); err != nil {
		t.Fatal(err)
	}
	_, distribution, err := manager.Active("graft")
	if err != nil {
		t.Fatal(err)
	}
	state, err := a.Store.LoadState()
	if err != nil {
		t.Fatal(err)
	}
	state.Components["graft"] = config.ComponentState{Installed: true, Managed: true, Version: codeintel.Version, Path: filepath.Join(distribution, "graft-runtime")}
	if err := a.Store.SaveState(state); err != nil {
		t.Fatal(err)
	}
	node := components.ManagedNodePath(a.Store.Paths.DataDir)
	if err := os.MkdirAll(filepath.Dir(node), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(node, []byte("#!/bin/sh\nexit 0\n"), 0700); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	if err := exec.Command("git", "init", "-q", root).Run(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	return a, root, spec, distribution, node
}

func TestNativeCodeProviderRequiresExpectedVerifiedArtifact(t *testing.T) {
	a, root, spec, distribution, _ := verifiedCodeRuntimeFixture(t)
	provider, err := a.codeIntelligenceProvider(context.Background(), root, "cs_fixture", spec)
	if err != nil {
		t.Fatal(err)
	}
	if provider.Root != root || provider.Scope != "cs_fixture" || provider.Distribution != distribution || !provider.Allowed {
		t.Fatalf("wrong host authority: %+v", provider)
	}
	wrong := spec
	wrong.Revision = strings.Repeat("b", 64)
	if _, err := a.codeIntelligenceProvider(context.Background(), root, "cs_fixture", wrong); err == nil {
		t.Fatal("active artifact differs from compiled metadata")
	}
	if err := os.WriteFile(filepath.Join(distribution, "graft-runtime"), []byte("tampered"), 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := a.codeIntelligenceProvider(context.Background(), root, "cs_fixture", spec); err == nil {
		t.Fatal("corrupted payload accepted")
	}
}

func TestNativeCodeProjectionCannotAdoptPersistentEndpoint(t *testing.T) {
	a := autoTestApp(t, t.TempDir(), "#!/bin/sh\nexit 0\n", "#!/bin/sh\nexit 0\n")
	cfg := config.Default()
	cfg.MCP.Servers[nativeCodeMCP] = config.MCPServer{Enabled: true, Kind: "native", URL: "http://127.0.0.1:1234/mcp/0", HeaderEnv: map[string]string{"Authorization": externalMCPTokenEnvironment}}
	got, err := a.prepareSessionKnowledge(context.Background(), cfg, nil, "codex", t.TempDir(), t.TempDir(), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer got.close()
	if _, ok := got.config.MCP.Servers[nativeCodeMCP]; ok {
		t.Fatal("persisted endpoint acquired native authority without verified runtime")
	}
	if _, ok := cfg.MCP.Servers[nativeCodeMCP]; !ok {
		t.Fatal("operator configuration mutated")
	}
}
