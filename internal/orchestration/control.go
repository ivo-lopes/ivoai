package orchestration

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/ivo-lopes/ivoai/internal/platform"
)

var (
	swarmIDPattern = regexp.MustCompile(`swarm-[A-Za-z0-9-]+`)
	taskIDPattern  = regexp.MustCompile(`task-[A-Za-z0-9-]+`)
	validTaskID    = regexp.MustCompile(`^task-[A-Za-z0-9_-]+$`)
)

type ControlPlane struct {
	Manager
	RuntimeDir string
}

type Swarm struct {
	ID      string
	Healthy bool
	Status  Status
}

func (c ControlPlane) Initialize(ctx context.Context, maxWorkers int) (Swarm, error) {
	status := c.Inspect(ctx)
	if !status.Installed {
		return Swarm{}, errors.New("Ruflo is not installed")
	}
	if !status.SafeMode || status.ProviderExecution || status.DurableMemory {
		return Swarm{}, errors.New("Ruflo safe profile is invalid or provider execution is enabled")
	}
	if maxWorkers < 1 || maxWorkers > 3 {
		return Swarm{}, errors.New("worker limit must be between 1 and 3")
	}
	if err := platform.EnsurePrivateDir(c.RuntimeDir); err != nil {
		return Swarm{}, err
	}
	if _, err := c.run(ctx, []string{"--version"}, 15*time.Second); err != nil {
		return Swarm{}, fmt.Errorf("Ruflo health failed: %w", err)
	}
	result, err := c.run(ctx, []string{"swarm", "init", "--topology", "hierarchical", "--max-agents", strconv.Itoa(maxWorkers + 1), "--auto-scale", "false"}, 30*time.Second)
	if err != nil {
		return Swarm{}, fmt.Errorf("initialize Ruflo swarm: %w", err)
	}
	id := swarmIDPattern.FindString(result.Stdout + "\n" + result.Stderr)
	if id == "" {
		return Swarm{}, errors.New("Ruflo initialized without returning a Swarm ID")
	}
	statusResult, err := c.run(ctx, []string{"swarm", "status"}, 20*time.Second)
	if err != nil || !strings.Contains(statusResult.Stdout+statusResult.Stderr, id) {
		return Swarm{}, errors.New("Ruflo swarm health could not be verified")
	}
	return Swarm{ID: id, Healthy: true, Status: status}, nil
}

// RegisterLifecycle creates only a Ruflo coordination task. It never invokes
// agent_spawn, swarm start, or any provider-backed execution path.
func (c ControlPlane) RegisterLifecycle(ctx context.Context, role, opaqueID string) (string, error) {
	if !safeLabel(role) || !safeLabel(opaqueID) {
		return "", errors.New("invalid lifecycle metadata")
	}
	description := "ivoai " + role + " " + opaqueID
	result, err := c.run(ctx, []string{"task", "create", "--type", "custom", "--description", description, "--tags", "ivoai," + role}, 20*time.Second)
	if err != nil {
		return "", fmt.Errorf("register Ruflo lifecycle: %w", err)
	}
	id := taskIDPattern.FindString(result.Stdout + "\n" + result.Stderr)
	if id == "" {
		return "", errors.New("Ruflo lifecycle registration returned no task ID")
	}
	return id, nil
}

func (c ControlPlane) CancelLifecycle(ctx context.Context, taskID string) error {
	if !validTaskID.MatchString(taskID) {
		return nil
	}
	_, err := c.run(ctx, []string{"task", "cancel", taskID, "--force"}, 15*time.Second)
	return err
}

func (c ControlPlane) Stop(ctx context.Context) error {
	_, err := c.run(ctx, []string{"swarm", "stop", "--save-state", "false"}, 20*time.Second)
	return err
}

func (c ControlPlane) run(ctx context.Context, args []string, timeout time.Duration) (platform.Result, error) {
	if c.Binary == "" || !filepath.IsAbs(c.Binary) {
		return platform.Result{}, errors.New("Ruflo executable is not a trusted absolute component path")
	}
	return c.Runner.Run(ctx, c.Binary, args, platform.RunOptions{Dir: c.RuntimeDir, Env: safeEnvironment(c.RuntimeDir, c.Binary), CleanEnv: true, Timeout: timeout})
}

func safeEnvironment(home, binary string) []string {
	path := filepath.Dir(binary) + string(os.PathListSeparator) + "/usr/local/bin:/usr/bin:/bin"
	environment := []string{
		"HOME=" + home,
		"PATH=" + path,
		"RUFLO_PROVIDER=ivoai-disabled",
		"CLAUDE_FLOW_HOOKS_ENABLED=false",
		"CLAUDE_FLOW_MEMORY_BACKEND=memory",
		"CLAUDE_FLOW_MCP_TOOLS=" + strings.Join(safeTools, ","),
		"NO_COLOR=1",
	}
	if locale := strings.TrimSpace(os.Getenv("LANG")); locale != "" && !strings.ContainsAny(locale, "\r\n\x00") {
		environment = append(environment, "LANG="+locale)
	}
	return environment
}

func safeLabel(value string) bool {
	if value == "" || len(value) > 128 {
		return false
	}
	for _, char := range value {
		if !(char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z' || char >= '0' && char <= '9' || char == '_' || char == '-') {
			return false
		}
	}
	return true
}
