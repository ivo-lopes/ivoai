package orchestration

import (
	"errors"
	"math"
	"sort"

	"github.com/ivo-lopes/ivoai/internal/quota"
	"github.com/ivo-lopes/ivoai/internal/routing"
)

// WorkerProfile is a capability envelope, not a grant of installed tools. Each
// task still needs explicit MCP/skill/source selection and a validated write set.
type WorkerProfile struct {
	Role             string
	MinimumTier      routing.Tier
	Write            bool
	RequiresWorktree bool
}

func CapabilityProfile(role string) (WorkerProfile, error) {
	profiles := map[string]WorkerProfile{
		"research":       {Role: "research", MinimumTier: routing.TierLight},
		"implementation": {Role: "implementation", MinimumTier: routing.TierBalanced, Write: true, RequiresWorktree: true},
		"review":         {Role: "review", MinimumTier: routing.TierStrong},
		"security":       {Role: "security", MinimumTier: routing.TierStrong},
		"documentation":  {Role: "documentation", MinimumTier: routing.TierLight, Write: true, RequiresWorktree: true},
		"ops":            {Role: "ops", MinimumTier: routing.TierStrong},
		"synthesis":      {Role: "synthesis", MinimumTier: routing.TierStrong},
	}
	profile, found := profiles[role]
	if !found {
		return WorkerProfile{}, errors.New("unknown worker capability profile")
	}
	return profile, nil
}

// HostResources is injectable: unknown resources conservatively admit one
// worker. No provider limit is inferred from missing quota telemetry.
type HostResources struct {
	CPUs                 int
	AvailableMemoryBytes uint64
	Load                 float64
	IOPressure           float64
}

type ConcurrencyInputs struct {
	Runnable          int
	UserCap           int
	ProviderSlots     int
	WorktreeSlots     int
	Writing           bool
	WorkerMemoryBytes uint64
	StartupCPUs       int
}

func Concurrency(host HostResources, input ConcurrencyInputs) int {
	if input.Runnable <= 0 || input.ProviderSlots <= 0 {
		return 0
	}
	if input.Writing && input.WorktreeSlots <= 0 {
		return 1
	}
	availableCPU := host.CPUs - int(math.Ceil(math.Max(0, host.Load)))
	if availableCPU < 1 {
		availableCPU = 1
	}
	startup := input.StartupCPUs
	if startup < 1 {
		startup = 1
	}
	count := availableCPU / startup
	if count < 1 {
		count = 1
	}
	if host.AvailableMemoryBytes == 0 || input.WorkerMemoryBytes == 0 {
		count = 1
	} else if memorySlots := host.AvailableMemoryBytes / input.WorkerMemoryBytes; memorySlots < uint64(count) {
		count = int(memorySlots)
		if count < 1 {
			count = 1
		}
	}
	if host.IOPressure >= 0.5 {
		count = max(1, count/2)
	}
	count = min(count, input.Runnable, input.ProviderSlots)
	if input.UserCap > 0 {
		count = min(count, input.UserCap)
	}
	if input.Writing {
		count = min(count, input.WorktreeSlots)
	}
	return count
}

type ConservationProposal struct {
	State     string           `json:"state"`
	Providers []quota.Provider `json:"providers"`
	Threshold int              `json:"threshold"`
}

// QuotaConservation never mutates a route. Only authoritative, current budget
// observations can propose conservation. Context capacity is not account quota.
func QuotaConservation(snapshot map[quota.Provider]quota.ProviderQuota, threshold int) ConservationProposal {
	if threshold < 1 || threshold > 100 {
		threshold = 10
	}
	proposal := ConservationProposal{State: "normal", Threshold: threshold, Providers: []quota.Provider{}}
	for provider, value := range snapshot {
		if !value.Authenticated || !value.Eligible || value.TelemetryUnknown {
			continue
		}
		for _, window := range value.Windows {
			if !window.Available || !window.Authoritative || window.State == quota.TelemetryStale || window.Kind == quota.KindContext {
				continue
			}
			if window.RemainingPercent >= 0 && window.RemainingPercent <= float64(threshold) {
				proposal.Providers = append(proposal.Providers, provider)
				break
			}
		}
	}
	sort.Slice(proposal.Providers, func(i, j int) bool { return proposal.Providers[i] < proposal.Providers[j] })
	if len(proposal.Providers) > 0 {
		proposal.State = "conservation_pending_confirmation"
	}
	return proposal
}

// SelectTools intersects a task's request with policy grants; registry presence
// is deliberately not an input. Copies prevent worker-to-worker slice aliasing.
func SelectTools(requested, granted []string) ([]string, error) {
	allowed := make(map[string]bool, len(granted))
	for _, name := range granted {
		allowed[name] = true
	}
	selected := make(map[string]bool, len(requested))
	for _, name := range requested {
		if name == "" || !allowed[name] {
			return nil, errors.New("MCP_DENIED")
		}
		selected[name] = true
	}
	result := make([]string, 0, len(selected))
	for name := range selected {
		result = append(result, name)
	}
	sort.Strings(result)
	return result, nil
}
