package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/ivo-lopes/ivoai/internal/platform"
	"github.com/pelletier/go-toml/v2"
)

const SchemaVersion = 1

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
type ConnectionsConfig struct {
	ChatGPT Connection `toml:"chatgpt"`
	Claude  Connection `toml:"claude"`
	Server  Connection `toml:"server"`
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
		IVOAI: IVOAIConfig{Version: SchemaVersion}, Client: ClientConfig{Profile: "default"},
		Headroom: HeadroomConfig{Enabled: true}, Memory: MemoryConfig{Enabled: true},
		Orchestration: OrchestrationConfig{Enabled: true, ProviderExecution: false, DefaultMode: "direct", PrimaryExecutor: "codex", ReviewExecutor: "claude", MaxWorkers: 2, Auto: defaultAutoConfig()},
		Connections: ConnectionsConfig{
			ChatGPT: Connection{Status: "not-connected"}, Claude: Connection{Status: "not-connected"}, Server: Connection{Status: "not-connected"},
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
	b, err := os.ReadFile(s.Paths.Config)
	if errors.Is(err, os.ErrNotExist) {
		return c, nil
	}
	if err != nil {
		return Config{}, fmt.Errorf("read config: %w", err)
	}
	if err := toml.Unmarshal(b, &c); err != nil {
		return Config{}, fmt.Errorf("parse config: %w", err)
	}
	if c.IVOAI.Version != SchemaVersion {
		return Config{}, fmt.Errorf("unsupported config schema %d", c.IVOAI.Version)
	}
	if c.MCP.Servers == nil {
		c.MCP.Servers = map[string]MCPServer{}
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

func defaultAutoConfig() AutoConfig {
	return AutoConfig{
		Enabled: true, DefaultPlanner: "codex", AutomaticFailover: true, CheckpointEnabled: true,
		QuotaRefreshSeconds: 45, MaxWorkers: 2,
		Quota:        AutoQuotaConfig{Enabled: true, ShowWeekly: true, ShowMonthly: true, ShowSession: true, ShowContext: true, ShowModelScoped: true},
		Optimization: AutoOptimizationConfig{Strategy: "efficient", Parallelism: true, SharedContextBootstrap: true, ProgressiveEscalation: true, Weights: AutoWeightsConfig{Complexity: 30, Risk: 25, ReasoningDepth: 20, VerificationNeed: 15, ContextBreadth: 10}},
	}
}

func (s *Store) Save(c Config) error {
	if err := ValidateOrchestration(c.Orchestration); err != nil {
		return err
	}
	if err := s.Ensure(); err != nil {
		return err
	}
	b, err := toml.Marshal(c)
	if err != nil {
		return fmt.Errorf("encode config: %w", err)
	}
	return platform.AtomicWritePrivate(b, s.Paths.Config)
}

func (s *Store) LoadState() (State, error) {
	state := State{Schema: SchemaVersion, Components: map[string]ComponentState{}}
	b, err := os.ReadFile(s.Paths.State)
	if errors.Is(err, os.ErrNotExist) {
		return state, nil
	}
	if err != nil {
		return State{}, fmt.Errorf("read state: %w", err)
	}
	if err := toml.Unmarshal(b, &state); err != nil {
		return State{}, fmt.Errorf("parse state: %w", err)
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
	b, err := toml.Marshal(state)
	if err != nil {
		return fmt.Errorf("encode state: %w", err)
	}
	return platform.AtomicWritePrivate(b, s.Paths.State)
}

func (s *Store) SaveOwnership(ownership Ownership) error {
	if err := s.Ensure(); err != nil {
		return err
	}
	b, err := toml.Marshal(ownership)
	if err != nil {
		return fmt.Errorf("encode ownership: %w", err)
	}
	return platform.AtomicWritePrivate(b, s.Paths.Ownership)
}

func (s *Store) LoadOwnership() (Ownership, error) {
	ownership := Ownership{Schema: SchemaVersion, Components: map[string]OwnedItem{}}
	b, err := os.ReadFile(s.Paths.Ownership)
	if errors.Is(err, os.ErrNotExist) {
		return ownership, nil
	}
	if err != nil {
		return Ownership{}, fmt.Errorf("read ownership: %w", err)
	}
	if err := toml.Unmarshal(b, &ownership); err != nil {
		return Ownership{}, fmt.Errorf("parse ownership: %w", err)
	}
	if ownership.Schema != SchemaVersion {
		return Ownership{}, fmt.Errorf("unsupported ownership schema %d", ownership.Schema)
	}
	if ownership.Components == nil {
		ownership.Components = map[string]OwnedItem{}
	}
	return ownership, nil
}

func PlatformSummary() (string, string) { return runtime.GOOS, runtime.GOARCH }
