package cli

import (
	"bufio"
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ivo-lopes/ivoai/internal/app"
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
	a, err := app.New("v0.1.0", strings.NewReader("1\n1\n0\n0\n"), &output, &output)
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
	for _, required := range []string{"setup", "connect.chatgpt", "connect.claude", "connect.server", "launch.codex", "launch.claude", "server.restore", "remote.doctor", "uninstall"} {
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
		{[]string{"server", "gateway", "serve"}, false},
		{[]string{"server", "context", "serve"}, false},
	} {
		if got := commandHeaderEnabled(test.args); got != test.want {
			t.Fatalf("commandHeaderEnabled(%v)=%t want %t", test.args, got, test.want)
		}
	}
}

type assertError string

func (e assertError) Error() string { return string(e) }
