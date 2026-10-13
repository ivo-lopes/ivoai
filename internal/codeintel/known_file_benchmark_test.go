package codeintel

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ivo-lopes/ivoai/internal/core"
)

// Same benchmark source can run on v0.12.0 and the candidate. Measures local
// time/allocations only; it is not a token or full-orchestration benchmark.
func BenchmarkKnownFileContext(b *testing.B) {
	root := b.TempDir()
	if err := exec.Command("git", "init", "-q", root).Run(); err != nil {
		b.Fatal(err)
	}
	source := "package fixture\nfunc API() int { return 42 }\n"
	if err := os.WriteFile(filepath.Join(root, "api.go"), []byte(source), 0600); err != nil {
		b.Fatal(err)
	}
	filler := []byte("package fixture\n" + strings.Repeat("// synthetic unrelated source\n", 1000))
	for i := range 200 {
		if err := os.WriteFile(filepath.Join(root, fmt.Sprintf("other%03d.go", i)), filler, 0600); err != nil {
			b.Fatal(err)
		}
	}
	p := &Provider{Root: root, Scope: "benchmark", Permissions: "filesystem.read", Allowed: true}
	q := core.CodeQuery{Operation: "skeleton", File: "api.go", BudgetBytes: 4096}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		r, err := p.Inspect(context.Background(), q)
		if err != nil || r.ProviderVersion != "direct" || !strings.Contains(r.Text, source) {
			b.Fatalf("incorrect direct evidence: %v", err)
		}
	}
}
