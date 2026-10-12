package memory

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"time"

	"github.com/ivo-lopes/ivoai/internal/platform"
)

func (m Manager) configureOwnedHooks(ctx context.Context, binary string, configuration Configuration) error {
	if err := safeHookPath(m.HooksDir); err != nil {
		return err
	}
	if err := platform.EnsurePrivateDir(m.HooksDir); err != nil {
		return err
	}
	isolated, err := os.MkdirTemp(m.HooksDir, ".ivoai-render-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(isolated)
	// Every provider home is explicit. CleanEnv prevents token inheritance and
	// credential discovery through real HOME or any provider-specific override.
	env := []string{"HOME=" + isolated, "XDG_CONFIG_HOME=" + filepath.Join(isolated, "config"), "XDG_DATA_HOME=" + filepath.Join(isolated, "data"), "XDG_STATE_HOME=" + filepath.Join(isolated, "state"), "XDG_CACHE_HOME=" + filepath.Join(isolated, "cache"), "CODEX_HOME=" + filepath.Join(isolated, "codex"), "CLAUDE_CONFIG_DIR=" + filepath.Join(isolated, "claude"), "AI_MEMORY_DATA_DIR=" + filepath.Join(m.HooksDir, ".ivoai-runtime"), "PATH=" + os.Getenv("PATH")}
	env = append(env, memoryEnv(configuration.HooksBaseURL, "")...)
	plans := map[string][]byte{}
	destinations := HookRegistrationPaths()
	for _, agent := range []string{"codex", "claude-code"} {
		target := filepath.Join(isolated, agent+".json")
		args := []string{"install-hooks", "--agent", agent, "--apply", "--project-strategy", "repo-root", "--hooks-dir", m.HooksDir, "--config-file", target}
		// Never stream renderer output: it may include settings paths or commands.
		if _, err := m.Runner.Run(ctx, binary, args, platform.RunOptions{Env: env, CleanEnv: true, Dir: isolated, Timeout: 2 * time.Minute}); err != nil {
			return errors.New("isolated ai-memory hook rendering failed")
		}
		body, _, err := hookBody(target)
		if err != nil || len(body) == 0 {
			return errors.New("isolated ai-memory renderer produced no hook configuration")
		}
		plans[destinations[agent]] = body
	}
	return applyHookConfiguration(m.HooksDir, binary, plans)
}
