// Package core defines the small, provider-neutral contracts used at IVOAI
// component boundaries. It deliberately contains no persistence types: these
// contracts may evolve without changing the config, state, or ownership schema.
package core

import (
	"context"
	"fmt"
	"strings"
	"time"
)

type ComponentID string

const (
	ComponentCodex         ComponentID = "codex"
	ComponentClaude        ComponentID = "claude"
	ComponentMemory        ComponentID = "memory"
	ComponentContext       ComponentID = "context"
	ComponentCompression   ComponentID = "compression"
	ComponentOrchestration ComponentID = "orchestration"
	ComponentSkills        ComponentID = "skills"
	ComponentTools         ComponentID = "tools"
)

type Capability string

const (
	CapabilitySessionStart           Capability = "session.start"
	CapabilitySessionAbort           Capability = "session.abort"
	CapabilityAdvisoryExecute        Capability = "advisory.execute"
	CapabilityMemoryConfigure        Capability = "memory.configure"
	CapabilityMemoryHooks            Capability = "memory.hooks"
	CapabilityMemoryStatus           Capability = "memory.status"
	CapabilityContextInitialize      Capability = "context.initialize"
	CapabilityContextSearch          Capability = "context.search"
	CapabilityContextRead            Capability = "context.read"
	CapabilityContextRecent          Capability = "context.recent"
	CapabilityContextStatus          Capability = "context.status"
	CapabilityContextIngest          Capability = "context.ingest"
	CapabilityCompressionWrap        Capability = "compression.wrap"
	CapabilityCompressionBypass      Capability = "compression.bypass"
	CapabilityOrchestrationSwarm     Capability = "orchestration.swarm"
	CapabilityOrchestrationLifecycle Capability = "orchestration.lifecycle"
)

type SupportState string

const (
	SupportUnknown     SupportState = "unknown"
	SupportSupported   SupportState = "supported"
	SupportUnsupported SupportState = "unsupported"
	SupportNotExposed  SupportState = "not_exposed"
)

type CapabilitySet map[Capability]SupportState

func (s CapabilitySet) Supports(capability Capability) bool {
	return s[capability] == SupportSupported
}

func (s CapabilitySet) Clone() CapabilitySet {
	result := make(CapabilitySet, len(s))
	for capability, state := range s {
		result[capability] = state
	}
	return result
}

type HealthState string

const (
	HealthUnknown     HealthState = "unknown"
	HealthHealthy     HealthState = "healthy"
	HealthDegraded    HealthState = "degraded"
	HealthUnavailable HealthState = "unavailable"
)

type CompatibilityState string

const (
	CompatibilityUnknown      CompatibilityState = "unknown"
	CompatibilityCompatible   CompatibilityState = "compatible"
	CompatibilityIncompatible CompatibilityState = "incompatible"
	CompatibilityNotExposed   CompatibilityState = "not_exposed"
)

type LifecycleState string

const (
	LifecycleUnknown  LifecycleState = "unknown"
	LifecycleStopped  LifecycleState = "stopped"
	LifecycleStarting LifecycleState = "starting"
	LifecycleRunning  LifecycleState = "running"
	LifecycleStopping LifecycleState = "stopping"
	LifecycleFailed   LifecycleState = "failed"
)

type Provenance struct {
	Source  string `json:"source"`
	Version string `json:"version,omitempty"`
	Path    string `json:"path,omitempty"`
}

type Compatibility struct {
	State  CompatibilityState `json:"state"`
	Reason string             `json:"reason,omitempty"`
}

type Fallback struct {
	Allowed bool   `json:"allowed"`
	Reason  string `json:"reason,omitempty"`
}

// ComponentStatus is runtime metadata only. It must not contain credentials,
// prompts, provider auth data, or live backend state.
type ComponentStatus struct {
	ID             ComponentID    `json:"id"`
	Implementation string         `json:"implementation"`
	Active         bool           `json:"active"`
	Installed      bool           `json:"installed"`
	Managed        bool           `json:"managed"`
	Available      bool           `json:"available"`
	Health         HealthState    `json:"health"`
	Lifecycle      LifecycleState `json:"lifecycle"`
	Provenance     Provenance     `json:"provenance"`
	Capabilities   CapabilitySet  `json:"capabilities"`
	Compatibility  Compatibility  `json:"compatibility"`
	Fallback       Fallback       `json:"fallback"`
}

func (s ComponentStatus) Validate() error {
	if !safeLabel(string(s.ID)) || !safeLabel(s.Implementation) {
		return fmt.Errorf("component identity must be a bounded safe label")
	}
	if !validHealth(s.Health) || !validCompatibility(s.Compatibility.State) || !validLifecycle(s.Lifecycle) {
		return fmt.Errorf("component %s returned invalid common state", s.ID)
	}
	for capability, state := range s.Capabilities {
		if strings.TrimSpace(string(capability)) == "" || !validSupport(state) {
			return fmt.Errorf("component %s returned invalid capability state", s.ID)
		}
	}
	return nil
}

type Component interface {
	ID() ComponentID
	Probe(context.Context) ComponentStatus
}

type SessionRequest struct {
	Args               []string
	CompressionEnabled bool
}

type SessionObservation struct {
	PID             int
	CompressionUsed bool
}

type Executor interface {
	Component
	StartSession(context.Context, SessionRequest, func(SessionObservation)) error
}

type MemoryConfiguration struct {
	MCPEndpoint  string
	HooksBaseURL string
	Token        string
	InstallMCP   bool
	InstallHooks bool
}

type MemoryBackend interface {
	Component
	Configure(context.Context, MemoryConfiguration) error
	Disable(context.Context) error
}

type ContextDocument struct {
	ID         string            `json:"id"`
	Source     string            `json:"source"`
	Path       string            `json:"path"`
	Title      string            `json:"title"`
	Content    string            `json:"content,omitempty"`
	Metadata   map[string]string `json:"metadata,omitempty"`
	ModifiedAt time.Time         `json:"modified_at"`
	IngestedAt time.Time         `json:"ingested_at"`
}

type ContextChunk struct {
	ID         string            `json:"id"`
	DocumentID string            `json:"document_id"`
	Text       string            `json:"text"`
	Index      int               `json:"index"`
	Metadata   map[string]string `json:"metadata,omitempty"`
	Vector     []float32         `json:"-"`
	CreatedAt  time.Time         `json:"created_at"`
}

type ContextSearchResult struct {
	Chunk ContextChunk `json:"chunk"`
	Score float32      `json:"score"`
}

type ContextStatus struct {
	Healthy    bool `json:"healthy"`
	Documents  int  `json:"documents"`
	Chunks     int  `json:"chunks"`
	Connectors int  `json:"connectors"`
}

type ContextBackend interface {
	Component
	Initialize(context.Context) error
	Search(context.Context, string, int) ([]ContextSearchResult, error)
	GetDocument(string) (ContextDocument, bool, error)
	Recent(int) ([]ContextDocument, error)
	Status(context.Context) ContextStatus
}

type CompressionRequest struct {
	Executor    ComponentID
	DirectPath  string
	Args        []string
	Environment []string
}

type CompressionDecision struct {
	Command     string
	Args        []string
	Environment []string
	Used        bool
}

type CompressionProvider interface {
	Component
	Prepare(context.Context, CompressionRequest) (CompressionDecision, error)
}

type Swarm struct {
	ID      string
	Healthy bool
}

type Orchestrator interface {
	Component
	Initialize(context.Context, int) (Swarm, error)
	RegisterLifecycle(context.Context, string, string) (string, error)
	CancelLifecycle(context.Context, string) error
	Stop(context.Context) error
}

type SkillDescriptor struct {
	ID      string
	Version string
	Source  Provenance
}

type SkillSource interface {
	Component
	Skills(context.Context) ([]SkillDescriptor, error)
}

type SkillRegistry interface {
	Component
	Resolve(context.Context, string) (SkillDescriptor, error)
}

type ToolDescriptor struct {
	Name         string
	Capabilities CapabilitySet
}

type ToolProvider interface {
	Component
	Tools(context.Context) ([]ToolDescriptor, error)
}

type UnsupportedError struct {
	Component  ComponentID
	Capability Capability
}

func (e *UnsupportedError) Error() string {
	return fmt.Sprintf("%s does not support %s", e.Component, e.Capability)
}

type UnavailableError struct {
	Component ComponentID
	Reason    string
}

func (e *UnavailableError) Error() string {
	if e.Reason == "" {
		return fmt.Sprintf("%s is unavailable", e.Component)
	}
	return fmt.Sprintf("%s is unavailable: %s", e.Component, e.Reason)
}

type IncompatibleError struct {
	Component ComponentID
	Reason    string
}

func (e *IncompatibleError) Error() string {
	if e.Reason == "" {
		return fmt.Sprintf("%s is incompatible", e.Component)
	}
	return fmt.Sprintf("%s is incompatible: %s", e.Component, e.Reason)
}

func safeLabel(value string) bool {
	if value == "" || len(value) > 128 {
		return false
	}
	for _, char := range value {
		if !(char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z' || char >= '0' && char <= '9' || char == '_' || char == '-' || char == '.') {
			return false
		}
	}
	return true
}

func validSupport(value SupportState) bool {
	return value == SupportUnknown || value == SupportSupported || value == SupportUnsupported || value == SupportNotExposed
}

func validHealth(value HealthState) bool {
	return value == HealthUnknown || value == HealthHealthy || value == HealthDegraded || value == HealthUnavailable
}

func validCompatibility(value CompatibilityState) bool {
	return value == CompatibilityUnknown || value == CompatibilityCompatible || value == CompatibilityIncompatible || value == CompatibilityNotExposed
}

func validLifecycle(value LifecycleState) bool {
	return value == LifecycleUnknown || value == LifecycleStopped || value == LifecycleStarting || value == LifecycleRunning || value == LifecycleStopping || value == LifecycleFailed
}
