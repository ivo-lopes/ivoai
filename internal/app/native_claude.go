package app

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/ivo-lopes/ivoai/internal/core"
	"github.com/ivo-lopes/ivoai/internal/nativeacp"
	"github.com/ivo-lopes/ivoai/internal/opencodebridge"
	"github.com/ivo-lopes/ivoai/internal/platform"
)

func claudeManagedEnvironment(input []string) []string {
	allowed := map[string]bool{"HOME": true, "PATH": true, "USER": true, "LOGNAME": true, "LANG": true, "LC_ALL": true, "TZ": true, "DBUS_SESSION_BUS_ADDRESS": true, "XDG_RUNTIME_DIR": true, "XDG_CONFIG_HOME": true, "XDG_STATE_HOME": true, "XDG_DATA_HOME": true, "XDG_CACHE_HOME": true, "CLAUDE_CONFIG_DIR": true}
	result := []string{}
	for _, entry := range input {
		key, _, ok := strings.Cut(entry, "=")
		if ok && allowed[key] {
			result = append(result, entry)
		}
	}
	return append(result, "DISABLE_AUTOUPDATER=1", "CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC=1", "CLAUDE_CODE_DISABLE_AUTO_MEMORY=1")
}

func claudeManagedArgs(runtimeDir string, servers []nativeacp.MCPServer) ([]string, error) {
	projected := map[string]any{}
	allowed := []string{}
	for _, server := range servers {
		if server.Type != "" || server.Command == "" || !nativeMCPName.MatchString(server.Name) || projected[server.Name] != nil {
			return nil, errors.New("CLAUDE_MCP_PROJECTION_INVALID")
		}
		env := map[string]string{}
		for _, value := range server.Env {
			env[value.Name] = value.Value
		}
		projected[server.Name] = map[string]any{"type": "stdio", "command": server.Command, "args": server.Args, "env": env}
		allowed = append(allowed, "mcp__"+server.Name+"__*")
	}
	body, err := json.Marshal(map[string]any{"mcpServers": projected})
	if err != nil {
		return nil, err
	}
	path := filepath.Join(runtimeDir, "claude-native-mcp.json")
	if err = platform.AtomicWritePrivate(body, path); err != nil {
		return nil, err
	}
	return []string{"--strict-mcp-config", "--mcp-config", path, "--tools", "", "--disallowedTools", "Bash,Edit,Write,NotebookEdit,Agent,Task,Skill,Workflow", "--allowedTools", strings.Join(allowed, ","), "--permission-mode", "dontAsk", "--setting-sources", "", "--settings", `{"disableAllHooks":true,"autoMemoryEnabled":false}`}, nil
}

// The official CLI owns subscription authentication and stream parsing. This
// wrapper supplies only Core projections, receipt checks and no-replay policy.
func managedClaudeRunner(identity managedNativeRuntime, options nativeacp.Options) (core.ExecutorRunner, error) {
	args, err := claudeManagedArgs(options.RuntimeDir, options.MCPServers)
	if err != nil {
		return nil, err
	}
	return turnRunnerFunc(func(parent context.Context, request core.ExecutorRequest, emit func(string) error) (result core.ExecutorResult, runErr error) {
		result.Policy = core.ExecutionPolicy{NoReplay: true, NoAutomaticFailover: true}
		if request.Executor != "claude" || options.Authorize == nil || options.BindSession == nil || options.Validate == nil {
			return result, errors.New("CLAUDE_NATIVE_CONFIGURATION_INVALID")
		}
		ctx, cancel := context.WithTimeout(parent, 15*time.Minute)
		defer cancel()
		receipt := nativeacp.Receipt{SessionID: options.SessionID, FrontendSessionID: request.FrontendSessionID, NativeSessionID: request.ExecutorSessionID}
		if err := options.Authorize(ctx, request, receipt); err != nil {
			return result, err
		}
		executable, err := os.Open(identity.Binary)
		if err != nil {
			return result, err
		}
		defer executable.Close()
		info, err := executable.Stat()
		if err != nil || !sameAdaptiveFile(identity.Info, info) {
			return result, errors.New("CLAUDE_NATIVE_RUNTIME_UNREVIEWED")
		}
		var once sync.Once
		var lifecycleErr error
		var output strings.Builder
		runner := opencodebridge.CLIRunner{Claude: opencodebridge.ExecutorSpec{Path: identity.Binary, ExecutableFile: executable, SHA256: identity.SHA256, Version: identity.Version, Args: args, Env: claudeManagedEnvironment(options.Environment), Dir: options.Directory, RuntimeDir: options.RuntimeDir, ObserveProgress: func(trace core.ExecutionTrace) {
			if trace.PID > 0 && options.Started != nil {
				once.Do(func() {
					lifecycleErr = options.Started(ctx, receipt, trace.PID)
					if lifecycleErr != nil {
						cancel()
					}
				})
			}
			if options.Authorize(ctx, request, receipt) != nil {
				cancel()
			}
		}}}
		value, err := runner.Run(ctx, request, func(text string) error {
			if output.Len()+len(text) > 1<<20 {
				return errors.New("CLAUDE_NATIVE_OUTPUT_LIMIT")
			}
			output.WriteString(text)
			return nil
		})
		value.Policy = result.Policy
		if err != nil {
			return value, err
		}
		if lifecycleErr != nil {
			return value, lifecycleErr
		}
		if value.ExecutorSessionID == "" || options.BindSession(ctx, value.ExecutorSessionID, request.ExecutorSessionID != "") != nil {
			return value, errors.New("CLAUDE_NATIVE_OWNERSHIP_DENIED")
		}
		receipt.NativeSessionID = value.ExecutorSessionID
		if err = options.Validate(ctx, request, receipt); err != nil {
			return value, err
		}
		if err = emit(output.String()); err != nil {
			return value, err
		}
		return value, nil
	}), nil
}
