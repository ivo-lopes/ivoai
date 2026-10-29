package codexresolver

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/ivo-lopes/ivoai/internal/platform"
)

// ConfigContext freezes the process-local configuration scope shared by MCP
// inventory and exec. It contains environment values and must never be logged.
// Accessors return copies; no credentials are read from or written to disk.
type ConfigContext struct {
	directory   string
	environment []string
	projectArgs []string
}

func NewConfigContext(options platform.RunOptions) (ConfigContext, error) {
	directory := options.Dir
	if !filepath.IsAbs(directory) {
		cwd, err := os.Getwd()
		if err != nil {
			return ConfigContext{}, errors.New("CODEX_MANAGED_CONFIG_CONTEXT_UNAVAILABLE")
		}
		directory = filepath.Join(cwd, directory)
	}
	directory = filepath.Clean(directory)
	if strings.ContainsRune(directory, '\x00') {
		return ConfigContext{}, errors.New("CODEX_MANAGED_CONFIG_CONTEXT_UNAVAILABLE")
	}
	return ConfigContext{directory: directory, environment: platform.SnapshotEnvironment(options), projectArgs: ProjectTrustArgs(directory)}, nil
}

func (c ConfigContext) Directory() string     { return c.directory }
func (c ConfigContext) Environment() []string { return append([]string{}, c.environment...) }
func (c ConfigContext) ProjectTrustArguments() []string {
	return append([]string(nil), c.projectArgs...)
}
func (c ConfigContext) RunOptions() platform.RunOptions {
	return platform.RunOptions{Dir: c.directory, Env: c.Environment(), CleanEnv: true}
}

func (c ConfigContext) String() string   { return "CodexConfigContext[redacted]" }
func (c ConfigContext) GoString() string { return c.String() }
func (c ConfigContext) Validate() error {
	if c.directory == "" || c.environment == nil || len(c.projectArgs) == 0 {
		return errors.New("CODEX_MANAGED_CONFIG_CONTEXT_UNAVAILABLE")
	}
	return nil
}

func (c ConfigContext) ManagedArguments(args []string) ([]string, error) {
	if err := c.Validate(); err != nil {
		return nil, err
	}
	// Validate caller arity before appending host arguments, otherwise an
	// incomplete caller option could consume the beginning of our policy.
	if err := ValidateManagedConfigScope(args); err != nil {
		return nil, err
	}
	boundary := managedArgumentBoundary(args)
	out := append([]string(nil), args[:boundary]...)
	out = append(out, c.projectArgs...)
	out = append(out, args[boundary:]...)
	return ManagedAgentArgs(out)
}

// MCPInventoryArguments carries config scope without forwarding exec options,
// prompts or values following the literal -- boundary. Consume unrelated option
// values before recognizing config, profile and disable options.
func MCPInventoryArguments(args []string) ([]string, error) {
	config, global := []string{}, []string{}
	boundary := managedArgumentBoundary(args)
	for i := 0; i < boundary; i++ {
		arg := args[i]
		option, _, inline := strings.Cut(arg, "=")
		isConfig := option == "-c" || option == "--config" || strings.HasPrefix(arg, "-c") && len(arg) > 2 && !strings.HasPrefix(arg, "--")
		compactProfile := strings.HasPrefix(arg, "-p") && len(arg) > 2 && !strings.HasPrefix(arg, "--")
		isGlobal := option == "--profile" || option == "-p" || option == "--disable" || compactProfile
		if isConfig || isGlobal {
			values := []string{arg}
			compactConfig := isConfig && strings.HasPrefix(arg, "-c") && len(arg) > 2 && !strings.HasPrefix(arg, "--")
			if !inline && !compactConfig && !compactProfile {
				if i+1 >= boundary {
					return nil, errors.New("CODEX_MANAGED_CONFIG_SCOPE_UNSUPPORTED: incomplete configuration option")
				}
				i++
				values = append(values, args[i])
			}
			if isConfig {
				// Preserve every explicit config override for the same local official
				// client: unknown keys may affect future scope. Never log these values.
				config = append(config, values...)
			} else {
				global = append(global, values...)
			}
		} else if !inline && (execOptions[option] || globalValueOptions[option]) && i+1 < boundary {
			i++
		}
	}
	// Profile/disable are root options, unlike inherited -c; keep them before mcp.
	out := append(global, "mcp", "list", "--json")
	return append(out, config...), nil
}

func validateManagedDirectoryArguments(args []string) error {
	boundary := managedArgumentBoundary(args)
	for i := 0; i < boundary; i++ {
		arg := args[i]
		option, _, inline := strings.Cut(arg, "=")
		if option == "--cd" || option == "-C" || strings.HasPrefix(arg, "-C") && len(arg) > 2 {
			return errors.New("CODEX_MANAGED_CONFIG_SCOPE_UNSUPPORTED: managed Codex directory is controlled by IVOAI; omit --cd/-C")
		}
		if !inline && (execOptions[option] || globalValueOptions[option]) {
			if i+1 >= boundary {
				return errors.New("CODEX_MANAGED_CONFIG_SCOPE_UNSUPPORTED: incomplete managed client option")
			}
			i++
		}
	}
	return nil
}
