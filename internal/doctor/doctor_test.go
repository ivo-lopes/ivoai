package doctor

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/ivo-lopes/ivoai/internal/config"
	"github.com/ivo-lopes/ivoai/internal/platform"
)

type statusRunner struct {
	output string
}

func (r statusRunner) LookPath(name string) (string, error) { return "/managed/" + name, nil }
func (r statusRunner) Run(_ context.Context, _ string, args []string, _ platform.RunOptions) (platform.Result, error) {
	if len(args) == 1 && args[0] == "--version" {
		return platform.Result{Stdout: "fixture 1.0.0"}, nil
	}
	return platform.Result{Stdout: r.output}, nil
}

func TestAgentAuthDoesNotTrustSuccessfulNegativeStatus(t *testing.T) {
	doctor := Doctor{Runner: statusRunner{output: `{"loggedIn": false}`}}
	status := doctor.agent(context.Background(), "codex", []string{"login", "status"}, config.ComponentState{Installed: true, Path: "/managed/codex"})
	if status.Authenticated {
		t.Fatalf("negative JSON status reported authenticated: %#v", status)
	}
}

func TestHooksInstalledRequiresMaterializedAssets(t *testing.T) {
	dir := t.TempDir()
	if hooksInstalled(dir) {
		t.Fatal("empty hooks directory reported installed")
	}
	if err := os.WriteFile(filepath.Join(dir, "session-start.sh"), []byte("#!/bin/sh\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	if !hooksInstalled(dir) {
		t.Fatal("hook asset was not detected")
	}
}

func TestServerDoctorUsesLiveProtocolAndRefusesCrossOriginRedirect(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"protocol_version":1,"health_endpoint":"/health","ready_endpoint":"/ready"}`))
	}))
	defer target.Close()
	redirector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusFound)
	}))
	defer redirector.Close()
	result := (Doctor{}).server(context.Background(), config.Connection{Status: "connected", URL: redirector.URL, Protocol: 1})
	if result.Reachable {
		t.Fatal("doctor followed a cross-origin redirect")
	}

	mismatch := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"protocol_version":99,"health_endpoint":"/health","ready_endpoint":"/ready"}`))
	}))
	defer mismatch.Close()
	result = (Doctor{HTTPClient: mismatch.Client()}).server(context.Background(), config.Connection{Status: "connected", URL: mismatch.URL, Protocol: 1})
	if !result.Reachable || result.ProtocolCompatible {
		t.Fatalf("doctor trusted persisted protocol instead of discovery: %#v", result)
	}
}
