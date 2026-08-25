package routing

import (
	"errors"
	"sort"
	"strings"

	"github.com/ivo-lopes/ivoai/internal/quota"
)

type Router struct {
	Registry  Registry
	Quota     map[quota.Provider]quota.ProviderQuota
	Overrides map[string]map[Tier]ProfileOverride
}

func (r Router) Resolve(input TaskInput, tier Tier) (ExecutionProfile, error) {
	providers := []string{"codex", "claude"}
	if input.PreferredExecutor == "codex" || input.PreferredExecutor == "claude" {
		providers = []string{input.PreferredExecutor, string(quota.Other(quota.Provider(input.PreferredExecutor)))}
	}
	type candidate struct {
		profile  ExecutionProfile
		pressure float64
	}
	var candidates []candidate
	for _, provider := range providers {
		capability, ok := r.Registry.Providers[provider]
		if !ok || !capability.WorkerCapable || !capability.Authenticated {
			continue
		}
		override := r.Overrides[provider][tier]
		current := r.Quota[quota.Provider(provider)]
		selected, modelSource := selectEligibleModel(capability.Models, tier, override.Model, current)
		if selected == nil && len(capability.Models) > 0 {
			continue
		}
		model := ""
		if selected != nil {
			model = selected.Name
		}
		if selected == nil && current.Provider != "" && !eligible(current, "") {
			continue
		}
		effort, effortSource := resolveEffort(tier, override.Effort, selected, capability.SupportsEffort)
		candidates = append(candidates, candidate{profile: ExecutionProfile{Provider: provider, Model: model, Effort: effort, Tier: tier, ModelSource: modelSource, EffortSource: effortSource}, pressure: quotaPressure(current)})
	}
	if len(candidates) == 0 {
		return ExecutionProfile{}, errors.New("no subscription-backed execution profile satisfies the task")
	}
	sort.SliceStable(candidates, func(i, j int) bool { return candidates[i].pressure < candidates[j].pressure })
	return candidates[0].profile, nil
}

func selectEligibleModel(models []ModelCapability, tier Tier, configured string, current quota.ProviderQuota) (*ModelCapability, Source) {
	ordered := make([]*ModelCapability, 0, len(models))
	if configured != "" {
		for index := range models {
			if models[index].Name == configured {
				ordered = append(ordered, &models[index])
				break
			}
		}
	}
	for requiredRank := tierRank(tier); requiredRank <= tierRank(TierMax); requiredRank++ {
		for index := range models {
			model := &models[index]
			if model.Name == configured || tierRank(model.CapabilityTier) != requiredRank {
				continue
			}
			ordered = append(ordered, model)
		}
	}
	if len(ordered) == 0 {
		for index := range models {
			if models[index].IsDefault {
				ordered = append(ordered, &models[index])
				break
			}
		}
	}
	for _, model := range ordered {
		if eligible(current, model.Name) {
			if configured != "" && model.Name == configured {
				return model, SourceConfigured
			}
			return model, model.Source
		}
	}
	return nil, SourceUnknown
}

func sufficientModel(models []ModelCapability, required Tier) *ModelCapability {
	var fallback *ModelCapability
	var selected *ModelCapability
	for index := range models {
		model := &models[index]
		if model.IsDefault {
			fallback = model
		}
		if model.CapabilityTier != "" && tierRank(model.CapabilityTier) >= tierRank(required) && (selected == nil || tierRank(model.CapabilityTier) < tierRank(selected.CapabilityTier)) {
			selected = model
		}
	}
	if selected != nil {
		return selected
	}
	return fallback
}

func tierRank(value Tier) int {
	return map[Tier]int{TierLight: 1, TierBalanced: 2, TierStrong: 3, TierMax: 4}[value]
}

func resolveEffort(tier Tier, configured string, model *ModelCapability, supported bool) (string, Source) {
	if !supported || model == nil || len(model.SupportedEfforts) == 0 {
		return "", SourceUnsupported
	}
	desired := configured
	if desired == "" {
		desired = map[Tier]string{TierLight: "low", TierBalanced: "medium", TierStrong: "high", TierMax: "max"}[tier]
	}
	for _, value := range model.SupportedEfforts {
		if value == desired {
			if configured != "" {
				return value, SourceConfigured
			}
			return value, SourceCapabilityRegistry
		}
	}
	// Select the highest supported effort not exceeding the desired tier. This
	// is based only on the runtime catalog, never on an invented capability.
	order := []string{"none", "minimal", "low", "medium", "high", "xhigh", "max", "ultra"}
	limit := len(order) - 1
	for index, value := range order {
		if value == desired {
			limit = index
			break
		}
	}
	available := map[string]bool{}
	for _, value := range model.SupportedEfforts {
		available[strings.ToLower(value)] = true
	}
	for index := limit; index >= 0; index-- {
		if available[order[index]] {
			return order[index], SourceCapabilityRegistry
		}
	}
	return model.DefaultEffort, SourceDefault
}

func eligible(value quota.ProviderQuota, model string) bool {
	if value.Provider == "" { // unknown telemetry is not zero quota
		return true
	}
	if !value.Eligible || value.HardLimitReached {
		return false
	}
	for _, window := range value.Windows {
		if model != "" && window.Kind == quota.KindModelWeekly && window.Model == model && window.Available && window.RemainingPercent <= 0 {
			return false
		}
	}
	return true
}

func quotaPressure(value quota.ProviderQuota) float64 {
	if value.Provider == "" {
		return 50
	}
	remaining := 100.0
	found := false
	for _, window := range value.Windows {
		if window.Available && window.Authoritative && (window.Kind == quota.KindWeekly || window.Kind == quota.KindSession || window.Kind == quota.KindMonthly) {
			if !found || window.RemainingPercent < remaining {
				remaining = window.RemainingPercent
			}
			found = true
		}
	}
	if !found {
		return 50
	}
	return 100 - remaining
}
