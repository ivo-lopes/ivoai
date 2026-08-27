// Package skillupdate coordinates safe, non-executing skill-pack updates on
// top of the generic supply-chain manager. Upstream archives and skill text
// remain untrusted data throughout this package.
package skillupdate

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/ivo-lopes/ivoai/internal/platform"
	"github.com/ivo-lopes/ivoai/internal/policy"
	"github.com/ivo-lopes/ivoai/internal/skills"
	"github.com/ivo-lopes/ivoai/internal/supplychain"
)

const maxSkillContentBytes = 1 << 20

type Classifier interface {
	Classify(context.Context, supplychain.ResolvedSource, string) ([]skills.Entry, error)
}

type ClassifierFunc func(context.Context, supplychain.ResolvedSource, string) ([]skills.Entry, error)

func (f ClassifierFunc) Classify(ctx context.Context, source supplychain.ResolvedSource, root string) ([]skills.Entry, error) {
	return f(ctx, source, root)
}

type Smoke interface {
	Validate(context.Context, supplychain.ResolvedSource, string, []skills.Entry) error
}

type SmokeFunc func(context.Context, supplychain.ResolvedSource, string, []skills.Entry) error

func (f SmokeFunc) Validate(ctx context.Context, source supplychain.ResolvedSource, root string, entries []skills.Entry) error {
	return f(ctx, source, root, entries)
}

type Manager struct {
	Supply                supplychain.Manager
	Discoverer            supplychain.Discoverer
	Fetcher               supplychain.Fetcher
	Registry              skills.Store
	Classifier            Classifier
	Smoke                 Smoke
	Policy                policy.Engine
	Executor              string
	AvailableCapabilities map[string]bool
	MaximumRisk           skills.RiskTier
	Doctor                func(context.Context, string) error
}

type Result struct {
	ArtifactID string
	Revision   string
	Changed    bool
	Skills     []string
}

func (m Manager) Update(ctx context.Context, reference supplychain.Reference) (Result, error) {
	if err := m.validate(); err != nil {
		return Result{}, err
	}
	if _, err := m.Recover(ctx); err != nil {
		return Result{}, fmt.Errorf("recover interrupted skill update: %w", err)
	}
	resolved, err := m.Discoverer.Resolve(ctx, reference)
	if err != nil {
		return Result{}, fmt.Errorf("discover skill pack: %w", err)
	}
	if err := resolved.Validate(); err != nil {
		return Result{}, err
	}
	if err := matchReference(reference, resolved); err != nil {
		return Result{}, err
	}
	if trust := m.Policy.EvaluateTrust(policy.TrustRequest{SubjectID: resolved.ID, TrustLevel: resolved.Integrity.TrustLevel, SignatureStatus: resolved.Integrity.SignatureStatus, AttestationStatus: resolved.Integrity.AttestationStatus, Automatic: true}); trust.Decision != policy.Allow {
		return Result{}, fmt.Errorf("automatic skill update denied: %s", trust.Reason)
	}
	if active, _, activeErr := m.Supply.Active(resolved.ID); activeErr == nil && active.Revision == resolved.Revision {
		if err := m.ValidateConsistency(ctx, resolved.ID); err != nil {
			return Result{}, fmt.Errorf("no-change skill pack is inconsistent: %w", err)
		}
		entries, err := m.packEntries(resolved.ID)
		return resultFor(resolved, false, entries), err
	} else if activeErr != nil && !errors.Is(activeErr, os.ErrNotExist) {
		return Result{}, activeErr
	}

	pipeline := supplychain.Pipeline{Manager: m.pipelineManager(), Discoverer: fixedDiscoverer{source: resolved}, Fetcher: m.Fetcher}
	staged, err := pipeline.Prepare(ctx, reference)
	if err != nil {
		return Result{}, err
	}
	entries, err := m.classify(ctx, staged.Source, staged.ObjectPath)
	if err != nil {
		return Result{}, fmt.Errorf("classify immutable staged skill pack: %w", err)
	}
	if m.Smoke != nil {
		if err := m.Smoke.Validate(ctx, staged.Source, staged.ObjectPath, entries); err != nil {
			return Result{}, fmt.Errorf("deterministic staged skill smoke: %w", err)
		}
	} else if err := deterministicSmoke(staged.ObjectPath, entries); err != nil {
		return Result{}, fmt.Errorf("deterministic staged skill smoke: %w", err)
	}
	previous, err := m.Registry.Load()
	if err != nil {
		return Result{}, err
	}
	next, err := replacePack(previous, staged.Source, entries)
	if err != nil {
		return Result{}, err
	}
	activation := supplychain.Activation{
		Apply: func() error { return m.Registry.Save(next) },
		Validate: func() error {
			if err := m.ValidateConsistency(ctx, staged.Source.ID); err != nil {
				return err
			}
			if m.Doctor != nil {
				return m.Doctor(ctx, staged.Source.ID)
			}
			return nil
		},
		Rollback: func() error { return m.Registry.Save(previous) },
	}
	if err := m.pipelineManager().PromoteWithActivation(staged, activation); err != nil {
		return Result{}, err
	}
	return resultFor(staged.Source, true, entries), nil
}

func (m Manager) Rollback(ctx context.Context, artifactID string) (bool, error) {
	if err := m.validate(); err != nil {
		return false, err
	}
	previousRegistry, err := m.Registry.Load()
	if err != nil {
		return false, err
	}
	var next skills.Registry
	activation := supplychain.Activation{
		Apply: func() error {
			source, root, err := m.Supply.Active(artifactID)
			if err != nil {
				return err
			}
			entries, err := m.classify(ctx, source, root)
			if err != nil {
				return err
			}
			next, err = replacePack(previousRegistry, source, entries)
			if err != nil {
				return err
			}
			return m.Registry.Save(next)
		},
		Validate: func() error {
			if err := m.ValidateConsistency(ctx, artifactID); err != nil {
				return err
			}
			if m.Doctor != nil {
				return m.Doctor(ctx, artifactID)
			}
			return nil
		},
		Rollback: func() error { return m.Registry.Save(previousRegistry) },
	}
	return m.pipelineManager().RollbackWithActivation(artifactID, activation)
}

func (m Manager) Recover(ctx context.Context) (int, error) {
	if err := m.validate(); err != nil {
		return 0, err
	}
	return m.Supply.RecoverWithActivation(func(artifactID string) error {
		registry, err := m.Registry.Load()
		if err != nil {
			return err
		}
		source, root, err := m.Supply.Active(artifactID)
		if errors.Is(err, os.ErrNotExist) {
			return m.Registry.Save(removePack(registry, artifactID, ""))
		}
		if err != nil {
			return err
		}
		entries, err := m.classify(ctx, source, root)
		if err != nil {
			return err
		}
		next, err := replacePack(registry, source, entries)
		if err != nil {
			return err
		}
		return m.Registry.Save(next)
	})
}

func (m Manager) ValidateConsistency(ctx context.Context, artifactID string) error {
	source, root, err := m.Supply.Active(artifactID)
	if err != nil {
		return err
	}
	entries, err := m.classify(ctx, source, root)
	if err != nil {
		return err
	}
	want, err := replacePack(skills.EmptyRegistry(), source, entries)
	if err != nil {
		return err
	}
	registry, err := m.Registry.Load()
	if err != nil {
		return err
	}
	actualPack := skills.Registry{Schema: skills.RegistrySchemaVersion, Entries: packEntries(registry, artifactID, source.Source)}
	if err := actualPack.Normalize(); err != nil {
		return err
	}
	want.UpdatedAt, actualPack.UpdatedAt = time.Time{}, time.Time{}
	left, _ := json.Marshal(want)
	right, _ := json.Marshal(actualPack)
	if string(left) != string(right) {
		return errors.New("skill Registry and active supply-chain pointer diverge")
	}
	return nil
}

func (m Manager) pipelineManager() supplychain.Manager {
	manager := m.Supply
	existingStructural, existingPolicy, existingHealth := manager.Structural, manager.Policy, manager.Health
	manager.Structural = supplychain.ValidatorFunc(func(ctx context.Context, source supplychain.ResolvedSource, root string) error {
		if existingStructural != nil {
			if err := existingStructural.Validate(ctx, source, root); err != nil {
				return err
			}
		}
		_, err := m.classify(ctx, source, root)
		return err
	})
	manager.Policy = supplychain.ValidatorFunc(func(ctx context.Context, source supplychain.ResolvedSource, root string) error {
		if existingPolicy != nil {
			if err := existingPolicy.Validate(ctx, source, root); err != nil {
				return err
			}
		}
		entries, err := m.classify(ctx, source, root)
		if err != nil {
			return err
		}
		return m.validatePolicy(entries)
	})
	manager.Health = supplychain.ValidatorFunc(func(ctx context.Context, source supplychain.ResolvedSource, root string) error {
		if existingHealth != nil {
			if err := existingHealth.Validate(ctx, source, root); err != nil {
				return err
			}
		}
		entries, err := m.classify(ctx, source, root)
		if err != nil {
			return err
		}
		if m.Smoke != nil {
			return m.Smoke.Validate(ctx, source, root, entries)
		}
		return deterministicSmoke(root, entries)
	})
	return manager
}

func (m Manager) classify(ctx context.Context, source supplychain.ResolvedSource, root string) ([]skills.Entry, error) {
	entries, err := m.Classifier.Classify(ctx, source, root)
	if err != nil {
		return nil, err
	}
	if len(entries) == 0 || len(entries) > skills.MaxRegistryEntries {
		return nil, errors.New("skill pack must classify to a bounded non-empty set")
	}
	for index := range entries {
		entry := &entries[index]
		entry.ArtifactID = source.ID
		entry.Lifecycle = skills.LifecycleStaged
		entry.QuarantineReason = ""
		entry.Provenance.Source.URL = source.Source
		entry.Provenance.Source.DefaultBranch = source.DefaultBranch
		entry.Provenance.Revision.Commit = source.Revision
		entry.Provenance.Revision.LogicalVersion = source.LogicalVersion
		if entry.Provenance.Revision.LogicalVersion == "" {
			entry.Provenance.Revision.LogicalVersion = source.Revision
		}
		entry.Provenance.Integrity = skills.Integrity{Algorithm: "sha256", Digest: source.Integrity.Digest, Verified: true, SignatureStatus: source.Integrity.SignatureStatus, AttestationStatus: source.Integrity.AttestationStatus, TrustLevel: source.Integrity.TrustLevel}
	}
	registry := skills.Registry{Schema: skills.RegistrySchemaVersion, Entries: entries}
	if err := registry.Normalize(); err != nil {
		return nil, err
	}
	return registry.Entries, nil
}

func (m Manager) validatePolicy(entries []skills.Entry) error {
	ids := make([]string, 0, len(entries))
	for _, entry := range entries {
		ids = append(ids, entry.ID)
	}
	registry := skills.Registry{Schema: skills.RegistrySchemaVersion, Entries: entries}
	resolution, err := (skills.Resolver{Registry: registry}).Resolve(skills.ResolutionRequest{IDs: ids, Executor: m.Executor, AvailableCapabilities: m.AvailableCapabilities, MaximumRisk: m.maximumRisk()})
	if err != nil {
		return err
	}
	for _, entry := range resolution.Ordered {
		result := m.Policy.Evaluate(policy.Request{SubjectID: entry.ID, SubjectKind: policy.SubjectSkill, DeclaredCapabilities: entry.Capabilities, RequestedCapabilities: entry.Capabilities, Risk: entry.Risk, Scope: "skill_update", MetadataValid: true, ConflictResolved: true})
		if result.Decision != policy.Allow {
			return fmt.Errorf("skill %s policy %s: %s", entry.ID, result.Decision, result.Reason)
		}
	}
	return nil
}

func (m Manager) validate() error {
	if m.Discoverer == nil || m.Fetcher == nil || m.Classifier == nil || m.Registry.Path == "" || m.Supply.Root == "" {
		return errors.New("skill update manager is incomplete")
	}
	if err := m.Policy.Validate(); err != nil {
		return err
	}
	return nil
}

func (m Manager) maximumRisk() skills.RiskTier {
	if m.MaximumRisk == "" {
		return skills.RiskModerate
	}
	return m.MaximumRisk
}

func matchReference(reference supplychain.Reference, source supplychain.ResolvedSource) error {
	if source.ID != reference.ID || source.Kind != reference.Kind || reference.Source != "" && reference.Source != source.Source || reference.Version != "" && reference.Version != source.Revision && reference.Version != source.LogicalVersion {
		return errors.New("resolved source does not match requested skill pack")
	}
	return nil
}

func replacePack(registry skills.Registry, source supplychain.ResolvedSource, entries []skills.Entry) (skills.Registry, error) {
	next := removePack(registry, source.ID, source.Source)
	for index := range entries {
		entries[index].Lifecycle = skills.LifecycleActive
	}
	next.Entries = append(next.Entries, entries...)
	next.UpdatedAt = time.Now().UTC()
	if err := next.Normalize(); err != nil {
		return skills.Registry{}, err
	}
	return next, nil
}

func removePack(registry skills.Registry, artifactID, sourceURL string) skills.Registry {
	result := skills.Registry{Schema: skills.RegistrySchemaVersion, UpdatedAt: registry.UpdatedAt, Entries: make([]skills.Entry, 0, len(registry.Entries))}
	for _, entry := range registry.Entries {
		if entry.ArtifactID == artifactID || entry.ArtifactID == "" && sourceURL != "" && entry.Provenance.Source.URL == sourceURL {
			continue
		}
		result.Entries = append(result.Entries, entry)
	}
	return result
}

func packEntries(registry skills.Registry, artifactID, sourceURL string) []skills.Entry {
	var result []skills.Entry
	for _, entry := range registry.Entries {
		if entry.ArtifactID == artifactID || entry.ArtifactID == "" && sourceURL != "" && entry.Provenance.Source.URL == sourceURL {
			result = append(result, entry)
		}
	}
	return result
}

func (m Manager) packEntries(artifactID string) ([]skills.Entry, error) {
	registry, err := m.Registry.Load()
	if err != nil {
		return nil, err
	}
	return packEntries(registry, artifactID, ""), nil
}

func deterministicSmoke(root string, entries []skills.Entry) error {
	seen := map[string]bool{}
	for _, entry := range entries {
		if seen[entry.ID] {
			return errors.New("deterministic smoke found duplicate skill ID")
		}
		seen[entry.ID] = true
		path := strings.TrimSpace(entry.Provenance.Source.Path)
		if path == "" {
			return errors.New("deterministic smoke found an empty skill path")
		}
		clean := filepath.Clean(path)
		if filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
			return errors.New("deterministic smoke found an unsafe skill path")
		}
		if _, err := platform.ReadRegularFile(filepath.Join(root, clean), maxSkillContentBytes); err != nil {
			return err
		}
	}
	return nil
}

func resultFor(source supplychain.ResolvedSource, changed bool, entries []skills.Entry) Result {
	ids := make([]string, 0, len(entries))
	for _, entry := range entries {
		ids = append(ids, entry.ID)
	}
	sort.Strings(ids)
	return Result{ArtifactID: source.ID, Revision: source.Revision, Changed: changed, Skills: ids}
}

type fixedDiscoverer struct{ source supplychain.ResolvedSource }

func (f fixedDiscoverer) Resolve(context.Context, supplychain.Reference) (supplychain.ResolvedSource, error) {
	return f.source, nil
}
