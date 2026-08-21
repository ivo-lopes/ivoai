package orchestration

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ivo-lopes/ivoai/internal/platform"
)

type controlRunner struct {
	calls []controlCall
}

type controlCall struct {
	args []string
	env  []string
	dir  string
}

func (r *controlRunner) LookPath(string) (string, error) { return "/managed/ruflo", nil }
func (r *controlRunner) Run(_ context.Context, _ string, args []string, options platform.RunOptions) (platform.Result, error) {
	r.calls = append(r.calls, controlCall{args: append([]string(nil), args...), env: append([]string(nil), options.Env...), dir: options.Dir})
	joined := strings.Join(args, " ")
	switch {
	case joined == "--version":
		return platform.Result{Stdout: "ruflo v3.38.12"}, nil
	case strings.HasPrefix(joined, "swarm init "):
		return platform.Result{Stdout: "Swarm ID: swarm-fixture-123"}, nil
	case joined == "swarm status":
		return platform.Result{Stdout: "swarm-fixture-123 active"}, nil
	case strings.HasPrefix(joined, "task create "):
		return platform.Result{Stdout: "Created task task-fixture-123"}, nil
	default:
		return platform.Result{}, nil
	}
}

func TestControlPlaneProvesSwarmAndRegistersOpaqueLifecycleWithoutProviders(t *testing.T) {
	runner := &controlRunner{}
	profileDir := t.TempDir()
	manager := Manager{Runner: runner, Binary: "/managed/ruflo", ProfileDir: profileDir}
	t.Setenv("IVOAI_TEST_MODE", "1")
	if err := manager.Configure(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	runtimeDir := filepath.Join(t.TempDir(), "runtime")
	control := ControlPlane{Manager: manager, RuntimeDir: runtimeDir}
	swarm, err := control.Initialize(context.Background(), 2)
	if err != nil {
		t.Fatal(err)
	}
	if swarm.ID != "swarm-fixture-123" || !swarm.Healthy || !swarm.Status.SafeMode {
		t.Fatalf("swarm=%+v", swarm)
	}
	task, err := control.RegisterLifecycle(context.Background(), "primary", "sess_0123456789abcdef0123456789abcdef")
	if err != nil || task != "task-fixture-123" {
		t.Fatalf("task=%q err=%v", task, err)
	}
	for _, call := range runner.calls {
		if call.dir != "" && call.dir != runtimeDir {
			t.Fatalf("unexpected runtime dir %q", call.dir)
		}
		joined := strings.Join(call.env, "\n")
		for _, prohibited := range providerVariables {
			if strings.Contains(joined, prohibited+"=") {
				t.Fatalf("provider credential variable reached Ruflo: %s", prohibited)
			}
		}
		if strings.Contains(strings.Join(call.args, " "), "agent_execute") || strings.Contains(strings.Join(call.args, " "), "agent spawn") {
			t.Fatalf("provider execution command used: %#v", call.args)
		}
	}
	info, err := os.Stat(runtimeDir)
	if err != nil || info.Mode().Perm() != 0o700 {
		t.Fatalf("runtime directory mode: %v %v", info, err)
	}
}

func TestControlPlaneRefusesTamperedProviderProfile(t *testing.T) {
	runner := &controlRunner{}
	manager := Manager{Runner: runner, Binary: "/managed/ruflo", ProfileDir: t.TempDir()}
	t.Setenv("IVOAI_TEST_MODE", "1")
	if err := manager.Configure(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(manager.profilePath(), []byte(`{"version":2,"tools":[],"provider_execution":true,"durable_memory":false}`), 0o600); err != nil {
		t.Fatal(err)
	}
	control := ControlPlane{Manager: manager, RuntimeDir: filepath.Join(t.TempDir(), "runtime")}
	if _, err := control.Initialize(context.Background(), 2); err == nil {
		t.Fatal("provider-enabled profile was accepted")
	}
}
