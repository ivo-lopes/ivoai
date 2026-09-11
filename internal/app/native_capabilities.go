package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ivo-lopes/ivoai/internal/config"
	"github.com/ivo-lopes/ivoai/internal/policy"
	"github.com/ivo-lopes/ivoai/internal/skillcatalog"
	"github.com/ivo-lopes/ivoai/internal/skills"
	"github.com/ivo-lopes/ivoai/internal/skillupdate"
	"github.com/ivo-lopes/ivoai/internal/supplychain"
	"golang.org/x/sys/unix"
)

type NativeCapabilityStatus struct {
	ID                string   `json:"id"`
	Name              string   `json:"name"`
	Type              string   `json:"type"`
	Status            string   `json:"status"`
	Revision          string   `json:"revision"`
	AvailableRevision string   `json:"available_revision"`
	UpdateAvailable   bool     `json:"update_available"`
	Pinned            bool     `json:"pinned"`
	Disabled          bool     `json:"disabled"`
	Skills            []string `json:"skills"`
	Risk              string   `json:"risk"`
	Compatibility     []string `json:"compatibility"`
	SelectionPolicy   string   `json:"selection_policy"`
}

func (a *App) nativeManager(id string) (skillupdate.Manager, error) {
	source, err := skillcatalog.NativeSource(id)
	if err != nil {
		return skillupdate.Manager{}, err
	}
	return skillupdate.Manager{Supply: supplychain.Manager{Root: filepath.Join(a.Store.Paths.DataDir, "supply-chain")}, Registry: skills.Store{Path: skills.RegistryPath(a.Store.Paths.StateDir)}, Discoverer: source, Fetcher: source, Classifier: skillcatalog.NativeClassifier{}, Policy: policy.DefaultEngine(), CatalogOnly: true, ReconcileMissingIndex: true}, nil
}

func (a *App) NativeCapabilities(ctx context.Context) ([]NativeCapabilityStatus, error) {
	cfg, err := a.Store.Load()
	if err != nil {
		return nil, err
	}
	catalog, err := skillcatalog.Load()
	if err != nil {
		return nil, err
	}
	var result []NativeCapabilityStatus
	for _, id := range skillcatalog.NativeIDs() {
		source, _ := catalog.Source(id)
		preference := cfg.Skills.Sources[id]
		row := NativeCapabilityStatus{ID: id, Name: source.DisplayName, Type: "skill pack", Status: "available", AvailableRevision: source.Provenance.Revision, Pinned: preference.Pinned, Disabled: preference.Disabled}
		if id == "i-have-adhd" {
			row.Type = "interaction profile"
		} else if id == "ponytail" {
			row.Type = "efficiency capability"
		}
		for index, c := range source.Classifications {
			row.Skills = append(row.Skills, c.CanonicalID)
			if index == 0 {
				row.Risk = string(c.Risk)
			} else if row.Risk != string(c.Risk) {
				row.Risk = "mixed (per capability)"
			}
			row.Compatibility = c.Executors
			decision := policy.DefaultEngine().Evaluate(policy.Request{SubjectID: c.CanonicalID, SubjectKind: policy.SubjectSkill, DeclaredCapabilities: c.RequestedCapabilities, RequestedCapabilities: c.RequestedCapabilities, Risk: c.Risk, MetadataValid: true, ConflictResolved: true})
			if index == 0 {
				row.SelectionPolicy = string(decision.Decision)
			} else if row.SelectionPolicy != string(decision.Decision) {
				row.SelectionPolicy = "mixed (per capability)"
			}
		}
		manager, err := a.nativeManager(id)
		if err != nil {
			return nil, err
		}
		active, _, err := manager.Supply.Active(id)
		if err == nil {
			row.Revision = active.Revision
			row.UpdateAvailable = active.Revision != source.Provenance.Revision
			row.Status = "ready"
			if manager.ValidateConsistency(ctx, id) != nil {
				row.Status = "integrity-failure"
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			row.Status = "integrity-failure"
		} else if registry, loadErr := manager.Registry.Load(); loadErr == nil {
			for _, entry := range registry.Entries {
				if entry.ArtifactID == id && entry.Lifecycle == skills.LifecycleQuarantined {
					row.Status = "quarantined"
				}
			}
		}
		if row.Disabled && row.Status != "integrity-failure" && row.Status != "quarantined" {
			row.Status = "disabled"
		}
		result = append(result, row)
	}
	return result, nil
}

// NativeCapabilityAction is shared by setup/update, CLI and TUI. It manages
// only the release-owned pack, never personal provider skill directories.
func (a *App) NativeCapabilityAction(ctx context.Context, action, id string) error {
	if err := a.Store.Ensure(); err != nil {
		return err
	}
	lockPath := filepath.Join(a.Store.Paths.StateDir, "native-capabilities.lock")
	fd, err := unix.Open(lockPath, unix.O_CREAT|unix.O_RDWR|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0600)
	if err != nil {
		return err
	}
	defer unix.Close(fd)
	if err := unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB); err != nil {
		return errors.New("native capability management is already running")
	}
	defer unix.Flock(fd, unix.LOCK_UN)
	ids := []string{id}
	if id == "" {
		if action != "update" {
			return errors.New("capability source ID required")
		}
		ids = skillcatalog.NativeIDs()
	}
	cfg, err := a.Store.Load()
	if err != nil {
		return err
	}
	for _, sourceID := range ids {
		manager, err := a.nativeManager(sourceID)
		if err != nil {
			return err
		}
		preference := cfg.Skills.Sources[sourceID]
		switch action {
		case "enable", "disable", "pin", "unpin":
			if action == "enable" {
				preference.Disabled = false
			}
			if action == "disable" {
				preference.Disabled = true
			}
			if action == "pin" {
				preference.Pinned = true
			}
			if action == "unpin" {
				preference.Pinned = false
			}
			if cfg.Skills.Sources == nil {
				cfg.Skills.Sources = map[string]config.SkillSourcePolicy{}
			}
			cfg.Skills.Sources[sourceID] = preference
			if err := a.Store.Save(cfg); err != nil {
				return err
			}
		case "update":
			if preference.Pinned {
				continue
			}
			// A matching name alone does not establish release ownership.
			if active, root, activeErr := manager.Supply.Active(sourceID); activeErr == nil {
				if _, err := (skillcatalog.NativeClassifier{}).Classify(ctx, active, root); err != nil {
					return fmt.Errorf("native source %s: existing object is not a valid release-owned pack; preserved", sourceID)
				}
			} else if !errors.Is(activeErr, os.ErrNotExist) {
				return fmt.Errorf("native source %s: existing object integrity failure; preserved", sourceID)
			}
			source, _ := skillcatalog.NativeSource(sourceID)
			if _, err := manager.Update(ctx, supplychain.Reference{ID: sourceID, Kind: supplychain.KindSkill, Source: source.Source.Upstream.Repository}); err != nil {
				if quarantineErr := quarantineMissingNative(manager, sourceID); quarantineErr != nil {
					return fmt.Errorf("native source %s: materialization failed; quarantine metadata could not be saved", sourceID)
				}
				return fmt.Errorf("native source %s: %w", sourceID, err)
			}
		case "rollback":
			changed, err := manager.Rollback(ctx, sourceID)
			if err != nil {
				return err
			}
			if !changed {
				return errors.New("no previous native revision is available")
			}
		default:
			return errors.New("unknown capability action")
		}
	}
	return nil
}

func quarantineMissingNative(manager skillupdate.Manager, id string) error {
	if _, _, err := manager.Supply.Active(id); !errors.Is(err, os.ErrNotExist) {
		return nil // Never replace a valid active revision or a corrupt pointer.
	}
	registry, err := manager.Registry.Load()
	if err != nil {
		return err
	}
	entries, err := skillcatalog.NativeQuarantine(id)
	if err != nil {
		return err
	}
	for _, candidate := range entries {
		for _, existing := range registry.Entries {
			if existing.ID == candidate.ID && existing.ArtifactID != id {
				return nil // A personal collision is preserved, not adopted.
			}
		}
	}
	next := registry
	next.Entries = nil
	for _, entry := range registry.Entries {
		if entry.ArtifactID != id {
			next.Entries = append(next.Entries, entry)
		}
	}
	next.Entries = append(next.Entries, entries...)
	return manager.Registry.Save(next)
}

func (a *App) PrintNativeCapabilities(ctx context.Context, id string) error {
	rows, err := a.NativeCapabilities(ctx)
	if err != nil {
		return err
	}
	for _, row := range rows {
		if id != "" && row.ID != id {
			continue
		}
		fmt.Fprintf(a.Out, "%s (%s): %s | type=%s | risk=%s | executors=%s | policy=%s | pinned=%t | update=%t\n", row.Name, row.ID, row.Status, row.Type, row.Risk, strings.Join(row.Compatibility, ","), row.SelectionPolicy, row.Pinned, row.UpdateAvailable)
		if id != "" {
			fmt.Fprintf(a.Out, "Revision: %s\nAvailable: %s\nSkills: %s\nProvenance: immutable commit + local SHA-256 (not an independent signature)\n", row.Revision, row.AvailableRevision, strings.Join(row.Skills, ", "))
			return nil
		}
	}
	if id != "" {
		return errors.New("unknown native capability source")
	}
	counts := map[string]int{}
	for _, row := range rows {
		counts[row.Status]++
	}
	fmt.Fprintf(a.Out, "Native Pack: %d ready / %d available / %d disabled / %d quarantined / %d integrity failures\n", counts["ready"], counts["available"], counts["disabled"], counts["quarantined"], counts["integrity-failure"])
	return nil
}
