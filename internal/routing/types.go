// Package routing validates automatic orchestration plans and resolves the
// lowest sufficient execution profile without accepting executable input from
// a model.
package routing

import "time"

const MaxTasks = 12

type Tier string

const (
	TierLight    Tier = "LIGHT"
	TierBalanced Tier = "BALANCED"
	TierStrong   Tier = "STRONG"
	TierMax      Tier = "MAX"
)

type Source string

const (
	SourceRuntimeVerified    Source = "runtime_verified"
	SourceCapabilityRegistry Source = "capability_registry"
	SourceConfigured         Source = "configured"
	SourceArgument           Source = "argument"
	SourceDefault            Source = "default"
	SourceUnsupported        Source = "unsupported"
	SourceUnknown            Source = "unknown"
)

type Scores struct {
	Complexity         int `json:"complexity"`
	Risk               int `json:"risk"`
	ReasoningDepth     int `json:"reasoning_depth"`
	ContextBreadth     int `json:"context_breadth"`
	VerificationNeed   int `json:"verification_need"`
	ParallelValue      int `json:"parallel_value"`
	LatencySensitivity int `json:"latency_sensitivity"`
}

type Weights struct {
	Complexity       int `json:"complexity" toml:"complexity"`
	Risk             int `json:"risk" toml:"risk"`
	ReasoningDepth   int `json:"reasoning_depth" toml:"reasoning_depth"`
	VerificationNeed int `json:"verification_need" toml:"verification_need"`
	ContextBreadth   int `json:"context_breadth" toml:"context_breadth"`
}

func DefaultWeights() Weights {
	return Weights{Complexity: 30, Risk: 25, ReasoningDepth: 20, VerificationNeed: 15, ContextBreadth: 10}
}

type TaskInput struct {
	ID                    string   `json:"id"`
	Role                  string   `json:"role"`
	Task                  string   `json:"task"`
	Dependencies          []string `json:"dependencies,omitempty"`
	ParallelGroup         string   `json:"parallel_group,omitempty"`
	RequiredCapabilities  []string `json:"required_capabilities,omitempty"`
	Scores                Scores   `json:"scores"`
	PreferredExecutor     string   `json:"preferred_executor,omitempty"`
	IntentionalRedundancy bool     `json:"intentional_redundancy,omitempty"`
}

type ExecutionProfile struct {
	Provider     string `json:"provider"`
	Model        string `json:"model,omitempty"`
	Effort       string `json:"effort,omitempty"`
	Tier         Tier   `json:"tier"`
	ModelSource  Source `json:"model_source"`
	EffortSource Source `json:"effort_source"`
}

type Task struct {
	TaskInput
	CapabilityScore      int              `json:"capability_score"`
	Tier                 Tier             `json:"tier"`
	Profile              ExecutionProfile `json:"execution_profile"`
	State                string           `json:"state"`
	EscalationCount      int              `json:"escalation_count,omitempty"`
	EscalationReason     string           `json:"escalation_reason,omitempty"`
	DurationMilliseconds int64            `json:"duration_ms,omitempty"`
	HeadroomUsed         bool             `json:"headroom_used"`
	ExecutionMode        string           `json:"execution_mode"`
	DelegationBenefit    int              `json:"delegation_benefit"`
	DelegationOverhead   int              `json:"delegation_overhead"`
	DelegationReason     string           `json:"delegation_reason"`
}

type Plan struct {
	ID        string    `json:"id"`
	Strategy  string    `json:"strategy"`
	CreatedAt time.Time `json:"created_at"`
	Tasks     []Task    `json:"tasks"`
}

type ModelCapability struct {
	Name             string   `json:"name"`
	Provider         string   `json:"provider"`
	CapabilityTier   Tier     `json:"capability_tier"`
	SupportedEfforts []string `json:"supported_efforts,omitempty"`
	DefaultEffort    string   `json:"default_effort,omitempty"`
	IsDefault        bool     `json:"is_default"`
	Source           Source   `json:"source"`
}

type ProviderCapability struct {
	Provider       string            `json:"provider"`
	Version        string            `json:"version,omitempty"`
	Authenticated  bool              `json:"authenticated"`
	WorkerCapable  bool              `json:"worker_capable"`
	Models         []ModelCapability `json:"models,omitempty"`
	SupportsEffort bool              `json:"supports_effort"`
	Source         Source            `json:"source"`
}

type Registry struct {
	Providers map[string]ProviderCapability `json:"providers"`
}

type ProfileOverride struct {
	Model  string `json:"model,omitempty" toml:"model"`
	Effort string `json:"effort,omitempty" toml:"effort"`
}
