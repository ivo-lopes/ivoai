package app

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"time"

	"github.com/ivo-lopes/ivoai/internal/config"
	"github.com/ivo-lopes/ivoai/internal/platform"
	"github.com/ivo-lopes/ivoai/internal/server"
	"github.com/pelletier/go-toml/v2"
)

type InventoryPaths struct {
	Executable string `json:"executable"`
	ConfigRoot string `json:"config_root"`
	DataRoot   string `json:"data_root"`
	StateRoot  string `json:"state_root"`
	CacheRoot  string `json:"cache_root"`
}

type InventoryComponent struct {
	Name      string `json:"name"`
	Installed bool   `json:"installed"`
	Managed   bool   `json:"managed"`
	Version   string `json:"version,omitempty"`
	Path      string `json:"path,omitempty"`
}

type InventoryConnections struct {
	ChatGPT bool `json:"chatgpt_connected"`
	Claude  bool `json:"claude_connected"`
	Server  bool `json:"server_connected"`
}

type Inventory struct {
	FormatVersion     int                  `json:"format_version"`
	CollectedAt       time.Time            `json:"collected_at"`
	IVOAI             string               `json:"ivoai_version"`
	OS                string               `json:"os"`
	Architecture      string               `json:"architecture"`
	Modes             []string             `json:"modes"`
	Paths             InventoryPaths       `json:"paths"`
	Schemas           config.Schemas       `json:"schemas"`
	ServerProtocol    int                  `json:"server_protocol,omitempty"`
	InstallProvenance string               `json:"install_provenance"`
	RollbackAvailable bool                 `json:"rollback_available"`
	Components        []InventoryComponent `json:"components"`
	Connections       InventoryConnections `json:"connections"`
	InventoryOverall  string               `json:"inventory_overall"`
	Services          map[string]string    `json:"services,omitempty"`
	Backends          map[string]string    `json:"backends,omitempty"`
	LegacyRuntime     map[string]bool      `json:"legacy_runtime,omitempty"`
	Issues            []string             `json:"issues,omitempty"`
}

// SupportInventory is a read-only, secret-free production evidence format. It
// deliberately avoids provider auth, capability and quota probes because those
// can update caches. Absolute host paths still need operator sanitization before
// a report is shared outside the host.
func (a *App) SupportInventory(ctx context.Context) Inventory {
	result := Inventory{FormatVersion: 1, CollectedAt: time.Now().UTC(), IVOAI: a.Version, OS: runtime.GOOS, Architecture: runtime.GOARCH, InventoryOverall: "not-run", Services: map[string]string{}, Backends: map[string]string{}, LegacyRuntime: map[string]bool{}}
	result.Paths = InventoryPaths{ConfigRoot: a.Store.Paths.ConfigDir, DataRoot: a.Store.Paths.DataDir, StateRoot: a.Store.Paths.StateDir, CacheRoot: a.Store.Paths.CacheDir}
	if executable, err := a.managedExecutable(); err == nil {
		result.Paths.Executable = executable
	} else {
		result.Issues = append(result.Issues, err.Error())
	}
	if schemas, err := a.Store.InspectSchemas(); err == nil {
		result.Schemas = schemas
	} else {
		result.Schemas = schemas
		result.Issues = append(result.Issues, err.Error())
	}
	clientPresent := regularInventoryFile(a.Store.Paths.Config) || regularInventoryFile(a.Store.Paths.State) || regularInventoryFile(a.Store.Paths.Ownership)
	cfg, cfgErr := a.Store.Load()
	if cfgErr == nil {
		result.Connections = InventoryConnections{ChatGPT: cfg.Connections.ChatGPT.Status == "connected", Claude: cfg.Connections.Claude.Status == "connected", Server: cfg.Connections.Server.Status == "connected"}
	} else {
		result.Issues = append(result.Issues, cfgErr.Error())
	}
	state, stateErr := a.Store.LoadState()
	ownership, ownershipErr := a.Store.LoadOwnership()
	if stateErr != nil {
		result.Issues = append(result.Issues, stateErr.Error())
	}
	if ownershipErr != nil {
		result.Issues = append(result.Issues, ownershipErr.Error())
	}
	names := map[string]bool{}
	for name := range state.Components {
		names[name] = true
	}
	for name := range ownership.Components {
		names[name] = true
	}
	ordered := make([]string, 0, len(names))
	for name := range names {
		ordered = append(ordered, name)
	}
	sort.Strings(ordered)
	for _, name := range ordered {
		component, owned := state.Components[name], ownership.Components[name]
		managed := component.Managed && owned.Managed
		path := component.Path
		if path == "" {
			path = owned.Path
		}
		result.Components = append(result.Components, InventoryComponent{Name: name, Installed: component.Installed, Managed: managed, Version: component.Version, Path: path})
	}
	if ivoai, ok := ownership.Components["ivoai"]; ok && ivoai.Managed {
		result.InstallProvenance = "ivoai-managed"
	} else {
		result.InstallProvenance = "external-or-unknown"
	}
	result.LegacyRuntime["headroom_enabled"] = cfg.Headroom.Enabled && state.Components["headroom"].Installed
	result.LegacyRuntime["ruflo_enabled"] = cfg.Orchestration.Enabled && state.Components["ruflo"].Installed
	result.LegacyRuntime["ruflo_provider_execution"] = cfg.Orchestration.ProviderExecution
	serverRoot := ""
	if os.Getenv("IVOAI_TEST_MODE") == "1" {
		serverRoot = os.Getenv("IVOAI_SERVER_ROOT")
	}
	layout := server.DefaultLayout(serverRoot)
	serverConfig := filepath.Join(layout.ConfigDir, "server.toml")
	if info, err := os.Lstat(serverConfig); err == nil && info.Mode().IsRegular() && info.Mode()&os.ModeSymlink == 0 {
		result.Modes = append(result.Modes, "server")
		data, readErr := platform.ReadRegularFile(serverConfig, 4<<20)
		var envelope struct {
			Protocol int `toml:"protocol_version"`
		}
		if readErr != nil {
			result.Issues = append(result.Issues, readErr.Error())
		} else if parseErr := toml.Unmarshal(data, &envelope); parseErr != nil || envelope.Protocol <= 0 {
			result.Issues = append(result.Issues, "server protocol schema is invalid")
		} else {
			result.ServerProtocol = envelope.Protocol
		}
		result.Backends["context"] = "qdrant"
		result.Backends["embeddings"] = "local"
		result.Backends["memory"] = "ai-memory"
		systemctl := trustedSystemctlPath()
		for _, unit := range []string{"ivoai-dependencies.service", "ivoai-context.service", "ivoai-gateway.service"} {
			status := "unknown"
			if os.Getenv("IVOAI_TEST_MODE") == "1" {
				status = "test-mode"
			} else if systemctl == "" {
				status = "unavailable"
			} else if _, err := a.Runner.Run(ctx, systemctl, []string{"is-active", unit}, platform.RunOptions{Timeout: 5 * time.Second, CleanEnv: true, Env: []string{"PATH=/usr/bin:/bin", "LANG=C"}}); err == nil {
				status = "active"
			} else {
				status = "inactive"
			}
			result.Services[unit] = status
		}
	}
	if clientPresent {
		result.Modes = append([]string{"client"}, result.Modes...)
	}
	if len(result.Modes) == 0 {
		result.Modes = []string{"unconfigured"}
	}
	for _, root := range []string{filepath.Join(a.Store.Paths.StateDir, "updates"), filepath.Join(layout.DataDir, "updates")} {
		if info, err := os.Lstat(filepath.Join(root, "current.json")); err == nil && info.Mode().IsRegular() && info.Mode()&os.ModeSymlink == 0 {
			result.RollbackAvailable = true
		}
		if info, err := os.Lstat(filepath.Join(root, "ivoai.previous")); err == nil && info.Mode().IsRegular() && info.Mode()&os.ModeSymlink == 0 {
			result.RollbackAvailable = true
		}
	}
	result.InventoryOverall = inventoryOverall(result)
	return result
}

// trustedSystemctlPath avoids resolving an attacker-controlled executable from
// PATH when a support inventory is collected through sudo/root. Supported
// server distributions install systemctl at /usr/bin.
func trustedSystemctlPath() string {
	const path = "/usr/bin/systemctl"
	info, err := os.Lstat(path)
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || info.Mode().Perm()&0o022 != 0 {
		return ""
	}
	return path
}

func regularInventoryFile(path string) bool {
	info, err := os.Lstat(path)
	return err == nil && info.Mode().IsRegular() && info.Mode()&os.ModeSymlink == 0
}

func inventoryOverall(value Inventory) string {
	if len(value.Issues) > 0 {
		return "DEGRADED"
	}
	for _, status := range value.Services {
		if status != "active" && status != "test-mode" {
			return "DEGRADED"
		}
	}
	return "READY"
}
