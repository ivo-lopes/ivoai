package config

import (
	"errors"
	"reflect"
)

// AutomationValues is the entire preset write/rollback allowlist. Provider,
// permission, approval, knowledge-source and authentication fields are excluded.
type AutomationValues struct {
	Concurrency    string            `toml:"concurrency" json:"concurrency"`
	WorkerCap      int               `toml:"worker_cap" json:"worker_cap"`
	ParallelWrites bool              `toml:"parallel_writes" json:"parallel_writes"`
	Parallelism    bool              `toml:"parallelism" json:"parallelism"`
	Weights        AutoWeightsConfig `toml:"weights" json:"weights"`
}

type AutomationProfileState struct {
	Name         string            `toml:"name,omitempty"`
	PreviousName string            `toml:"previous_name,omitempty"`
	Previous     *AutomationValues `toml:"previous,omitempty"`
	Applied      *AutomationValues `toml:"applied,omitempty"`
}

type AutomationProfile struct {
	ID      string           `json:"id"`
	Purpose string           `json:"purpose"`
	Values  AutomationValues `json:"values"`
}

func AutomationProfiles() []AutomationProfile {
	base := Default().Orchestration.Auto
	values := AutomationValuesOf(base)
	values.WorkerCap = 2
	sequential := values
	sequential.Concurrency = "sequential"
	sequential.WorkerCap = 1
	sequential.Parallelism = false
	sequential.ParallelWrites = false
	review := values
	review.ParallelWrites = false
	review.Weights = AutoWeightsConfig{20, 30, 15, 30, 5}
	research := sequential
	research.Weights = AutoWeightsConfig{20, 15, 15, 20, 30}
	intensive := values
	intensive.WorkerCap = 3
	intensive.Weights = AutoWeightsConfig{25, 25, 25, 20, 5}
	diagnostic := sequential
	diagnostic.Weights = AutoWeightsConfig{15, 30, 15, 30, 10}
	return []AutomationProfile{
		{"economic", "Sequential work; minimum sufficient runtime model and effort", sequential},
		{"balanced", "Up to two useful independent workers; host and quota limits still apply", values},
		{"intensive", "Up to three workers only when the approved DAG justifies coordination", intensive},
		{"review", "Code review with stronger risk and verification signals; no parallel writes", review},
		{"development", "Development and tests in independent isolated worktrees", values},
		{"research", "Research and documentation with bounded sequential context work", research},
		{"diagnostic", "Sequential operational diagnosis with verification emphasis", diagnostic},
	}
}

func AutomationValuesOf(a AutoConfig) AutomationValues {
	return AutomationValues{a.Concurrency, a.WorkerCap, a.ParallelWrites, a.Optimization.Parallelism, a.Optimization.Weights}
}
func (v AutomationValues) apply(a *AutoConfig) {
	a.Concurrency = v.Concurrency
	a.WorkerCap = v.WorkerCap
	a.ParallelWrites = v.ParallelWrites
	a.Optimization.Parallelism = v.Parallelism
	a.Optimization.Weights = v.Weights
}
func FindAutomationProfile(id string) (AutomationProfile, error) {
	for _, p := range AutomationProfiles() {
		if p.ID == id {
			return p, nil
		}
	}
	return AutomationProfile{}, errors.New("unknown automation profile")
}
func EffectiveAutomationProfile(c Config) string {
	state := c.Orchestration.Auto.AutomationProfile
	if state.Name == "" {
		return "inherited/custom"
	}
	p, err := FindAutomationProfile(state.Name)
	if err != nil || !reflect.DeepEqual(AutomationValuesOf(c.Orchestration.Auto), p.Values) {
		return "custom (based on " + state.Name + ")"
	}
	return state.Name
}

// ApplyAutomationProfile records only its bounded allowlist and changes it in
// the same atomic config write. Reapplying never replaces the rollback point.
func (s *Store) ApplyAutomationProfile(id string, expected *AutomationValues) error {
	p, err := FindAutomationProfile(id)
	if err != nil {
		return err
	}
	return s.updateAutomationConfig(func(c *Config) error {
		a := &c.Orchestration.Auto
		current := AutomationValuesOf(*a)
		if expected != nil && !reflect.DeepEqual(current, *expected) {
			return errors.New("automation settings changed after preview; preview again")
		}
		if a.AutomationProfile.Name == id && reflect.DeepEqual(current, p.Values) {
			return nil
		}
		a.AutomationProfile = AutomationProfileState{Name: id, PreviousName: a.AutomationProfile.Name, Previous: &current, Applied: &p.Values}
		p.Values.apply(a)
		return nil
	})
}

func (s *Store) RestoreAutomationProfile() error {
	return s.updateAutomationConfig(func(c *Config) error {
		a := &c.Orchestration.Auto
		state := a.AutomationProfile
		if state.Previous == nil {
			return nil
		}
		if state.Applied == nil || !reflect.DeepEqual(AutomationValuesOf(*a), *state.Applied) {
			return errors.New("automation settings changed since apply; preserve custom values and review configuration")
		}
		state.Previous.apply(a)
		a.AutomationProfile = AutomationProfileState{Name: state.PreviousName}
		return nil
	})
}

func validateAutomationProfile(value OrchestrationConfig) error {
	state := value.Auto.AutomationProfile
	for _, name := range []string{state.Name, state.PreviousName} {
		if name != "" {
			if _, err := FindAutomationProfile(name); err != nil {
				return err
			}
		}
	}
	if (state.Previous == nil) != (state.Applied == nil) {
		return errors.New("incomplete automation profile rollback metadata")
	}
	// Validate snapshots through the same runtime rules, without recursive receipts.
	for _, snapshot := range []*AutomationValues{state.Previous, state.Applied} {
		if snapshot != nil {
			copy := value
			copy.Auto.AutomationProfile = AutomationProfileState{}
			snapshot.apply(&copy.Auto)
			if err := ValidateOrchestration(copy); err != nil {
				return err
			}
		}
	}
	return nil
}
