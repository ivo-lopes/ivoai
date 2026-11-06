package app

import (
	"context"
	"golang.org/x/sys/cpu"
	"os"
	"runtime"
	"testing"
)

func TestAntigravityCPUAdmissionRequiresPCLMULQDQ(t *testing.T) {
	if err := antigravityCPUAdmission(false); err == nil || err.Error() != "ANTIGRAVITY_CPU_UNSUPPORTED" {
		t.Fatalf("unsupported CPU admission=%v", err)
	}
	if err := antigravityCPUAdmission(true); err != nil {
		t.Fatalf("PCLMULQDQ capability refused: %v", err)
	}
}

func TestReviewedAntigravityRejectsBeforeRuntimeDiscoveryWhenCPUUnsupported(t *testing.T) {
	// An empty PATH prevents ambient client discovery on every host. The actual
	// CPU feature state is read, never overridden. Core/QEMU separately covers
	// the real unsupported-host path before session/attempt creation.
	t.Setenv("PATH", "")
	path, info, err := reviewedAntigravity(context.Background())
	expected := "ANTIGRAVITY_RUNTIME_UNAVAILABLE"
	if runtime.GOOS != "linux" || runtime.GOARCH != "amd64" {
		expected = "ANTIGRAVITY_PLATFORM_UNCERTIFIED"
	} else if !cpu.X86.HasPCLMULQDQ {
		expected = "ANTIGRAVITY_CPU_UNSUPPORTED"
	}
	if path != "" || info != nil || err == nil || err.Error() != expected {
		t.Fatalf("preflight path=%q info=%v error=%v expected=%s", path, info, err, expected)
	}
	if os.Getenv("PATH") != "" {
		t.Fatal("runtime discovery changed the caller environment")
	}
}
