package skills

import (
	"errors"
	"reflect"
	"testing"
)

func graphRegistry(entries ...Entry) Registry {
	registry := EmptyRegistry()
	for index := range entries {
		entries[index].Lifecycle = LifecycleStaged
		if len(entries[index].Capabilities) == 0 {
			entries[index].Capabilities = []string{"filesystem.read"}
		}
		if len(entries[index].Compatibility.Executors) == 0 {
			entries[index].Compatibility.Executors = []string{"codex", "claude"}
		}
	}
	registry.Entries = entries
	return registry
}

func resolveFixture(registry Registry, ids ...string) (Resolution, error) {
	return (Resolver{Registry: registry}).Resolve(ResolutionRequest{IDs: ids, Executor: "codex", MaximumRisk: RiskHigh, AvailableCapabilities: map[string]bool{"filesystem.read": true}})
}

func TestResolverOrdersDependenciesAndDeduplicatesCandidates(t *testing.T) {
	base, middle, leaf := fixtureEntry("base"), fixtureEntry("middle"), fixtureEntry("leaf")
	middle.RequiredDependencies = []string{"base"}
	leaf.RequiredDependencies = []string{"middle"}
	resolution, err := resolveFixture(graphRegistry(leaf, base, middle), "leaf", "leaf", "base")
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, entry := range resolution.Ordered {
		ids = append(ids, entry.ID)
	}
	if !reflect.DeepEqual(ids, []string{"base", "middle", "leaf"}) {
		t.Fatalf("order=%v", ids)
	}
}

func TestResolverRejectsMissingDependencyAndCycle(t *testing.T) {
	missing := fixtureEntry("missing")
	missing.RequiredDependencies = []string{"absent"}
	if _, err := resolveFixture(graphRegistry(missing), "missing"); err == nil {
		t.Fatal("missing dependency accepted")
	}
	left, right := fixtureEntry("left"), fixtureEntry("right")
	left.RequiredDependencies, right.RequiredDependencies = []string{"right"}, []string{"left"}
	_, err := resolveFixture(graphRegistry(left, right), "left")
	var resolutionErr *ResolutionError
	if !errors.As(err, &resolutionErr) || resolutionErr.Kind != "dependency_cycle" {
		t.Fatalf("cycle err=%v", err)
	}
}

func TestResolverExplainsMutualAndRoleConflicts(t *testing.T) {
	left, right := fixtureEntry("left"), fixtureEntry("right")
	left.Conflicts = []string{"right"}
	if _, err := resolveFixture(graphRegistry(left, right), "left", "right"); err == nil {
		t.Fatal("declared conflict accepted")
	}
	left.Conflicts = nil
	left.Role, right.Role = "visual_director", "visual_director"
	left.RoleMode, right.RoleMode = RoleExclusive, RoleExclusive
	left.Phase, right.Phase = PhaseArtDirection, PhaseArtDirection
	if _, err := resolveFixture(graphRegistry(left, right), "left", "right"); err == nil {
		t.Fatal("exclusive role conflict accepted")
	}
}

func TestResolverComposesComplementaryPhases(t *testing.T) {
	planning, implementation, security := fixtureEntry("planning"), fixtureEntry("implementation"), fixtureEntry("security")
	planning.Phase, implementation.Phase, security.Phase = PhasePlanning, PhaseImplementation, PhaseSecurity
	implementation.RequiredDependencies = []string{"planning"}
	security.RequiredDependencies = []string{"implementation"}
	resolution, err := resolveFixture(graphRegistry(security, implementation, planning), "security")
	if err != nil || len(resolution.Ordered) != 3 || len(resolution.Phases) != 3 {
		t.Fatalf("resolution=%+v err=%v", resolution, err)
	}
}

func TestResolverRejectsUnavailableCapabilityExecutorRiskAndOrchestrationAuthorityConflict(t *testing.T) {
	entry := fixtureEntry("entry")
	entry.Capabilities = []string{"network.read"}
	if _, err := resolveFixture(graphRegistry(entry), "entry"); err == nil {
		t.Fatal("unavailable capability accepted")
	}
	entry = fixtureEntry("entry")
	entry.Compatibility.Executors = []string{"claude"}
	if _, err := resolveFixture(graphRegistry(entry), "entry"); err == nil {
		t.Fatal("incompatible executor accepted")
	}
	entry = fixtureEntry("entry")
	entry.Risk = RiskCritical
	if _, err := resolveFixture(graphRegistry(entry), "entry"); err == nil {
		t.Fatal("risk beyond policy accepted")
	}
	left, right := fixtureEntry("left"), fixtureEntry("right")
	left.Phase, right.Phase = PhaseOrchestration, PhaseOrchestration
	left.Role, right.Role = "control_plane", "advisor"
	if _, err := resolveFixture(graphRegistry(left, right), "left", "right"); err == nil {
		t.Fatal("orchestration authority conflict accepted")
	}
}
