package config

import "errors"

// ApplyAutomationProfile edits the existing policy, never creates a second
// routing stack. Explicit provider/model overrides and all security gates stay
// authoritative. Selecting custom retains every current setting.
func (c *AutoConfig) ApplyAutomationProfile(name string) error {
	if name == "custom" {
		c.AutomationProfile = name
		return nil
	}
	if name != "economic" && name != "balanced" && name != "quality" {
		return errors.New("unknown automation profile")
	}
	c.AutomationProfile = name
	c.PlanExecution = "approve"
	c.Concurrency = "auto"
	c.Optimization.Strategy = "efficient"
	c.Optimization.ProgressiveEscalation = true
	c.Optimization.SharedContextBootstrap = true
	c.Optimization.Parallelism = true
	c.LowQuotaThreshold = 10
	c.Quota.Enabled = true
	c.WorkerCap = 0
	c.ParallelWrites = true
	c.Optimization.Weights = AutoWeightsConfig{Complexity: 30, Risk: 25, ReasoningDepth: 20, VerificationNeed: 15, ContextBreadth: 10}
	switch name {
	case "economic":
		c.WorkerCap = 2
		c.ParallelWrites = false
	case "quality":
		c.WorkerCap = 4
		c.Optimization.Weights = AutoWeightsConfig{Complexity: 20, Risk: 30, ReasoningDepth: 20, VerificationNeed: 25, ContextBreadth: 5}
	}
	return nil
}

func (c AutoConfig) ResolvedAutomationProfile() string {
	if c.AutomationProfile == "" {
		return "custom"
	}
	return c.AutomationProfile
}
