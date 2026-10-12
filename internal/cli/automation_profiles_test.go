package cli

import (
	"bufio"
	"bytes"
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/ivo-lopes/ivoai/internal/app"
	"github.com/ivo-lopes/ivoai/internal/config"
)

func TestAutomationProfilePreviewApplyRestorePreservesBoundaries(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", root)
	for _, key := range []string{"XDG_CONFIG_HOME", "XDG_DATA_HOME", "XDG_STATE_HOME", "XDG_CACHE_HOME"} {
		t.Setenv(key, filepath.Join(root, key))
	}
	var out bytes.Buffer
	a, err := app.New("test", strings.NewReader(""), &out, &out)
	if err != nil {
		t.Fatal(err)
	}
	original := config.Default()
	original.Orchestration.Auto.ProviderPreference = "claude"
	original.Orchestration.Auto.PlanExecution = "approve"
	original.Orchestration.Auto.WorkerCap = 4
	original.Connections.Servers = map[string]config.ServerProfile{"fixture": {ID: "srv_fixture", Alias: "fixture", Purpose: "research", URL: "https://fixture.invalid", Enabled: true}}
	if err := a.Store.Save(original); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(a.Store.Paths.Config)
	if err := runConfig(a, []string{"profile", "preview", "economic"}); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(a.Store.Paths.Config)
	if !bytes.Equal(before, after) {
		t.Fatal("preview mutated config")
	}
	if !strings.Contains(out.String(), "worker_cap") {
		t.Fatal("preview omitted effective delta")
	}
	for i := 0; i < 2; i++ {
		if err := runConfig(a, []string{"profile", "apply", "economic"}); err != nil {
			t.Fatal(err)
		}
	}
	applied, err := a.Store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if applied.Orchestration.Auto.WorkerCap != 1 || applied.Orchestration.Auto.ResolvedConcurrency() != "sequential" {
		t.Fatal("economic profile not applied")
	}
	if applied.Orchestration.Auto.ProviderPreference != "claude" || applied.Orchestration.Auto.PlanExecution != "approve" || !reflect.DeepEqual(applied.Connections.Servers, original.Connections.Servers) {
		t.Fatal("profile changed provider, policy or server scope")
	}
	if err := runConfig(a, []string{"profile", "restore"}); err != nil {
		t.Fatal(err)
	}
	restored, err := a.Store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(restored.Orchestration, original.Orchestration) {
		t.Fatal("restore did not recover original orchestration")
	}
	if err := runConfig(a, []string{"profile", "restore"}); err != nil {
		t.Fatal("restore not idempotent", err)
	}
}

func TestAutomationProfilesTUIConfirmationAndRestore(t *testing.T) {
	for _, input := range []string{"2\nno\n0\n", "2\nAPPLY\n9\nRESTORE\n0\n"} {
		t.Run(input, func(t *testing.T) {
			root := t.TempDir()
			t.Setenv("HOME", root)
			for _, key := range []string{"XDG_CONFIG_HOME", "XDG_DATA_HOME", "XDG_STATE_HOME", "XDG_CACHE_HOME"} {
				t.Setenv(key, filepath.Join(root, key))
			}
			var out bytes.Buffer
			a, err := app.New("fixture", strings.NewReader(input), &out, &out)
			if err != nil {
				t.Fatal(err)
			}
			original := config.Default()
			original.Orchestration.Auto.WorkerCap = 4
			if err := a.Store.Save(original); err != nil {
				t.Fatal(err)
			}
			s := &menuSession{ctx: context.Background(), app: a, reader: bufio.NewReader(a.In)}
			if _, err := s.automationProfiles(); err != nil {
				t.Fatal(err)
			}
			got, err := a.Store.Load()
			if err != nil || !reflect.DeepEqual(got.Orchestration, original.Orchestration) {
				t.Fatal("TUI failed confirmation/restore contract", err)
			}
			for _, want := range []string{"proposed", "current", "future sessions only", "Type APPLY"} {
				if !strings.Contains(out.String(), want) {
					t.Fatalf("missing preview %s", want)
				}
			}
		})
	}
}
