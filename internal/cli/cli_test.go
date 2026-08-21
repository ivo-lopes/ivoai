package cli

import (
	"bufio"
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ivo-lopes/ivoai/internal/app"
	"github.com/ivo-lopes/ivoai/internal/quota"
	"github.com/ivo-lopes/ivoai/internal/session"
	"github.com/ivo-lopes/ivoai/internal/terminalui"
)

func TestMenuCanExitAndDoctorJSON(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", root)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, "data"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(root, "state"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(root, "cache"))
	t.Setenv("IVOAI_TEST_MODE", "1")
	var out bytes.Buffer
	a, err := app.New("v0.1.0", strings.NewReader("0\n"), &out, &out)
	if err != nil {
		t.Fatal(err)
	}
	if err := Run(context.Background(), a, nil); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "Personal AI runtime") || !strings.Contains(out.String(), "Agents") {
		t.Fatal("menu missing")
	}
	out.Reset()
	if err := a.Setup(context.Background()); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if err := Run(context.Background(), a, []string{"doctor", "--json"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), `"overall": "READY"`) || !strings.Contains(out.String(), `"test_mode": true`) {
		t.Fatalf("doctor JSON: %s", out.String())
	}
	if !strings.Contains(out.String(), `"ruflo"`) {
		t.Fatalf("doctor JSON omits Ruflo: %s", out.String())
	}
}

func TestUserErrorRedactsSecrets(t *testing.T) {
	got := UserError(assertError("Authorization: Bearer secret-token"))
	if strings.Contains(got, "secret-token") {
		t.Fatal("secret leaked")
	}
}

func TestHierarchicalPlainMenuCanRunStatusAndReturn(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", root)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, "data"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(root, "state"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(root, "cache"))
	t.Setenv("IVOAI_TEST_MODE", "1")
	var output bytes.Buffer
	a, err := app.New("v0.1.0", strings.NewReader("2\n1\n0\n0\n"), &output, &output)
	if err != nil {
		t.Fatal(err)
	}
	if err := a.Setup(context.Background()); err != nil {
		t.Fatal(err)
	}
	output.Reset()
	if err := Run(context.Background(), a, nil); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "Dashboard") || !strings.Contains(output.String(), "Overall: READY") || !strings.Contains(output.String(), "Agents") {
		t.Fatalf("menu output: %s", output.String())
	}
}

func TestPublicMenuActionCoverageIsUnique(t *testing.T) {
	ids := PublicMenuActionIDs()
	seen := make(map[string]bool, len(ids))
	for _, id := range ids {
		if id == "" || seen[id] {
			t.Fatalf("invalid or duplicate menu action %q", id)
		}
		seen[id] = true
	}
	for _, required := range []string{"auto", "setup", "connect.chatgpt", "connect.claude", "connect.server", "launch.codex", "launch.claude", "session.direct.codex", "session.orchestrated.claude", "session.monitor", "session.stop", "server.restore", "remote.doctor", "uninstall"} {
		if !seen[required] {
			t.Fatalf("public command missing from menu: %s", required)
		}
	}
}

func TestDestructiveConfirmationRequiresExactPhrase(t *testing.T) {
	var output bytes.Buffer
	a := &app.App{In: strings.NewReader("remove\nREMOVE\n"), Out: &output, Err: &output}
	session := &menuSession{app: a, reader: bufio.NewReader(a.In)}
	if session.confirm("REMOVE") {
		t.Fatal("case-insensitive destructive confirmation was accepted")
	}
	if !session.confirm("REMOVE") {
		t.Fatal("exact destructive confirmation was refused")
	}
}

func TestMenuFormValidation(t *testing.T) {
	valid := []struct {
		value    string
		validate func(string) error
	}{
		{"https://ai.example.com", validateHTTPSURL},
		{"ivoai-enroll_1234567890abcdef_abcdefghijklmnopqrstuvwxyz", validateEnrollmentCode},
		{"10m", validatePositiveDuration},
		{"context-main", validateIdentifier},
		{"filesystem", validateConnectorType},
		{"/var/lib/ivoai/backups/archive.tar.gz", validateAbsolutePath},
		{"127.0.0.1:7744", validateListenAddress},
		{"192.0.2.0/24", validateCIDR},
	}
	for _, test := range valid {
		if err := test.validate(test.value); err != nil {
			t.Fatalf("valid value %q rejected: %v", test.value, err)
		}
	}
	invalid := []struct {
		value    string
		validate func(string) error
	}{
		{"http://ai.example.com", validateHTTPSURL},
		{"https://user:secret@ai.example.com", validateHTTPSURL},
		{"ivoai-enroll short", validateEnrollmentCode},
		{"0s", validatePositiveDuration},
		{"../escape", validateAbsolutePath},
		{"all", validateConnectorType},
		{"not-a-cidr", validateCIDR},
	}
	for _, test := range invalid {
		if err := test.validate(test.value); err == nil {
			t.Fatalf("invalid value %q accepted", test.value)
		}
	}
}

func TestMenuAvailabilityAdaptsToHostAndServerState(t *testing.T) {
	if read, mutation := serverRestrictions("darwin", 0, true); read != "Linux server only" || mutation != read {
		t.Fatalf("non-Linux restrictions: read=%q mutation=%q", read, mutation)
	}
	if read, mutation := serverRestrictions("linux", 1000, false); read != "local server not installed" || mutation != "requires root" {
		t.Fatalf("desktop restrictions: read=%q mutation=%q", read, mutation)
	}
	if read, mutation := serverRestrictions("linux", 0, true); read != "" || mutation != "" {
		t.Fatalf("root server unexpectedly restricted: read=%q mutation=%q", read, mutation)
	}
	if reason := serverSetupRestriction("linux", 1000); reason != "requires root" {
		t.Fatalf("setup restriction=%q", reason)
	}
}

func TestDoctorJSONDoesNotEmitProgress(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", root)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, "data"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(root, "state"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(root, "cache"))
	t.Setenv("IVOAI_TEST_MODE", "1")
	var stdout, stderr bytes.Buffer
	a, err := app.New("v0.1.0", strings.NewReader(""), &stdout, &stderr)
	if err != nil {
		t.Fatal(err)
	}
	if err := a.Setup(context.Background()); err != nil {
		t.Fatal(err)
	}
	stdout.Reset()
	stderr.Reset()
	if err := Run(context.Background(), a, []string{"doctor", "--json"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), `"overall"`) || stderr.Len() != 0 {
		t.Fatalf("stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
}

func TestCommandProgressClassification(t *testing.T) {
	for _, test := range []struct {
		args    []string
		enabled bool
	}{
		{[]string{"setup"}, true},
		{[]string{"doctor"}, true},
		{[]string{"doctor", "--json"}, false},
		{[]string{"server", "backup"}, true},
		{[]string{"server", "logs"}, false},
		{[]string{"codex"}, false},
	} {
		_, enabled := commandProgress(test.args)
		if enabled != test.enabled {
			t.Fatalf("commandProgress(%v)=%t want %t", test.args, enabled, test.enabled)
		}
	}
}

func TestCommandHeaderExcludesMachineAndProcessEntrypoints(t *testing.T) {
	for _, test := range []struct {
		args []string
		want bool
	}{
		{[]string{"status"}, true},
		{[]string{"doctor", "--json"}, false},
		{[]string{"codex"}, false},
		{[]string{"claude"}, false},
		{[]string{"_register-install"}, false},
		{[]string{"_orchestrator-serve", "--session", "x"}, false},
		{[]string{"monitor", "--json"}, false},
		{[]string{"server", "gateway", "serve"}, false},
		{[]string{"server", "context", "serve"}, false},
	} {
		if got := commandHeaderEnabled(test.args); got != test.want {
			t.Fatalf("commandHeaderEnabled(%v)=%t want %t", test.args, got, test.want)
		}
	}
}

func TestMonitorRenderingFitsNarrowTerminalAndJSONHasNoANSI(t *testing.T) {
	now := time.Now().UTC()
	value := session.Session{SessionID: "sess_0123456789abcdef0123456789abcdef", StartedAt: now, UpdatedAt: now, Mode: session.ModeOrchestrated, PrimaryExecutor: "codex", WorkingDirectory: "/tmp/project", PrimaryModel: session.ModelInfo{Name: strings.Repeat("model", 20), Source: session.ModelConfigured}, RufloEnabled: true, RufloHealthy: true, RufloSafeMode: true, SwarmID: "swarm-fixture-with-a-long-identifier", Workers: []session.Worker{}, MaxWorkers: 2, State: session.StateRunning}
	var human bytes.Buffer
	renderMonitorSized(&human, value, 40)
	for _, line := range strings.Split(human.String(), "\n") {
		if terminalui.CellWidth(line) > 40 {
			t.Fatalf("monitor overflow (%d cells): %q", terminalui.CellWidth(line), line)
		}
	}
	var machine bytes.Buffer
	if err := writeSessions(&machine, []session.Session{value}, true); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(machine.String(), "\x1b[") || !strings.Contains(machine.String(), `"session_id"`) {
		t.Fatalf("invalid machine output: %q", machine.String())
	}
}

func TestAutomaticMonitorAlwaysShowsWeeklyAndMonthlyWithSourceAndFreshness(t *testing.T) {
	now := time.Unix(100, 0).UTC()
	reset := time.Unix(200, 0).UTC()
	value := session.Session{
		SessionID: "sess_0123456789abcdef0123456789abcdef", StartedAt: now, UpdatedAt: now,
		Mode: session.ModeAuto, Auto: true, InitialPlanner: "codex", CurrentPrimary: "claude", PrimaryExecutor: "claude",
		PrimaryModel: session.UnknownModel(), WorkingDirectory: "/tmp/project", Workers: []session.Worker{}, MaxWorkers: 2,
		State: session.StateRunning, CurrentPhase: "conversation", Quota: map[quota.Provider]quota.ProviderQuota{
			quota.ProviderCodex:  {Provider: quota.ProviderCodex, Eligible: true, Windows: []quota.Window{{Kind: quota.KindWeekly, RemainingPercent: 71, UsedPercent: 29, ResetsAt: &reset, Source: "codex app-server", ObservedAt: now, Available: true, Authoritative: true}}},
			quota.ProviderClaude: {Provider: quota.ProviderClaude, Eligible: true},
		},
	}
	var output bytes.Buffer
	renderMonitorSized(&output, value, 120)
	text := output.String()
	for _, expected := range []string{"Automatic Session", "Conversation", "Claude Code", "Failovers", "Codex", "Claude Code", "Weekly", "Monthly", "71% remaining", "codex app-server", "1970-01-01T00:01:40Z", "N/A / not exposed"} {
		if !strings.Contains(text, expected) {
			t.Fatalf("automatic monitor missing %q:\n%s", expected, text)
		}
	}
	for _, line := range strings.Split(text, "\n") {
		if terminalui.CellWidth(line) > 120 {
			t.Fatalf("automatic monitor overflow: %q", line)
		}
	}
}

type assertError string

func (e assertError) Error() string { return string(e) }
