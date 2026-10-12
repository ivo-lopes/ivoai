package codeintel

import (
	"context"
	"github.com/ivo-lopes/ivoai/internal/core"
	"os"
	"path/filepath"
)

func (p *Provider) ID() core.ComponentID { return core.ComponentID("code_intelligence") }
func (p *Provider) Probe(context.Context) core.ComponentStatus {
	installed := false
	if info, err := os.Stat(filepath.Join(p.Distribution, "dist", "graph", "build.js")); err == nil && info.Mode().IsRegular() {
		installed = true
	}
	health := core.HealthUnavailable
	if installed && p.Allowed {
		health = core.HealthUnknown
	}
	return core.ComponentStatus{ID: p.ID(), Implementation: "graft", Installed: installed, Managed: true, Available: installed && p.Allowed, Health: health, Lifecycle: core.LifecycleStopped, Provenance: core.Provenance{Source: "https://github.com/trailhq/Graft", Version: Version}, Capabilities: core.CapabilitySet{core.Capability("code.structure"): core.SupportSupported}, Compatibility: core.Compatibility{State: core.CompatibilityUnknown, Reason: "sandbox and managed runtime verified at execution"}, Fallback: core.Fallback{Allowed: true, Reason: "bounded direct inspection where semantically sufficient"}}
}

var _ core.CodeIntelligenceProvider = (*Provider)(nil)
