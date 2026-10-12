package cli

import (
	"fmt"
	"io"

	"github.com/ivo-lopes/ivoai/internal/doctor"
)

// The menu Doctor action uses runDoctor too, keeping console and TUI semantics
// identical. Metadata verification must not be presented as execution health.
func printDoctorCodeIntelligence(out io.Writer, report doctor.Report) {
	g, c := report.Graft, report.CodeIntelligence
	fmt.Fprintf(out, "Graft: installed=%t managed=%t version=%s revision=%s license=%s trust=%s integrity=%s\n", g.Installed, g.Managed, g.Version, g.Revision, g.License, g.TrustLevel, c.Integrity)
	fmt.Fprintf(out, "Code intelligence: health=%s available=%t compatibility=%s coverage=%s private-node=%s node-integrity=%s libatomic=%s landlock-abi=%d seccomp-action=%t\n  %s\n", c.Component.Health, c.Component.Available, c.Component.Compatibility.State, c.Coverage, c.Node, c.NodeIntegrity, c.Libatomic, c.LandlockABI, c.Seccomp, c.Component.Compatibility.Reason)
}
