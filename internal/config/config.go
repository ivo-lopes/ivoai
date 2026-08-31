package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"time"

	"github.com/ivo-lopes/ivoai/internal/platform"
	"github.com/pelletier/go-toml/v2"
)

const (
	ConfigSchemaVersion    = 1
	StateSchemaVersion     = 1
	OwnershipSchemaVersion = 1
	// SchemaVersion remains as a source-compatible alias for integrations that
	// used the original shared v0.5.0 constant.
	SchemaVersion = ConfigSchemaVersion
)

type Paths struct {
	ConfigDir   string
	DataDir     string
	StateDir    string
	CacheDir    string
	BinDir      string
	Config      string
	State       string
	Secrets     string
	Ownership   string
	HooksDir    string
	SessionsDir string
	QuotaDir    string
}

func ResolvePaths() (Paths, error) {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return Paths{}, errors.New("cannot determine user home directory")
	}
	configHome := envOr("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	dataHome := envOr("XDG_DATA_HOME", filepath.Join(home, ".local", "share"))
	stateHome := envOr("XDG_STATE_HOME", filepath.Join(home, ".local", "state"))
	cacheHome := envOr("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	configDir := filepath.Join(configHome, "ivoai")
	dataDir := filepath.Join(dataHome, "ivoai")
	stateDir := filepath.Join(stateHome, "ivoai")
	return Paths{
		ConfigDir: configDir, DataDir: dataDir, StateDir: stateDir,
		CacheDir: filepath.Join(cacheHome, "ivoai"), BinDir: filepath.Join(dataDir, "bin"),
		Config: filepath.Join(configDir, "config.toml"), State: filepath.Join(stateDir, "state.toml"),
		Secrets: filepath.Join(configDir, "secrets.json"), Ownership: filepath.Join(stateDir, "ownership.toml"),
		HooksDir: filepath.Join(dataDir, "hooks"), SessionsDir: filepath.Join(stateDir, "sessions"), QuotaDir: filepath.Join(stateDir, "quota"),
	}, nil
}

func envOr(key, fallback string) string {
	if value := os.Getenv(key); value != "" && filepath.IsAbs(value) {
		return filepath.Clean(value)
	}
	return fallback
}

type Config struct {
	IVOAI         IVOAIConfig         `toml:"ivoai"`
	Client        ClientConfig        `toml:"client"`
	Headroom      HeadroomConfig      `toml:"headroom"`
	Compression   CompressionConfig   `toml:"compression"`
	Memory        MemoryConfig        `toml:"memory"`
	Orchestration OrchestrationConfig `toml:"orchestration"`
	Connections   ConnectionsConfig   `toml:"connections"`
	MCP           MCPConfig           `toml:"mcp"`
}

type IVOAIConfig struct {
	Version int `toml:"version"`
}
type ClientConfig struct {
	Profile string `toml:"profile"`
}
type HeadroomConfig struct {
	Enabled bool `toml:"enabled"`
}
type CompressionConfig struct {
	// Provider is additive and backward compatible. Empty configurations from
	// v0.5.0 normalize to headroom, preserving the published behavior.
	Provider string `toml:"provider"`
}
type MemoryConfig struct {
	Enabled bool `toml:"enabled"`
}
type OrchestrationConfig struct {
	Enabled           bool       `toml:"enabled"`
	ProviderExecution bool       `toml:"provider_execution"`
	DefaultMode       string     `toml:"default_mode"`
	PrimaryExecutor   string     `toml:"primary_executor"`
	ReviewExecutor    string     `toml:"review_executor"`
	MaxWorkers        int        `toml:"max_workers"`
	Auto              AutoConfig `toml:"auto"`
}
type AutoConfig struct {
	Enabled             bool                   `toml:"enabled"`
	DefaultPlanner      string                 `toml:"default_planner"`
	AutomaticFailover   bool                   `toml:"automatic_failover"`
	CheckpointEnabled   bool                   `toml:"checkpoint_enabled"`
	QuotaRefreshSeconds int                    `toml:"quota_refresh_seconds"`
	MaxWorkers          int                    `toml:"max_workers"`
	Quota               AutoQuotaConfig        `toml:"quota"`
	Optimization        AutoOptimizationConfig `toml:"optimization"`
	Profiles            AutoProfilesConfig     `toml:"profiles"`
}
type AutoOptimizationConfig struct {
	Strategy               string            `toml:"strategy"`
	Parallelism            bool              `toml:"parallelism"`
	SharedContextBootstrap bool              `toml:"shared_context_bootstrap"`
	ProgressiveEscalation  bool              `toml:"progressive_escalation"`
	Weights                AutoWeightsConfig `toml:"weights"`
}
type AutoWeightsConfig struct {
	Complexity       int `toml:"complexity"`
	Risk             int `toml:"risk"`
	ReasoningDepth   int `toml:"reasoning_depth"`
	VerificationNeed int `toml:"verification_need"`
	ContextBreadth   int `toml:"context_breadth"`
}
type AutoProfileConfig struct {
	Model  string `toml:"model"`
	Effort string `toml:"effort"`
}
type AutoTierProfiles struct {
	Light    AutoProfileConfig `toml:"light"`
	Balanced AutoProfileConfig `toml:"balanced"`
	Strong   AutoProfileConfig `toml:"strong"`
	Max      AutoProfileConfig `toml:"max"`
}
type AutoProfilesConfig struct {
	Codex  AutoTierProfiles `toml:"codex"`
	Claude AutoTierProfiles `toml:"claude"`
}
type AutoQuotaConfig struct {
	Enabled         bool `toml:"enabled"`
	ShowWeekly      bool `toml:"show_weekly"`
	ShowMonthly     bool `toml:"show_monthly"`
	ShowSession     bool `toml:"show_session"`
	ShowContext     bool `toml:"show_context"`
	ShowModelScoped bool `toml:"show_model_scoped"`
}
type Connection struct {
	Status   string `toml:"status"`
	URL      string `toml:"url,omitempty"`
	Protocol int    `toml:"protocol,omitempty"`
}

// LegacyServerID is the stable identity assigned to a pre-multi-server
// connection. It intentionally does not depend on the editable alias.
const LegacyServerID = "srv_legacy_default"

// ServerProfile is one independently enrolled ivoai-server. Server is kept in
// ConnectionsConfig as a v0.5-compatible mirror; new code must use Servers.
type ServerProfile struct {
	ID              string          `toml:"id"`
	Alias           string          `toml:"alias,omitempty"`
	URL             string          `toml:"url"`
	Status          string          `toml:"status"`
	Enabled         bool            `toml:"enabled"`
	Purpose         string          `toml:"purpose,omitempty"`
	RedundancyGroup string          `toml:"redundancy_group,omitempty"`
	Priority        int             `toml:"priority,omitempty"`
	Protocol        int             `toml:"protocol,omitempty"`
	ContextMCPURL   string          `toml:"context_mcp_url,omitempty"`
	MemoryMCPURL    string          `toml:"memory_mcp_url,omitempty"`
	MemoryHooksURL  string          `toml:"memory_hooks_url,omitempty"`
	ServerVersion   string          `toml:"server_version,omitempty"`
	Features        map[string]bool `toml:"features,omitempty"`
}
type ConnectionsConfig struct {
	ChatGPT Connection               `toml:"chatgpt"`
	Claude  Connection               `toml:"claude"`
	Server  Connection               `toml:"server"`
	Servers map[string]ServerProfile `toml:"servers,omitempty"`
}
type MCPConfig struct {
	Servers map[string]MCPServer `toml:"servers"`
}
type MCPServer struct {
	URL      string `toml:"url"`
	HooksURL string `toml:"hooks_url,omitempty"`
	Enabled  bool   `toml:"enabled"`
	Kind     string `toml:"kind"`
}

func Default() Config {
	return Config{
		IVOAI: IVOAIConfig{Version: ConfigSchemaVersion}, Client: ClientConfig{Profile: "default"},
		Headroom: HeadroomConfig{Enabled: true}, Compression: CompressionConfig{Provider: "headroom"}, Memory: MemoryConfig{Enabled: true},
		Orchestration: OrchestrationConfig{Enabled: true, ProviderExecution: false, DefaultMode: "direct", PrimaryExecutor: "codex", ReviewExecutor: "claude", MaxWorkers: 2, Auto: defaultAutoConfig()},
		Connections: ConnectionsConfig{
			ChatGPT: Connection{Status: "not-connected"}, Claude: Connection{Status: "not-connected"}, Server: Connection{Status: "not-connected"}, Servers: map[string]ServerProfile{},
		}, MCP: MCPConfig{Servers: map[string]MCPServer{}},
	}
}

type State struct {
	Schema           int                       `toml:"schema"`
	SetupCompletedAt time.Time                 `toml:"setup_completed_at"`
	LastDoctorAt     time.Time                 `toml:"last_doctor_at"`
	Components       map[string]ComponentState `toml:"components"`
}
type ComponentState struct {
	Installed bool   `toml:"installed"`
	Managed   bool   `toml:"managed"`
	Version   string `toml:"version,omitempty"`
	Path      string `toml:"path,omitempty"`
}

type Ownership struct {
	Schema     int                  `toml:"schema"`
	Components map[string]OwnedItem `toml:"components"`
}
type OwnedItem struct {
	Managed   bool     `toml:"managed"`
	Path      string   `toml:"path,omitempty"`
	Launchers []string `toml:"launchers,omitempty"`
}

type Store struct{ Paths Paths }

type Schemas struct {
	Config    int `json:"config"`
	State     int `json:"state"`
	Ownership int `json:"ownership"`
}

func NewStore(paths Paths) *Store { return &Store{Paths: paths} }

func (s *Store) Ensure() error {
	for _, dir := range []string{s.Paths.ConfigDir, s.Paths.DataDir, s.Paths.StateDir, s.Paths.CacheDir, s.Paths.HooksDir, s.Paths.SessionsDir, s.Paths.QuotaDir} {
		if dir == "" {
			continue
		}
		if err := platform.EnsurePrivateDir(dir); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) Load() (Config, error) {
	c := Default()
	b, err := platform.ReadRegularFile(s.Paths.Config, 4<<20)
	if errors.Is(err, os.ErrNotExist) {
		return c, nil
	}
	if err != nil {
		return Config{}, fmt.Errorf("read config: %w", err)
	}
	if err := toml.Unmarshal(b, &c); err != nil {
		return Config{}, fmt.Errorf("parse config: %w", err)
	}
	if c.IVOAI.Version != ConfigSchemaVersion {
		return Config{}, fmt.Errorf("unsupported config schema %d", c.IVOAI.Version)
	}
	if c.MCP.Servers == nil {
		c.MCP.Servers = map[string]MCPServer{}
	}
	normalizeLegacyServers(&c)
	if c.Compression.Provider == "" {
		c.Compression.Provider = "headroom"
	}
	if err := ValidateCompression(c.Compression); err != nil {
		return Config{}, err
	}
	// Zero values come from pre-session-control-plane configurations. Migrate
	// them in memory and persist on the next normal setup/config write.
	if c.Orchestration.DefaultMode == "" {
		c.Orchestration.DefaultMode = "direct"
	}
	if c.Orchestration.PrimaryExecutor == "" {
		c.Orchestration.PrimaryExecutor = "codex"
	}
	if c.Orchestration.ReviewExecutor == "" {
		c.Orchestration.ReviewExecutor = "claude"
	}
	if c.Orchestration.MaxWorkers == 0 {
		c.Orchestration.MaxWorkers = 2
	}
	if c.Orchestration.Auto.DefaultPlanner == "" {
		c.Orchestration.Auto = defaultAutoConfig()
	}
	if c.Orchestration.Auto.Optimization.Strategy == "" {
		defaults := defaultAutoConfig()
		c.Orchestration.Auto.Optimization = defaults.Optimization
		c.Orchestration.Auto.Profiles = defaults.Profiles
	}
	if err := ValidateOrchestration(c.Orchestration); err != nil {
		return Config{}, err
	}
	return c, nil
}

func ValidateOrchestration(value OrchestrationConfig) error {
	if value.ProviderExecution {
		return errors.New("orchestration provider execution must remain disabled")
	}
	if value.DefaultMode != "direct" && value.DefaultMode != "orchestrated" {
		return errors.New("orchestration default_mode must be direct or orchestrated")
	}
	if value.PrimaryExecutor != "codex" && value.PrimaryExecutor != "claude" {
		return errors.New("orchestration primary_executor must be codex or claude")
	}
	if value.ReviewExecutor != "codex" && value.ReviewExecutor != "claude" {
		return errors.New("orchestration review_executor must be codex or claude")
	}
	if value.MaxWorkers < 1 || value.MaxWorkers > 3 {
		return errors.New("orchestration max_workers must be between 1 and 3")
	}
	if value.Auto.DefaultPlanner != "codex" && value.Auto.DefaultPlanner != "claude" {
		return errors.New("orchestration auto default_planner must be codex or claude")
	}
	if value.Auto.QuotaRefreshSeconds < 30 || value.Auto.QuotaRefreshSeconds > 300 {
		return errors.New("orchestration auto quota_refresh_seconds must be between 30 and 300")
	}
	if value.Auto.MaxWorkers < 1 || value.Auto.MaxWorkers > 3 {
		return errors.New("orchestration auto max_workers must be between 1 and 3")
	}
	if value.Auto.Optimization.Strategy != "efficient" {
		return errors.New("orchestration auto optimization strategy must be efficient")
	}
	weights := value.Auto.Optimization.Weights
	if weights.Complexity < 0 || weights.Risk < 0 || weights.ReasoningDepth < 0 || weights.VerificationNeed < 0 || weights.ContextBreadth < 0 || weights.Complexity+weights.Risk+weights.ReasoningDepth+weights.VerificationNeed+weights.ContextBreadth <= 0 {
		return errors.New("orchestration auto optimization weights must be non-negative with a positive sum")
	}
	for _, profiles := range []AutoTierProfiles{value.Auto.Profiles.Codex, value.Auto.Profiles.Claude} {
		for _, profile := range []AutoProfileConfig{profiles.Light, profiles.Balanced, profiles.Strong, profiles.Max} {
			if len(profile.Model) > 128 || len(profile.Effort) > 32 {
				return errors.New("automatic execution profile override is too long")
			}
		}
	}
	return nil
}

func ValidateCompression(value CompressionConfig) error {
	switch value.Provider {
	case "headroom", "caveman", "direct":
		return nil
	default:
		return errors.New("compression provider must be headroom, caveman, or direct")
	}
}

func normalizeLegacyServers(c *Config) {
	if c.Connections.Servers == nil {
		c.Connections.Servers = map[string]ServerProfile{}
	}
	if len(c.Connections.Servers) == 0 && c.Connections.Server.Status == "connected" && strings.TrimSpace(c.Connections.Server.URL) != "" {
		profile := ServerProfile{
			ID: LegacyServerID, Alias: "default", URL: c.Connections.Server.URL,
			Status: "connected", Enabled: true, Purpose: "default",
			Protocol: c.Connections.Server.Protocol, Features: map[string]bool{},
		}
		if contextServer, ok := c.MCP.Servers["ivoai-context"]; ok {
			profile.ContextMCPURL = contextServer.URL
		}
		if memoryServer, ok := c.MCP.Servers["ivoai-memory"]; ok {
			profile.MemoryMCPURL = memoryServer.URL
			profile.MemoryHooksURL = memoryServer.HooksURL
			profile.Features["memory"] = memoryServer.Enabled
		}
		c.Connections.Servers["default"] = profile
	}
	for alias, profile := range c.Connections.Servers {
		if profile.Alias == "" {
			profile.Alias = alias
		}
		if profile.Purpose == "" {
			profile.Purpose = alias
		}
		if profile.Features == nil {
			profile.Features = map[string]bool{}
		}
		c.Connections.Servers[alias] = profile
	}
}

func defaultAutoConfig() AutoConfig {
	return AutoConfig{
		Enabled: true, DefaultPlanner: "codex", AutomaticFailover: true, CheckpointEnabled: true,
		QuotaRefreshSeconds: 45, MaxWorkers: 2,
		Quota:        AutoQuotaConfig{Enabled: true, ShowWeekly: true, ShowMonthly: true, ShowSession: true, ShowContext: true, ShowModelScoped: true},
		Optimization: AutoOptimizationConfig{Strategy: "efficient", Parallelism: true, SharedContextBootstrap: true, ProgressiveEscalation: true, Weights: AutoWeightsConfig{Complexity: 30, Risk: 25, ReasoningDepth: 20, VerificationNeed: 15, ContextBreadth: 10}},
	}
}

func (s *Store) Save(c Config) error {
	normalizeLegacyServers(&c)
	if err := ValidateCompression(c.Compression); err != nil {
		return err
	}
	if err := ValidateOrchestration(c.Orchestration); err != nil {
		return err
	}
	if err := s.Ensure(); err != nil {
		return err
	}
	b, err := s.marshalPreservingUnknown(s.Paths.Config, c, []string{"mcp.servers", "connections.servers"}, []string{
		"connections.servers",
		"connections.chatgpt.url", "connections.chatgpt.protocol",
		"connections.claude.url", "connections.claude.protocol",
		"connections.server.url", "connections.server.protocol",
		"connections.servers.*.alias", "connections.servers.*.url", "connections.servers.*.purpose", "connections.servers.*.redundancy_group",
		"connections.servers.*.context_mcp_url", "connections.servers.*.memory_mcp_url", "connections.servers.*.memory_hooks_url", "connections.servers.*.server_version",
		"mcp.servers.*.hooks_url",
	})
	if err != nil {
		return fmt.Errorf("encode config: %w", err)
	}
	return platform.AtomicWritePrivate(b, s.Paths.Config)
}

func (s *Store) LoadState() (State, error) {
	state, err := s.LoadStateForUpdate()
	if err != nil {
		return State{}, err
	}
	if state.Schema != StateSchemaVersion {
		return State{}, fmt.Errorf("unsupported state schema %d", state.Schema)
	}
	return state, nil
}

// LoadStateForUpdate decodes only the stable migration projection without
// requiring the source schema to equal the candidate's current schema. Update
// preflight needs ownership paths before the ordered migration registry runs;
// normal application code must continue to use LoadState and fail closed.
func (s *Store) LoadStateForUpdate() (State, error) {
	state := State{Schema: StateSchemaVersion, Components: map[string]ComponentState{}}
	b, err := platform.ReadRegularFile(s.Paths.State, 16<<20)
	if errors.Is(err, os.ErrNotExist) {
		return state, nil
	}
	if err != nil {
		return State{}, fmt.Errorf("read state: %w", err)
	}
	if err := toml.Unmarshal(b, &state); err != nil {
		return State{}, fmt.Errorf("parse state: %w", err)
	}
	if state.Schema <= 0 {
		return State{}, fmt.Errorf("invalid state schema %d", state.Schema)
	}
	if state.Components == nil {
		state.Components = map[string]ComponentState{}
	}
	return state, nil
}

func (s *Store) SaveState(state State) error {
	if err := s.Ensure(); err != nil {
		return err
	}
	if state.Schema != StateSchemaVersion {
		return fmt.Errorf("encode state: unsupported state schema %d", state.Schema)
	}
	b, err := s.marshalPreservingUnknown(s.Paths.State, state, []string{"components"}, []string{"components.*.version", "components.*.path"})
	if err != nil {
		return fmt.Errorf("encode state: %w", err)
	}
	return platform.AtomicWritePrivate(b, s.Paths.State)
}

func (s *Store) SaveOwnership(ownership Ownership) error {
	if err := s.Ensure(); err != nil {
		return err
	}
	if ownership.Schema != OwnershipSchemaVersion {
		return fmt.Errorf("encode ownership: unsupported ownership schema %d", ownership.Schema)
	}
	b, err := s.marshalPreservingUnknown(s.Paths.Ownership, ownership, []string{"components"}, []string{"components.*.path", "components.*.launchers"})
	if err != nil {
		return fmt.Errorf("encode ownership: %w", err)
	}
	return platform.AtomicWritePrivate(b, s.Paths.Ownership)
}

func (s *Store) LoadOwnership() (Ownership, error) {
	ownership, err := s.LoadOwnershipForUpdate()
	if err != nil {
		return Ownership{}, err
	}
	if ownership.Schema != OwnershipSchemaVersion {
		return Ownership{}, fmt.Errorf("unsupported ownership schema %d", ownership.Schema)
	}
	return ownership, nil
}

// LoadOwnershipForUpdate is the ownership counterpart to
// LoadStateForUpdate. Its fields are the stable, minimal projection required
// to snapshot IVOAI-owned component files while preserving the raw TOML.
func (s *Store) LoadOwnershipForUpdate() (Ownership, error) {
	ownership := Ownership{Schema: OwnershipSchemaVersion, Components: map[string]OwnedItem{}}
	b, err := platform.ReadRegularFile(s.Paths.Ownership, 16<<20)
	if errors.Is(err, os.ErrNotExist) {
		return ownership, nil
	}
	if err != nil {
		return Ownership{}, fmt.Errorf("read ownership: %w", err)
	}
	if err := toml.Unmarshal(b, &ownership); err != nil {
		return Ownership{}, fmt.Errorf("parse ownership: %w", err)
	}
	if ownership.Schema <= 0 {
		return Ownership{}, fmt.Errorf("invalid ownership schema %d", ownership.Schema)
	}
	if ownership.Components == nil {
		ownership.Components = map[string]OwnedItem{}
	}
	return ownership, nil
}

// InspectSchemas reads only the version envelopes needed by support inventory
// and migration preflight. It does not decode secrets or provider stores.
func (s *Store) InspectSchemas() (Schemas, error) {
	result := Schemas{}
	var failures []error
	for _, item := range []struct {
		path string
		set  func(map[string]any) error
	}{
		{s.Paths.Config, func(value map[string]any) error {
			section, ok := value["ivoai"].(map[string]any)
			if !ok {
				return errors.New("config schema envelope is missing")
			}
			result.Config = integerValue(section["version"])
			if result.Config <= 0 {
				return errors.New("config schema is invalid")
			}
			return nil
		}},
		{s.Paths.State, func(value map[string]any) error {
			result.State = integerValue(value["schema"])
			if result.State <= 0 {
				return errors.New("state schema is invalid")
			}
			return nil
		}},
		{s.Paths.Ownership, func(value map[string]any) error {
			result.Ownership = integerValue(value["schema"])
			if result.Ownership <= 0 {
				return errors.New("ownership schema is invalid")
			}
			return nil
		}},
	} {
		data, err := platform.ReadRegularFile(item.path, 16<<20)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			failures = append(failures, err)
			continue
		}
		var document map[string]any
		if err := toml.Unmarshal(data, &document); err != nil {
			failures = append(failures, fmt.Errorf("inspect %s schema: %w", filepath.Base(item.path), err))
			continue
		}
		if err := item.set(document); err != nil {
			failures = append(failures, fmt.Errorf("inspect %s schema: %w", filepath.Base(item.path), err))
		}
	}
	return result, errors.Join(failures...)
}

func integerValue(value any) int {
	switch typed := value.(type) {
	case int64:
		return int(typed)
	case int:
		return typed
	case uint64:
		converted := int(typed)
		if converted < 0 || uint64(converted) != typed {
			return 0
		}
		return converted
	default:
		return 0
	}
}

func PlatformSummary() (string, string) { return runtime.GOOS, runtime.GOARCH }

func (s *Store) marshalPreservingUnknown(filename string, value any, exactMaps, omittedKnownFields []string) ([]byte, error) {
	typedBytes, err := toml.Marshal(value)
	if err != nil {
		return nil, err
	}
	var typed map[string]any
	if err := toml.Unmarshal(typedBytes, &typed); err != nil {
		return nil, err
	}
	existingBytes, err := platform.ReadRegularFile(filename, 16<<20)
	if errors.Is(err, os.ErrNotExist) {
		return typedBytes, nil
	}
	if err != nil {
		return nil, err
	}
	var existing map[string]any
	if err := toml.Unmarshal(existingBytes, &existing); err != nil {
		return nil, fmt.Errorf("parse existing document before save: %w", err)
	}
	exact := make(map[string]bool, len(exactMaps))
	for _, path := range exactMaps {
		exact[path] = true
	}
	merged := mergeDocument(existing, typed, "", exact, omittedKnownFields)
	if reflect.DeepEqual(merged, existing) {
		return existingBytes, nil
	}
	return toml.Marshal(merged)
}

func mergeDocument(existing, typed map[string]any, prefix string, exact map[string]bool, omittedKnownFields []string) map[string]any {
	result := cloneMap(existing)
	for key := range result {
		path := key
		if prefix != "" {
			path = prefix + "." + key
		}
		if _, present := typed[key]; !present && matchesKnownPath(path, omittedKnownFields) {
			delete(result, key)
		}
	}
	for key, typedValue := range typed {
		path := key
		if prefix != "" {
			path = prefix + "." + key
		}
		typedMap, typedIsMap := typedValue.(map[string]any)
		existingMap, existingIsMap := result[key].(map[string]any)
		if typedIsMap {
			if exact[path] {
				selected := make(map[string]any, len(typedMap))
				for childKey, childValue := range typedMap {
					childMap, childIsMap := childValue.(map[string]any)
					oldChild, oldChildIsMap := existingMap[childKey].(map[string]any)
					if existingIsMap && childIsMap && oldChildIsMap {
						selected[childKey] = mergeDocument(oldChild, childMap, path+"."+childKey, exact, omittedKnownFields)
					} else {
						selected[childKey] = childValue
					}
				}
				result[key] = selected
				continue
			}
			if !existingIsMap {
				existingMap = map[string]any{}
			}
			result[key] = mergeDocument(existingMap, typedMap, path, exact, omittedKnownFields)
			continue
		}
		result[key] = typedValue
	}
	return result
}

func matchesKnownPath(path string, patterns []string) bool {
	parts := strings.Split(path, ".")
	for _, pattern := range patterns {
		patternParts := strings.Split(pattern, ".")
		if len(parts) != len(patternParts) {
			continue
		}
		matched := true
		for index := range parts {
			if patternParts[index] != "*" && patternParts[index] != parts[index] {
				matched = false
				break
			}
		}
		if matched {
			return true
		}
	}
	return false
}

func cloneMap(source map[string]any) map[string]any {
	result := make(map[string]any, len(source))
	for key, value := range source {
		result[key] = value
	}
	return result
}
