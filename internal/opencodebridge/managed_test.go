package opencodebridge

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

func TestManagedOpenCodeUsesPrivateIsolatedConfiguration(t *testing.T) {
	root := t.TempDir()
	bridge, err := Start(Options{Runner: &fakeRunner{result: ExecutorResult{ExecutorSessionID: "thread_fixture"}}, Select: func(context.Context, string) (string, error) { return "codex", nil }, Status: func() Status { return Status{} }})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = bridge.Close(context.Background()) })
	wrapper := filepath.Join(root, "opencode")
	body := "#!/bin/sh\nGO_WANT_MANAGED_OPENCODE_HELPER=1 exec \"" + os.Args[0] + "\" -test.run=TestManagedOpenCodeHelper -- \"$@\"\n"
	if err := os.WriteFile(wrapper, []byte(body), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("OPENCODE_CONFIG_CONTENT", `{"share":"auto"}`)
	t.Setenv("OPENCODE_CONFIG_DIR", filepath.Join(root, "hostile"))
	t.Setenv("OPENAI_API_KEY", "must-not-reach-frontend")
	t.Setenv("GITHUB_TOKEN", "generic-secret-must-not-reach-frontend")
	t.Setenv("SSH_AUTH_SOCK", "/tmp/agent-must-not-reach-frontend")
	t.Setenv("IVOAI_KNOWLEDGE_SESSION_TOKEN", "must-not-reach-frontend")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	managed, err := StartManaged(ctx, ManagedOptions{OpenCodePath: wrapper, Version: "fixture", RuntimeDir: filepath.Join(root, "runtime"), StateDir: filepath.Join(root, "state"), Directory: root, Bridge: bridge, Instructions: "fixture instructions", ResumeSessionID: "ses_resume_fixture"})
	if err != nil {
		t.Fatal(err)
	}
	defer managed.Close(context.Background())
	if !managed.BackendLoopback() || !strings.HasPrefix(managed.BackendURL(), "http://127.0.0.1:") {
		t.Fatalf("backend=%q", managed.BackendURL())
	}
	if args := managed.Args(); len(args) < 2 || args[len(args)-2] != "--session" || args[len(args)-1] != "ses_resume_fixture" {
		t.Fatalf("managed attach did not preserve the approved resume session: %v", args)
	}
	environment := strings.Join(managed.Env(), "\n")
	for _, expected := range []string{"OPENCODE_DISABLE_PROJECT_CONFIG=1", "OPENCODE_DISABLE_AUTOUPDATE=1", "OPENCODE_DISABLE_MODELS_FETCH=1", "OPENCODE_DISABLE_LSP_DOWNLOAD=1", "OPENCODE_DISABLE_EXTERNAL_SKILLS=1", "OPENCODE_DISABLE_DEFAULT_PLUGINS=1", "OPENCODE_CONFIG=", "OPENCODE_TUI_CONFIG=", "XDG_CONFIG_HOME="} {
		if !strings.Contains(environment, expected) {
			t.Fatalf("managed environment omitted %q", expected)
		}
	}
	for _, forbidden := range []string{"OPENCODE_CONFIG_CONTENT=", "OPENCODE_CONFIG_DIR=", "OPENAI_API_KEY=", "GITHUB_TOKEN=", "SSH_AUTH_SOCK=", "IVOAI_KNOWLEDGE_SESSION_TOKEN=", "must-not-reach-frontend", "generic-secret-must-not-reach-frontend"} {
		if strings.Contains(environment, forbidden) {
			t.Fatalf("managed frontend inherited forbidden environment key %q", forbidden)
		}
	}
	for _, entry := range managed.Env() {
		if !strings.HasPrefix(entry, "OPENCODE_CONFIG=") && !strings.HasPrefix(entry, "OPENCODE_TUI_CONFIG=") {
			continue
		}
		path := strings.SplitN(entry, "=", 2)[1]
		info, err := os.Stat(path)
		if err != nil || info.Mode().Perm() != 0o600 {
			t.Fatalf("managed file %s mode=%v err=%v", filepath.Base(path), info.Mode().Perm(), err)
		}
		if strings.HasPrefix(entry, "OPENCODE_CONFIG=") {
			var cfg map[string]any
			body, _ := os.ReadFile(path)
			if json.Unmarshal(body, &cfg) != nil || cfg["autoupdate"] != false || cfg["share"] != "disabled" {
				t.Fatal("unsafe managed OpenCode config")
			}
		}
	}
}

func TestManagedBackendProcessGroupDiesWithIVOAI(t *testing.T) {
	attributes := managedProcessAttributes()
	if attributes == nil || !attributes.Setpgid || attributes.Pdeathsig != syscall.SIGTERM {
		t.Fatalf("unsafe managed backend process attributes: %+v", attributes)
	}
}

func TestManagedOpenCodeRejectsUnsafeResumeSessionID(t *testing.T) {
	root := t.TempDir()
	bridge, err := Start(Options{Runner: &fakeRunner{}, Select: func(context.Context, string) (string, error) { return "codex", nil }, Status: func() Status { return Status{} }})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = bridge.Close(context.Background()) })
	wrapper := fixtureExecutable(t, root, "opencode", "#!/bin/sh\nGO_WANT_MANAGED_OPENCODE_HELPER=1 exec \""+os.Args[0]+"\" -test.run=TestManagedOpenCodeHelper -- \"$@\"\n")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := StartManaged(ctx, ManagedOptions{OpenCodePath: wrapper, Version: "fixture", RuntimeDir: filepath.Join(root, "runtime"), StateDir: filepath.Join(root, "state"), Directory: root, Bridge: bridge, ResumeSessionID: "../../unsafe"}); err == nil {
		t.Fatal("unsafe resume session identifier was accepted")
	}
}

func TestManagedOpenCodeEarlyExitReturnsWithoutHanging(t *testing.T) {
	root := t.TempDir()
	bridge, err := Start(Options{Runner: &fakeRunner{}, Select: func(context.Context, string) (string, error) { return "codex", nil }, Status: func() Status { return Status{} }})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = bridge.Close(context.Background()) })
	path := fixtureExecutable(t, root, "opencode-exit", "#!/bin/sh\nexit 7\n")
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	started := time.Now()
	if _, err := StartManaged(ctx, ManagedOptions{OpenCodePath: path, Version: "fixture", RuntimeDir: filepath.Join(root, "runtime"), StateDir: filepath.Join(root, "state"), Directory: root, Bridge: bridge}); err == nil {
		t.Fatal("early backend exit was accepted")
	}
	if time.Since(started) > 2*time.Second {
		t.Fatal("early backend exit cleanup hung")
	}
}

func TestLiveManagedOpenCodeRoutesPromptThroughIVOAI(t *testing.T) {
	path := os.Getenv("IVOAI_LIVE_OPENCODE_PATH")
	version := os.Getenv("IVOAI_LIVE_OPENCODE_VERSION")
	if path == "" || version == "" {
		t.Skip("set IVOAI_LIVE_OPENCODE_PATH and IVOAI_LIVE_OPENCODE_VERSION for the pinned integration smoke")
	}
	root := t.TempDir()
	runner := &fakeRunner{result: ExecutorResult{ExecutorSessionID: "thread_fixture"}}
	var mappingsMu sync.Mutex
	var mappings []Mapping
	bridge, err := Start(Options{Runner: runner, Select: func(context.Context, string) (string, error) { return "codex", nil }, Status: func() Status { return Status{} }, Mapping: func(value Mapping) error {
		mappingsMu.Lock()
		defer mappingsMu.Unlock()
		mappings = append(mappings, value)
		return nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer bridge.Close(context.Background())
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	managed, err := StartManaged(ctx, ManagedOptions{OpenCodePath: path, Version: version, RuntimeDir: filepath.Join(root, "runtime"), StateDir: filepath.Join(root, "state"), Directory: root, Bridge: bridge, Instructions: "Use only IVOAI-managed execution."})
	if err != nil {
		t.Fatal(err)
	}
	defer managed.Close(context.Background())
	password := envValue(managed.Env(), "OPENCODE_SERVER_PASSWORD")
	client := &http.Client{Timeout: 20 * time.Second}
	call := func(method, target, body string) *http.Response {
		request, requestErr := http.NewRequestWithContext(ctx, method, managed.BackendURL()+target, strings.NewReader(body))
		if requestErr != nil {
			t.Fatal(requestErr)
		}
		request.SetBasicAuth("ivoai", password)
		request.Header.Set("Content-Type", "application/json")
		response, responseErr := client.Do(request)
		if responseErr != nil {
			t.Fatal(responseErr)
		}
		return response
	}
	response := call(http.MethodPost, "/session", `{}`)
	var created struct {
		ID string `json:"id"`
	}
	if response.StatusCode != http.StatusOK || json.NewDecoder(response.Body).Decode(&created) != nil || !safeID(created.ID) {
		_ = response.Body.Close()
		t.Fatalf("session create status=%d id=%q", response.StatusCode, created.ID)
	}
	_ = response.Body.Close()
	response = call(http.MethodPost, "/session/"+created.ID+"/message", `{"parts":[{"type":"text","text":"return fixture"}]}`)
	_, _ = io.Copy(io.Discard, response.Body)
	_ = response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("prompt status=%d", response.StatusCode)
	}
	runner.mu.Lock()
	requests := append([]ExecutorRequest(nil), runner.requests...)
	runner.mu.Unlock()
	if len(requests) != 1 || requests[0].FrontendSessionID != created.ID || requests[0].Prompt != "return fixture" {
		t.Fatalf("OpenCode did not route through the IVOAI bridge: %+v", requests)
	}
	if err := managed.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	managed, err = StartManaged(ctx, ManagedOptions{OpenCodePath: path, Version: version, RuntimeDir: filepath.Join(root, "runtime-restarted"), StateDir: filepath.Join(root, "state"), Directory: root, Bridge: bridge, Instructions: "Use only IVOAI-managed execution.", ResumeSessionID: created.ID})
	if err != nil {
		t.Fatal(err)
	}
	defer managed.Close(context.Background())
	if args := managed.Args(); len(args) < 2 || args[len(args)-2] != "--session" || args[len(args)-1] != created.ID {
		t.Fatalf("restart did not attach the persisted OpenCode session: %v", args)
	}
	password = envValue(managed.Env(), "OPENCODE_SERVER_PASSWORD")
	response = call(http.MethodGet, "/session/"+created.ID, "")
	_, _ = io.Copy(io.Discard, response.Body)
	_ = response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("persisted OpenCode session was not available after restart: HTTP %d", response.StatusCode)
	}
	mappingsMu.Lock()
	defer mappingsMu.Unlock()
	if len(mappings) != 1 || mappings[0].FrontendSessionID != created.ID {
		t.Fatalf("session mapping=%+v", mappings)
	}
}

func TestLiveManagedOpenCodeAttachRendersIVOAIPlugin(t *testing.T) {
	path := os.Getenv("IVOAI_LIVE_OPENCODE_PATH")
	version := os.Getenv("IVOAI_LIVE_OPENCODE_VERSION")
	if path == "" || version == "" || os.Getenv("IVOAI_LIVE_OPENCODE_TUI") != "1" {
		t.Skip("set the pinned OpenCode path, version, and IVOAI_LIVE_OPENCODE_TUI=1 for the PTY smoke")
	}
	if _, err := exec.LookPath("script"); err != nil {
		t.Skip("util-linux script is not available")
	}
	root := t.TempDir()
	bridge, err := Start(Options{Runner: &fakeRunner{}, Select: func(context.Context, string) (string, error) { return "codex", nil }, Status: func() Status {
		return Status{Version: "fixture", Frontend: "opencode", KnowledgeMode: "automatic"}
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer bridge.Close(context.Background())
	ctx, cancel := context.WithTimeout(context.Background(), 35*time.Second)
	defer cancel()
	managed, err := StartManaged(ctx, ManagedOptions{OpenCodePath: path, Version: version, RuntimeDir: filepath.Join(root, "runtime"), StateDir: filepath.Join(root, "state"), Directory: root, Bridge: bridge, Instructions: "Use only IVOAI-managed execution."})
	if err != nil {
		t.Fatal(err)
	}
	defer managed.Close(context.Background())
	transcript := filepath.Join(root, "transcript")
	argv := append([]string{path}, managed.Args()...)
	commandLine := make([]string, 0, len(argv))
	for _, value := range argv {
		commandLine = append(commandLine, shellQuote(value))
	}
	command := exec.Command("script", "-qfec", strings.Join(commandLine, " "), transcript)
	command.Env = managed.Env()
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true, Pdeathsig: syscall.SIGTERM}
	stdin, err := command.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	time.Sleep(4 * time.Second)
	_, _ = io.WriteString(stdin, "/ivoai\r")
	time.Sleep(2 * time.Second)
	_, _ = stdin.Write([]byte{3})
	_ = stdin.Close()
	done := make(chan error, 1)
	go func() { done <- command.Wait() }()
	select {
	case <-done:
	case <-time.After(8 * time.Second):
		_ = syscall.Kill(-command.Process.Pid, syscall.SIGKILL)
		<-done
	}
	body, err := os.ReadFile(transcript)
	if err != nil {
		t.Fatal(err)
	}
	plain := stripTerminalControls(string(body))
	if !strings.Contains(plain, "IVOAI") || !strings.Contains(plain, "OpenCode frontend") {
		t.Fatalf("managed TUI did not render the IVOAI plugin (transcript bytes=%d)", len(body))
	}
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

func stripTerminalControls(value string) string {
	var result strings.Builder
	for _, character := range value {
		if character == '\n' || character == '\r' || character == '\t' || character >= 0x20 && character != 0x7f {
			result.WriteRune(character)
		}
	}
	return result.String()
}

func TestManagedOpenCodeHelper(t *testing.T) {
	if os.Getenv("GO_WANT_MANAGED_OPENCODE_HELPER") != "1" {
		return
	}
	args := os.Args
	port := ""
	for index := range args {
		if args[index] == "--port" && index+1 < len(args) {
			port = args[index+1]
		}
	}
	if port == "" {
		os.Exit(2)
	}
	if _, err := strconv.Atoi(port); err != nil {
		os.Exit(2)
	}
	listener, err := net.Listen("tcp4", "127.0.0.1:"+port)
	if err != nil {
		os.Exit(3)
	}
	fmt.Printf("opencode server listening on http://%s\n", listener.Addr().String())
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/config/providers" {
			if r.URL.Query().Get("directory") == "" {
				http.Error(w, "missing directory", http.StatusBadRequest)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprint(w, `{"providers":[],"default":{}}`)
			return
		}
		if r.URL.Path != "/global/health" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"healthy":true,"version":"fixture"}`)
	})}
	if err := server.Serve(listener); err != nil && err != http.ErrServerClosed {
		os.Exit(3)
	}
}

func envValue(environment []string, key string) string {
	prefix := key + "="
	for _, entry := range environment {
		if strings.HasPrefix(entry, prefix) {
			return strings.TrimPrefix(entry, prefix)
		}
	}
	return ""
}
