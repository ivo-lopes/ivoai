//go:build linux

package app

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/ivo-lopes/ivoai/internal/codexfrontend"
	"github.com/ivo-lopes/ivoai/internal/core"
	"github.com/ivo-lopes/ivoai/internal/opencodebridge"
	"github.com/ivo-lopes/ivoai/internal/routing"
	"github.com/ivo-lopes/ivoai/internal/session"
	"golang.org/x/sys/unix"
)

// This opt-in test drives the official presentation TUI and official backend
// CLI. A PTY exit alone is insufficient: both real model turns, persisted Core
// receipts, and the owned App Server's normal shutdown must all succeed.
func TestLiveAdaptivePrimaryNativeTUI(t *testing.T) {
	if os.Getenv("IVOAI_ADAPTIVE_NATIVE_TUI_LIVE") != "1" {
		t.Skip("NOT_RUN: explicit official Codex native TUI integration required")
	}
	binary := os.Getenv("IVOAI_LIVE_CODEX_PATH")
	if !filepath.IsAbs(binary) {
		t.Fatal("reviewed official executable required")
	}
	report := os.Getenv("IVOAI_ADAPTIVE_NATIVE_TUI_REPORT")
	if primaryDirectActivationOutput(report) != nil {
		t.Fatal("new private report path required")
	}
	type receipt struct {
		SchemaVersion      int                        `json:"schema_version"`
		Scope              string                     `json:"scope"`
		Outcome            string                     `json:"outcome"`
		Failure            string                     `json:"failure,omitempty"`
		WallMS             int64                      `json:"wall_ms"`
		CompletedTurns     int                        `json:"completed_turns"`
		Attempts           int                        `json:"attempts"`
		ClosedReceipts     int                        `json:"closed_receipts"`
		Approvals          int                        `json:"code_read_approvals"`
		DAG                bool                       `json:"dag"`
		Workers            int                        `json:"workers"`
		SameFrontend       bool                       `json:"same_frontend"`
		SameNativeThread   bool                       `json:"same_native_thread"`
		BackendExitZero    bool                       `json:"backend_exit_zero"`
		FrontendExitZero   bool                       `json:"frontend_exit_zero"`
		CleanShutdown      bool                       `json:"clean_shutdown"`
		OfficialUsage      []*core.ReportedTokenUsage `json:"official_client_usage,omitempty"`
		GlobalEconomicGate string                     `json:"global_economic_gate"`
		Phase              string                     `json:"phase"`
		Startup            adaptiveNativeStartup      `json:"startup"`
	}
	row := receipt{SchemaVersion: 1, Scope: "official_reviewed_Codex_linux_amd64_native_TUI_two_turns", Outcome: "FAIL", GlobalEconomicGate: "INCOMPLETE"}
	started := time.Now()
	defer func() {
		row.WallMS = time.Since(started).Milliseconds()
		out, err := os.OpenFile(report, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			t.Error("bounded integration receipt unavailable")
			return
		}
		if err := json.NewEncoder(out).Encode(row); err != nil {
			t.Error("bounded integration receipt failed")
		}
		if out.Close() != nil {
			t.Error("bounded integration receipt close failed")
		}
	}()
	fail := func(code string) {
		t.Helper()
		row.Failure = code
		t.Fatal(code)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	environment := prefetchSubscriptionEnvironment()
	f := adaptiveInferenceFixture(t, binary)
	if _, err := fingerprintAdaptivePair(ctx, func() adaptiveRuntime { _, p := adaptiveLiveRuntime(t, binary); return p }()); err != nil {
		fail("OFFICIAL_PAIR_NOT_MATCHED")
	}
	registry := (routing.Discoverer{CodexPath: binary}).Discover(ctx)
	observed := opencodebridge.CatalogFromRegistry(registry).RestrictedTo("codex")
	model, available := observed.StrongPrimary("codex")
	if !available {
		row.Outcome, row.Failure = "NOT_RUN", "VERIFIED_STRONG_MODEL_UNAVAILABLE"
		t.Skip("NOT_RUN: observed official strong model unavailable")
	}
	if _, valid := observed.Resolve(model.ID, model.DefaultEffort); !valid {
		fail("VERIFIED_MODEL_EFFORT_UNAVAILABLE")
	}
	scope, err := prefetchNewDirectScope(binary, f.root, model.UpstreamModel, "read-only", environment)
	if err != nil {
		fail("SUBSCRIPTION_SCOPE_UNAVAILABLE")
	}
	_, admission := prefetchCommonLiveAdmission(ctx, scope, prefetchSubscriptionHostPolicy{PermitFilteredEnvironment: true})
	if admission.State != "ADMITTED" {
		row.Outcome, row.Failure = "NOT_RUN", admission.Reason
		t.Skip("NOT_RUN: existing official admission refused")
	}
	// Narrow the presentation to the one real, observed model under test. This
	// selects its supported default effort; it invents no capability or provider.
	provider := registry.Providers["codex"]
	for _, entry := range provider.Models {
		if entry.Name == model.UpstreamModel {
			entry.DefaultEffort = model.DefaultEffort
			provider.Models = []routing.ModelCapability{entry}
			break
		}
	}
	registry.Providers = map[string]routing.ProviderCapability{"codex": provider}
	catalog := opencodebridge.CatalogFromRegistry(registry).RestrictedTo("codex")
	if selected, ok := catalog.StrongPrimary("codex"); !ok || selected.ID != model.ID || selected.DefaultEffort != model.DefaultEffort {
		fail("VERIFIED_STRONG_SELECTION_UNAVAILABLE")
	}
	b, results := adaptiveInferenceBridge(t, f, environment, catalog)
	runtimeDir, err := f.store.RuntimeDir(f.id)
	if err != nil {
		fail("RUNTIME_UNAVAILABLE")
	}
	var selectedMu sync.Mutex
	selectedFrontend := ""
	front, err := codexfrontend.Start(ctx, codexfrontend.Options{Binary: binary, Directory: f.root, RuntimeDir: runtimeDir, NativeHome: filepath.Join(runtimeDir, "native-tui-history"), SessionID: f.id, Bridge: b, Environment: environment,
		ThreadSelected: func(id string) error {
			selectedMu.Lock()
			defer selectedMu.Unlock()
			if selectedFrontend != "" && selectedFrontend != id {
				return errors.New("unexpected frontend conversation switch")
			}
			_, err := f.store.Update(f.id, func(v *session.Session) error { v.SetFrontend("codex", id); return nil })
			if err == nil {
				selectedFrontend = id
			}
			return err
		},
	})
	if err != nil {
		fail("NATIVE_FRONTEND_START_FAILED")
	}
	defer front.Close()
	pty, err := adaptiveNativePTY(ctx, binary, f.root, front.Args(), front.Environment())
	if err != nil {
		fail("NATIVE_TUI_START_FAILED")
	}
	defer pty.close()
	defer func() {
		selectedMu.Lock()
		selected := selectedFrontend != ""
		selectedMu.Unlock()
		row.Startup = pty.startup(model, selected)
	}()
	approved := map[string]bool{}
	approveRead := func() error {
		pending, err := b.PendingDecisions(ctx)
		if err != nil {
			return errors.New("APPROVAL_LIST_FAILED")
		}
		for _, p := range pending {
			if approved[p.ID] {
				continue
			}
			if !strings.HasPrefix(p.ID, "ext_") || p.Description != "ivoai-code · code_inspect" {
				return errors.New("UNEXPECTED_APPROVAL_SCOPE")
			}
			if b.ReplyDecision(ctx, p.ID, true) != nil {
				return errors.New("CODE_READ_APPROVAL_FAILED")
			}
			approved[p.ID] = true
			row.Approvals++
		}
		return nil
	}
	wait := func(ready func() bool, bound time.Duration) {
		t.Helper()
		deadline := time.NewTimer(bound)
		defer deadline.Stop()
		tick := time.NewTicker(50 * time.Millisecond)
		defer tick.Stop()
		for {
			if ready() {
				return
			}
			select {
			case <-pty.done:
				fail("NATIVE_TUI_EXITED_EARLY")
			case <-ctx.Done():
				fail("INTEGRATION_DEADLINE")
			case <-deadline.C:
				fail("NATIVE_STATE_DEADLINE")
			case <-tick.C:
				if err := approveRead(); err != nil {
					fail(err.Error())
				}
			}
		}
	}
	row.Phase = "native_startup"
	wait(func() bool {
		selectedMu.Lock()
		selected := selectedFrontend != ""
		selectedMu.Unlock()
		return pty.startup(model, selected).ready()
	}, 25*time.Second)
	submit := func(text string) error {
		if err := pty.send(text); err != nil {
			return err
		}
		// Match the native PTY smoke's separation of text input and Enter;
		// sending both in one burst can trigger the TUI's paste heuristics.
		select {
		case <-time.After(250 * time.Millisecond):
			return pty.send("\r")
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	firstNative, firstReceipt, firstFrontend := "", "", ""
	for i, prompt := range []string{"Explique como funciona a autenticação deste projeto.", "Qual função faz essa verificação? Informe o nome e o arquivo."} {
		row.Phase = "turn_" + strconv.Itoa(i+1) + "_metadata"
		if submit(prompt) != nil {
			fail("NATIVE_INPUT_FAILED")
		}
		wait(func() bool {
			v, err := f.store.Get(f.id)
			if err != nil || len(v.TurnAttempts) != i+1 || v.PrimaryTurn == nil || v.PrimaryTurn.Mode != core.PrimaryClosed {
				return false
			}
			var attempt opencodebridge.TurnAttempt
			return json.Unmarshal(v.TurnAttempts[i], &attempt) == nil && attempt.State == "completed" && attempt.EndedAt != nil
		}, 110*time.Second)
		v, err := f.store.Get(f.id)
		if err != nil {
			fail("PERSISTED_SESSION_UNAVAILABLE")
		}
		var result core.ExecutorResult
		select {
		case result = <-results:
		case <-ctx.Done():
			fail("BACKEND_RESULT_UNAVAILABLE")
		}
		row.Attempts, row.DAG, row.Workers = len(v.TurnAttempts), v.PlanID != "", len(v.Workers)
		row.BackendExitZero = result.Trace != nil && result.Trace.ExitCode == 0 && result.Trace.Signal == "" && result.Trace.Completion && result.Trace.FinalResponse
		selectedMu.Lock()
		frontend := selectedFrontend
		selectedMu.Unlock()
		if i == 0 {
			firstNative, firstReceipt, firstFrontend = result.ExecutorSessionID, v.PrimaryTurn.ID, frontend
		}
		row.SameFrontend = frontend != "" && frontend != "fresh_frontend" && frontend == firstFrontend && v.FrontendSessionID == frontend && v.PrimaryTurn.FrontendSessionID == frontend
		row.SameNativeThread = firstNative != "" && result.ExecutorSessionID == firstNative && v.ExecutorSessionID == firstNative
		if row.DAG || row.Workers != 0 || !row.BackendExitZero || !row.SameFrontend || !row.SameNativeThread || !result.Policy.NoReplay || !result.Policy.NoAutomaticFailover || (i == 1 && v.PrimaryTurn.ID == firstReceipt) {
			fail("CORE_LIFECYCLE_OR_ISOLATION_FAILED")
		}
		row.ClosedReceipts++
		row.CompletedTurns++
		if result.Trace.Usage != nil {
			row.OfficialUsage = append(row.OfficialUsage, result.Trace.Usage)
		}
		row.Phase = "turn_" + strconv.Itoa(i+1) + "_render"
		// Metadata is the completion oracle. Rendering of the fixture answer
		// additionally proves the result reached the real native interface.
		wait(func() bool {
			if i == 0 {
				return pty.contains("Authenticate") && pty.contains("fixture-allowed")
			}
			return pty.contains("auth.go")
		}, 15*time.Second)
		if front.TurnError() != nil {
			fail("NATIVE_TURN_ERROR")
		}
		// Permit the trailing native turn/completed event to update the composer.
		select {
		case <-time.After(350 * time.Millisecond):
		case <-ctx.Done():
			fail("INTEGRATION_DEADLINE")
		}
	}
	row.Phase = "normal_tui_exit"
	if submit("/exit") != nil {
		fail("NORMAL_EXIT_INPUT_FAILED")
	}
	select {
	case <-pty.done:
		row.FrontendExitZero = pty.err == nil
	case <-time.After(15 * time.Second):
		fail("NORMAL_NATIVE_EXIT_NOT_OBSERVED")
	case <-ctx.Done():
		fail("INTEGRATION_DEADLINE")
	}
	if !row.FrontendExitZero {
		fail("NATIVE_EXIT_NONZERO")
	}
	row.Phase = "owned_app_server_shutdown"
	front.Close()
	row.CleanShutdown = front.ShutdownError() == nil
	if !row.CleanShutdown {
		fail("OWNED_APP_SERVER_SHUTDOWN_FAILED")
	}
	final, err := f.store.Get(f.id)
	if err != nil || len(final.TurnAttempts) != 2 || len(final.FrontendRequests) != 2 || final.PlanID != "" || len(final.Tasks) != 0 || len(final.Workers) != 0 || final.PrimaryTurn == nil || final.PrimaryTurn.Mode != core.PrimaryClosed {
		fail("FINAL_SESSION_OR_REPLAY_INVARIANT_FAILED")
	}
	row.Phase = "completed"
	row.Outcome = "PASS"
}

// Terminal content, including model responses, stays bounded in memory. No
// transcript, command arguments, environment, prompt, or response is logged.
type adaptiveNativeTerminal struct {
	master  *os.File
	cmd     *exec.Cmd
	done    chan struct{}
	err     error // published by closing done
	mu      sync.Mutex
	write   sync.Mutex
	output  []byte
	queries adaptiveNativeQueries
}

func adaptiveNativePTY(ctx context.Context, binary, directory string, args, environment []string) (*adaptiveNativeTerminal, error) {
	master, err := os.OpenFile("/dev/ptmx", os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		return nil, err
	}
	ok := false
	defer func() {
		if !ok {
			_ = master.Close()
		}
	}()
	if err := unix.IoctlSetPointerInt(int(master.Fd()), unix.TIOCSPTLCK, 0); err != nil {
		return nil, err
	}
	number, err := unix.IoctlGetInt(int(master.Fd()), unix.TIOCGPTN)
	if err != nil {
		return nil, err
	}
	slave, err := os.OpenFile("/dev/pts/"+strconv.Itoa(number), os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		return nil, err
	}
	defer slave.Close()
	if err := unix.IoctlSetWinsize(int(master.Fd()), unix.TIOCSWINSZ, &unix.Winsize{Row: 45, Col: 140}); err != nil {
		return nil, err
	}
	cmd := exec.CommandContext(ctx, binary, args...)
	// This PTY emulates an xterm, independently of the noninteractive test
	// runner's TERM. Do not alter the operator's or backend's environment.
	terminalEnvironment := make([]string, 0, len(environment)+1)
	for _, entry := range environment {
		if !strings.HasPrefix(entry, "TERM=") {
			terminalEnvironment = append(terminalEnvironment, entry)
		}
	}
	cmd.Dir, cmd.Env = directory, append(terminalEnvironment, "TERM=xterm-256color")
	cmd.Stdin, cmd.Stdout, cmd.Stderr = slave, slave, slave
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true, Setctty: true, Ctty: 0}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	p := &adaptiveNativeTerminal{master: master, cmd: cmd, done: make(chan struct{})}
	go func() { p.err = cmd.Wait(); close(p.done) }()
	go func() {
		buffer := make([]byte, 32768)
		queryTail := ""
		for {
			n, err := master.Read(buffer)
			if n > 0 {
				chunk := buffer[:n]
				var replies []string
				var queries adaptiveNativeQueries
				queryTail, replies, queries = adaptiveNativeQueryReplies(queryTail, string(chunk))
				p.mu.Lock()
				remaining := (4 << 20) - len(p.output)
				if remaining > 0 {
					p.output = append(p.output, chunk[:min(n, remaining)]...)
				}
				p.queries.Cursor += queries.Cursor
				p.queries.Status += queries.Status
				p.queries.Device += queries.Device
				p.queries.Keyboard += queries.Keyboard
				p.mu.Unlock()
				for _, reply := range replies {
					_ = p.send(reply)
				}
			}
			if err != nil {
				return
			}
		}
	}()
	ok = true
	return p, nil
}

func (p *adaptiveNativeTerminal) send(value string) error {
	p.write.Lock()
	defer p.write.Unlock()
	_, err := io.WriteString(p.master, value)
	return err
}

var adaptiveNativeANSI = regexp.MustCompile("\x1b\\[[0-?]*[ -/]*[@-~]")

func (p *adaptiveNativeTerminal) contains(expected string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	compact := func(text string) string {
		return strings.Join(strings.Fields(adaptiveNativeANSI.ReplaceAllString(text, "")), "")
	}
	return strings.Contains(compact(string(p.output)), compact(expected))
}

func (p *adaptiveNativeTerminal) close() {
	// Emergency cleanup is used only after a failed/incomplete test. A passing
	// receipt requires /exit, Wait()==nil, and orderly Facade.Close above.
	select {
	case <-p.done:
	default:
		_ = syscall.Kill(-p.cmd.Process.Pid, syscall.SIGTERM)
		select {
		case <-p.done:
		case <-time.After(2 * time.Second):
			_ = syscall.Kill(-p.cmd.Process.Pid, syscall.SIGKILL)
			<-p.done
		}
	}
	_ = p.master.Close()
}

// Startup classifications are a bounded diagnostic, not a terminal transcript
// or proof of inference. Unknown modals remain a timeout; no dialog is accepted.
type adaptiveNativeStartup struct {
	TerminalOutput  bool                  `json:"terminal_output"`
	TerminalBytes   int                   `json:"terminal_bytes"`
	ThreadSelected  bool                  `json:"thread_selected"`
	ModelDisplay    bool                  `json:"model_display_seen"`
	ModelUpstream   bool                  `json:"model_upstream_seen"`
	Composer        bool                  `json:"composer_seen"`
	TrustDialog     bool                  `json:"trust_dialog_seen"`
	UpdateDialog    bool                  `json:"update_dialog_seen"`
	AuthDialog      bool                  `json:"auth_dialog_seen"`
	ConnectionError bool                  `json:"connection_error_seen"`
	TerminalWarning bool                  `json:"terminal_warning_seen"`
	Queries         adaptiveNativeQueries `json:"terminal_queries"`
}

type adaptiveNativeQueries struct {
	Cursor   int `json:"cursor"`
	Status   int `json:"status"`
	Device   int `json:"device_attributes"`
	Keyboard int `json:"keyboard"`
}

func (s adaptiveNativeStartup) ready() bool {
	return !s.TrustDialog && !s.UpdateDialog && !s.AuthDialog && !s.ConnectionError && !s.TerminalWarning && (s.ModelDisplay || s.ModelUpstream) && (s.ThreadSelected || s.Composer)
}

func (p *adaptiveNativeTerminal) startup(model opencodebridge.ModelSpec, selected bool) adaptiveNativeStartup {
	p.mu.Lock()
	defer p.mu.Unlock()
	text := strings.ToLower(strings.Join(strings.Fields(adaptiveNativeANSI.ReplaceAllString(string(p.output), "")), ""))
	seen := func(values ...string) bool {
		for _, value := range values {
			value = strings.ToLower(strings.Join(strings.Fields(value), ""))
			if value != "" && strings.Contains(text, value) {
				return true
			}
		}
		return false
	}
	return adaptiveNativeStartup{
		TerminalOutput: len(p.output) != 0, TerminalBytes: len(p.output), ThreadSelected: selected,
		ModelDisplay: seen(strings.TrimPrefix(model.Name, "Codex · ")), ModelUpstream: seen(model.UpstreamModel),
		Composer:        seen("? for shortcuts", "›"),
		TrustDialog:     seen("Do you trust the contents of this directory", "Do you trust this folder", "Do you trust the files in this folder", "Trust this directory"),
		UpdateDialog:    seen("Update available", "Update now", "Skip until next version", "Remind me later"),
		AuthDialog:      seen("Sign in with ChatGPT", "Sign in to Codex", "Paste your API key", "Log in to Codex"),
		ConnectionError: seen("Failed to connect", "Connection refused", "WebSocket handshake failed"),
		TerminalWarning: seen("TERM is set to dumb", "terminal does not support", "not a supported terminal"), Queries: p.queries,
	}
}

// Recognize only standard queries emitted by the TUI. Tail handling permits
// queries split across arbitrary PTY reads and avoids replying twice. Report
// no advanced keyboard features and only basic VT100 device attributes.
func adaptiveNativeQueryReplies(tail, chunk string) (string, []string, adaptiveNativeQueries) {
	text := tail + chunk
	var replies []string
	var counts adaptiveNativeQueries
	queries := []struct{ request, reply, kind string }{
		{"\x1b[6n", "\x1b[1;1R", "cursor"}, {"\x1b[?6n", "\x1b[?1;1R", "cursor"},
		{"\x1b[5n", "\x1b[0n", "status"},
		{"\x1b[c", "\x1b[?1;2c", "device"}, {"\x1b[0c", "\x1b[?1;2c", "device"},
		{"\x1b[>c", "\x1b[>0;0;0c", "device"}, {"\x1b[>0c", "\x1b[>0;0;0c", "device"},
		{"\x1b[?u", "\x1b[?0u", "keyboard"},
	}
	for offset := 0; offset < len(text); offset++ {
		for _, query := range queries {
			if !strings.HasPrefix(text[offset:], query.request) {
				continue
			}
			end := offset + len(query.request)
			if end > len(tail) {
				replies = append(replies, query.reply)
				switch query.kind {
				case "cursor":
					counts.Cursor++
				case "status":
					counts.Status++
				case "device":
					counts.Device++
				case "keyboard":
					counts.Keyboard++
				}
			}
			offset = end - 1
			break
		}
	}
	if len(text) > 32 {
		text = text[len(text)-32:]
	}
	return text, replies, counts
}

func TestAdaptiveNativeStartupUsesObservedModelAndRejectsKnownModals(t *testing.T) {
	model := opencodebridge.ModelSpec{Name: "Codex · Observed model", UpstreamModel: "observed-model"}
	for _, output := range []string{"observed-model ›", "Observed model ? for shortcuts"} {
		terminal := &adaptiveNativeTerminal{output: []byte(output)}
		if !terminal.startup(model, false).ready() {
			t.Fatal("real model and composer were not recognized")
		}
	}
	terminal := &adaptiveNativeTerminal{output: []byte("observed-model Do you trust the contents of this directory? ›")}
	state := terminal.startup(model, true)
	if !state.TrustDialog || state.ready() {
		t.Fatal("trust dialog was mistaken for an admitted composer")
	}
	terminal.output = []byte("observed-model unrelated private terminal content")
	if terminal.startup(model, false).ready() {
		t.Fatal("model label alone certified startup")
	}
	raw, err := json.Marshal(terminal.startup(model, true))
	if err != nil || strings.Contains(string(raw), "private terminal content") || strings.Contains(string(raw), "observed-model") {
		t.Fatal("startup diagnostic exported terminal text")
	}
}

func TestAdaptiveNativeQueriesSurviveReadBoundariesWithoutReplay(t *testing.T) {
	var tail string
	var replies []string
	var total adaptiveNativeQueries
	for _, chunk := range []string{"prefix\x1b[", "6n", "suffix\x1b[?", "u\x1b[", "c"} {
		var responses []string
		var counts adaptiveNativeQueries
		tail, responses, counts = adaptiveNativeQueryReplies(tail, chunk)
		replies = append(replies, responses...)
		total.Cursor += counts.Cursor
		total.Device += counts.Device
		total.Keyboard += counts.Keyboard
	}
	if strings.Join(replies, "|") != "\x1b[1;1R|\x1b[?0u|\x1b[?1;2c" || total.Cursor != 1 || total.Device != 1 || total.Keyboard != 1 {
		t.Fatal("split terminal queries were missed or replayed")
	}
}
