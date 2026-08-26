package migration

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestRegistryResolvesSequentialMigrationsInOrder(t *testing.T) {
	var calls []string
	step := func(id string, from, to int) Step {
		return Step{ID: id, Artifact: ArtifactConfig, From: from, To: to,
			Apply:    func(context.Context, Workspace) error { calls = append(calls, "apply:"+id); return nil },
			Validate: func(context.Context, Workspace) error { calls = append(calls, "validate:"+id); return nil },
			Rollback: func(context.Context, Workspace) error { calls = append(calls, "rollback:"+id); return nil },
		}
	}
	registry := Registry{Steps: []Step{step("config-2-to-3", 2, 3), step("config-1-to-2", 1, 2)}}
	plan, err := registry.Resolve(Schemas{ArtifactConfig: 1}, Schemas{ArtifactConfig: 3})
	if err != nil {
		t.Fatal(err)
	}
	if got := []string{plan.Steps[0].ID, plan.Steps[1].ID}; !reflect.DeepEqual(got, []string{"config-1-to-2", "config-2-to-3"}) {
		t.Fatalf("migration order=%v", got)
	}
	if err := plan.Apply(context.Background(), Workspace{}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(calls, []string{"apply:config-1-to-2", "validate:config-1-to-2", "apply:config-2-to-3", "validate:config-2-to-3"}) {
		t.Fatalf("calls=%v", calls)
	}
}

func TestRegistryNoOpMigrationIsExplicitlyEmpty(t *testing.T) {
	plan, err := (Registry{}).Resolve(
		Schemas{ArtifactConfig: 1, ArtifactState: 1},
		Schemas{ArtifactConfig: 1, ArtifactState: 1},
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Steps) != 0 {
		t.Fatalf("no-op plan contains %d steps", len(plan.Steps))
	}
}

func TestRegistryAdvertisesOnlyReachableSourceSchemas(t *testing.T) {
	registry := Registry{Steps: []Step{
		{ID: "config-1-to-2", Artifact: ArtifactConfig, From: 1, To: 2, Apply: noStep, Validate: noStep, Rollback: noStep},
		{ID: "config-2-to-3", Artifact: ArtifactConfig, From: 2, To: 3, Apply: noStep, Validate: noStep, Rollback: noStep},
	}}
	supported, err := registry.SupportedSources(Schemas{ArtifactConfig: 3, ArtifactState: 1})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(supported[ArtifactConfig], []int{1, 2, 3}) || !reflect.DeepEqual(supported[ArtifactState], []int{1}) {
		t.Fatalf("supported=%v", supported)
	}
}

func TestRegistryOrdersCrossArtifactDependenciesAndRejectsCycles(t *testing.T) {
	step := func(id string, artifact Artifact, dependency string) Step {
		value := Step{ID: id, Artifact: artifact, From: 1, To: 2, Apply: noStep, Validate: noStep, Rollback: noStep}
		if dependency != "" {
			value.DependsOn = []string{dependency}
		}
		return value
	}
	registry := Registry{Steps: []Step{step("state", ArtifactState, "config"), step("config", ArtifactConfig, "")}}
	plan, err := registry.Resolve(Schemas{ArtifactConfig: 1, ArtifactState: 1}, Schemas{ArtifactConfig: 2, ArtifactState: 2})
	if err != nil {
		t.Fatal(err)
	}
	if got := []string{plan.Steps[0].ID, plan.Steps[1].ID}; !reflect.DeepEqual(got, []string{"config", "state"}) {
		t.Fatalf("order=%v", got)
	}
	supported, err := registry.SupportedSources(Schemas{ArtifactConfig: 2, ArtifactState: 2})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(supported[ArtifactState], []int{1, 2}) {
		t.Fatalf("state supported sources=%v", supported[ArtifactState])
	}
	cycle := Registry{Steps: []Step{step("config", ArtifactConfig, "state"), step("state", ArtifactState, "config")}}
	if _, err := cycle.Resolve(Schemas{ArtifactConfig: 1, ArtifactState: 1}, Schemas{ArtifactConfig: 2, ArtifactState: 2}); err == nil {
		t.Fatal("cross-artifact dependency cycle accepted")
	}
}

func TestRegistryRejectsMissingAmbiguousAndIrreversibleSteps(t *testing.T) {
	valid := Step{ID: "one", Artifact: ArtifactConfig, From: 1, To: 2, Apply: noStep, Validate: noStep, Rollback: noStep}
	for name, registry := range map[string]Registry{
		"missing":      {},
		"ambiguous":    {Steps: []Step{valid, {ID: "two", Artifact: ArtifactConfig, From: 1, To: 2, Apply: noStep, Validate: noStep, Rollback: noStep}}},
		"irreversible": {Steps: []Step{{ID: "bad", Artifact: ArtifactConfig, From: 1, To: 2, Apply: noStep, Validate: noStep}}},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := registry.Resolve(Schemas{ArtifactConfig: 1}, Schemas{ArtifactConfig: 2}); err == nil {
				t.Fatal("invalid migration graph accepted")
			}
		})
	}
}

func TestPlanValidationFailureRunsStepRollback(t *testing.T) {
	rolledBack := false
	plan, err := (Registry{Steps: []Step{{ID: "failing", Artifact: ArtifactConfig, From: 1, To: 2,
		Apply:    noStep,
		Validate: func(context.Context, Workspace) error { return errors.New("invalid") },
		Rollback: func(context.Context, Workspace) error { rolledBack = true; return nil },
	}}}).Resolve(Schemas{ArtifactConfig: 1}, Schemas{ArtifactConfig: 2})
	if err != nil {
		t.Fatal(err)
	}
	if err := plan.Apply(context.Background(), Workspace{}); err == nil || !rolledBack {
		t.Fatalf("validation failure was not compensated: %v", err)
	}
}

func TestPlanApplyAndLaterPreconditionFailuresCompensatePriorWork(t *testing.T) {
	for name, second := range map[string]Step{
		"partial apply": {
			ID: "second", Artifact: ArtifactConfig, From: 2, To: 3,
			Apply: func(context.Context, Workspace) error { return errors.New("partial") }, Validate: noStep, Rollback: noStep,
		},
		"later precondition": {
			ID: "second", Artifact: ArtifactConfig, From: 2, To: 3,
			Precondition: func(context.Context, Workspace) error { return errors.New("blocked") }, Apply: noStep, Validate: noStep, Rollback: noStep,
		},
	} {
		t.Run(name, func(t *testing.T) {
			var rollbacks []string
			first := Step{ID: "first", Artifact: ArtifactConfig, From: 1, To: 2, Apply: noStep, Validate: noStep, Rollback: func(context.Context, Workspace) error {
				rollbacks = append(rollbacks, "first")
				return nil
			}}
			second.Rollback = func(context.Context, Workspace) error {
				rollbacks = append(rollbacks, "second")
				return nil
			}
			plan, err := (Registry{Steps: []Step{first, second}}).Resolve(Schemas{ArtifactConfig: 1}, Schemas{ArtifactConfig: 3})
			if err != nil {
				t.Fatal(err)
			}
			if err := plan.Apply(context.Background(), Workspace{}); err == nil {
				t.Fatal("failing plan succeeded")
			}
			if len(rollbacks) == 0 || rollbacks[len(rollbacks)-1] != "first" {
				t.Fatalf("prior step was not compensated: %v", rollbacks)
			}
		})
	}
}

func TestMigrationStepCanUseBoundedWorkspace(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "config.toml")
	if err := os.WriteFile(path, []byte("schema=1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	step := Step{ID: "config-1-to-2", Artifact: ArtifactConfig, From: 1, To: 2,
		Apply: func(_ context.Context, workspace Workspace) error {
			return os.WriteFile(workspace.Files[ArtifactConfig], []byte("schema=2\n"), 0o600)
		},
		Validate: func(_ context.Context, workspace Workspace) error {
			data, err := os.ReadFile(workspace.Files[ArtifactConfig])
			if err == nil && string(data) != "schema=2\n" {
				err = errors.New("wrong schema")
			}
			return err
		},
		Rollback: func(_ context.Context, workspace Workspace) error {
			return os.WriteFile(workspace.Files[ArtifactConfig], []byte("schema=1\n"), 0o600)
		},
	}
	plan, err := (Registry{Steps: []Step{step}}).Resolve(Schemas{ArtifactConfig: 1}, Schemas{ArtifactConfig: 2})
	if err != nil {
		t.Fatal(err)
	}
	if err := plan.Apply(context.Background(), Workspace{Files: map[Artifact]string{ArtifactConfig: path}}); err != nil {
		t.Fatal(err)
	}
}

func noStep(context.Context, Workspace) error { return nil }
