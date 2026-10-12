package doctor

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"time"

	"github.com/ivo-lopes/ivoai/internal/components"
	"github.com/ivo-lopes/ivoai/internal/config"
	"github.com/ivo-lopes/ivoai/internal/core"
	"github.com/ivo-lopes/ivoai/internal/platform"
	"github.com/ivo-lopes/ivoai/internal/supplychain"
)

type CodeIntelligence struct {
	Component         core.ComponentStatus `json:"component"`
	Integrity         string               `json:"integrity"`
	ArtifactSHA256    string               `json:"artifact_sha256,omitempty"`
	SignatureStatus   string               `json:"signature_status"`
	AttestationStatus string               `json:"attestation_status"`
	Node              string               `json:"private_node"`
	NodeIntegrity     string               `json:"node_integrity"`
	Libatomic         string               `json:"libatomic"`
	LandlockABI       int                  `json:"landlock_abi"`
	Seccomp           bool                 `json:"seccomp_action_available"`
	Coverage          string               `json:"coverage"`
}
type sandboxPrerequisites struct {
	LandlockABI  int
	Seccomp      bool
	Unprivileged bool
}

// This probe reads the managed inventory and kernel prerequisites. It never
// imports Graft, indexes a repository, installs anything or claims sandbox execution.
func (d Doctor) codeIntelligence(ctx context.Context, state config.State, catalog []components.Spec, sandbox sandboxPrerequisites) (ManagedComponent, CodeIntelligence) {
	c := state.Components["graft"]
	graft := ManagedComponent{Component: componentFromState(c)}
	diagnostic := CodeIntelligence{Integrity: "metadata_unavailable", SignatureStatus: "not_exposed", AttestationStatus: "not_exposed", Node: "unavailable", NodeIntegrity: "not_exposed", Libatomic: "unknown", LandlockABI: sandbox.LandlockABI, Seccomp: sandbox.Seccomp, Coverage: "unknown"}
	diagnostic.Component = core.ComponentStatus{ID: "code_intelligence", Implementation: "graft", Installed: graft.Installed, Managed: c.Managed, Health: core.HealthUnavailable, Lifecycle: core.LifecycleStopped, Capabilities: core.CapabilitySet{"code.structure": core.SupportSupported}, Compatibility: core.Compatibility{State: core.CompatibilityUnknown, Reason: "managed Graft metadata unavailable"}, Fallback: core.Fallback{Allowed: true, Reason: "direct inspection remains available"}}
	var spec *components.Spec
	for i := range catalog {
		if catalog[i].Name == "graft" {
			spec = &catalog[i]
			break
		}
	}
	if spec == nil {
		return graft, diagnostic
	}
	diagnostic.Integrity = "unavailable"
	diagnostic.Component.Compatibility.Reason = "verified managed Graft runtime unavailable"
	if !c.Managed || !graft.Installed || c.Version != spec.Version {
		return graft, diagnostic
	}
	expected, err := components.ResolvedSource(*spec)
	if err != nil {
		return graft, diagnostic
	}
	active, root, err := (supplychain.Manager{Root: filepath.Join(d.Store.Paths.DataDir, "supply-chain")}).Active("graft")
	if err != nil || !reflect.DeepEqual(active, expected) || c.Path != filepath.Join(root, spec.PayloadPath) {
		diagnostic.Integrity = "failed"
		return graft, diagnostic
	}
	diagnostic.Integrity = "verified"
	diagnostic.ArtifactSHA256 = active.Integrity.Digest
	diagnostic.SignatureStatus, diagnostic.AttestationStatus = active.Integrity.SignatureStatus, active.Integrity.AttestationStatus
	graft.Revision, graft.Source, graft.License, graft.TrustLevel = active.Revision, active.Source, active.License, active.Integrity.TrustLevel
	diagnostic.Component.Provenance = core.Provenance{Source: active.Source, Version: active.LogicalVersion, Path: c.Path}
	diagnostic.Component.Health = core.HealthDegraded
	diagnostic.Component.Compatibility.Reason = "private Node runtime unavailable"
	node := components.ManagedNodePath(d.Store.Paths.DataDir)
	info, err := os.Lstat(node)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&0111 == 0 || d.Runner == nil {
		return graft, diagnostic
	}
	result, err := d.Runner.Run(ctx, node, []string{"--version"}, platform.RunOptions{CleanEnv: true, Env: []string{"PATH=/usr/bin:/bin"}, Timeout: 10 * time.Second})
	if err != nil || result.ExitCode != 0 {
		diagnostic.Node = "failed"
		if strings.Contains(result.Stderr, "libatomic.so.1") {
			diagnostic.Libatomic = "unavailable"
			diagnostic.Component.Compatibility.Reason = "private Node requires libatomic.so.1; install libatomic1 on Debian/Ubuntu"
		}
		return graft, diagnostic
	}
	// The private path is version-qualified by the component installer. No
	// per-file Node receipt exists today; do not claim checksum verification.
	expectedNodeVersion := "v" + strings.TrimPrefix(filepath.Base(filepath.Dir(filepath.Dir(node))), "node-")
	if strings.TrimSpace(result.Stdout) != expectedNodeVersion {
		diagnostic.Node = "version_mismatch"
		return graft, diagnostic
	}
	diagnostic.Node = "available"
	diagnostic.Libatomic = "loader_satisfied"
	diagnostic.Component.Compatibility.Reason = "sandbox prerequisites unavailable; execution remains unverified"
	if sandbox.LandlockABI < 1 || !sandbox.Seccomp || !sandbox.Unprivileged {
		diagnostic.Component.Compatibility.State = core.CompatibilityIncompatible
		return graft, diagnostic
	}
	diagnostic.Component.Available = true
	diagnostic.Component.Active = true
	diagnostic.Component.Health = core.HealthUnknown
	diagnostic.Component.Compatibility.Reason = "kernel prerequisites available; sandbox execution and repository coverage unverified"
	return graft, diagnostic
}
