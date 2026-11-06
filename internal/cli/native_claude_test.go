package cli

import (
	"context"
	"github.com/ivo-lopes/ivoai/internal/app"
	"io"
	"strings"
	"testing"
)

func TestClaudeRunDispatchRequiresManagedHeadlessAdmission(t *testing.T) {
	t.Setenv("IVOAI_EXPERIMENTAL_CLAUDE_HEADLESS", "")
	a := &app.App{Out: io.Discard, Err: io.Discard}
	err := Run(context.Background(), a, []string{"claude", "run", "--prompt", "Explique o projeto."})
	if err == nil || !strings.Contains(err.Error(), "IVOAI_EXPERIMENTAL_CLAUDE_HEADLESS") {
		t.Fatalf("managed run escaped to native TUI: %v", err)
	}
}
