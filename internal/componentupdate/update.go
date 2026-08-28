// Package componentupdate coordinates managed external executables on top of
// the shared supply-chain transaction manager. Downloaded payloads are treated
// as untrusted data and are never executed during staging.
package componentupdate

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ivo-lopes/ivoai/internal/config"
	"github.com/ivo-lopes/ivoai/internal/platform"
	"github.com/ivo-lopes/ivoai/internal/supplychain"
)

type Manager struct {
	Supply         supplychain.Manager
	Discoverer     supplychain.Discoverer
	Fetcher        supplychain.Fetcher
	Store          *config.Store
	Runner         platform.Runner
	Executable     string
	VersionArg     []string
	NoVersionProbe bool
	Timeout        time.Duration
}

type Result struct {
	State    config.ComponentState
	Revision string
	License  string
	Changed  bool
}

func (m Manager) Update(ctx context.Context, reference supplychain.Reference) (Result, error) {
	if err := m.validate(); err != nil {
		return Result{}, err
	}
	if _, err := m.Recover(ctx); err != nil {
		return Result{}, fmt.Errorf("recover interrupted component update: %w", err)
	}
	state, err := m.Store.LoadState()
	if err != nil {
		return Result{}, err
	}
	current := state.Components[reference.ID]
	if !current.Managed {
		if external, lookupErr := m.Runner.LookPath(m.Executable); lookupErr == nil && external != "" {
			version := m.probeVersion(ctx, external)
			return Result{State: config.ComponentState{Installed: true, Managed: false, Version: version, Path: external}}, nil
		}
	}
	resolved, err := m.Discoverer.Resolve(ctx, reference)
	if err != nil {
		return Result{}, fmt.Errorf("discover managed component: %w", err)
	}
	if err := resolved.Validate(); err != nil {
		return Result{}, err
	}
	if resolved.ID != reference.ID || resolved.Kind != supplychain.KindComponent || reference.Source != "" && resolved.Source != reference.Source || reference.Version != "" && reference.Version != resolved.Revision && reference.Version != resolved.LogicalVersion {
		return Result{}, errors.New("resolved component does not match requested artifact")
	}
	manager := m.pipelineManager()
	if active, root, activeErr := manager.Active(resolved.ID); activeErr == nil && active.Revision == resolved.Revision {
		result, err := m.activate(ctx, active, root, true)
		if err != nil {
			return Result{}, fmt.Errorf("no-change managed component is inconsistent: %w", err)
		}
		result.Changed = false
		return result, nil
	} else if activeErr != nil && !errors.Is(activeErr, os.ErrNotExist) {
		return Result{}, activeErr
	}

	pipeline := supplychain.Pipeline{Manager: manager, Discoverer: fixedDiscoverer{source: resolved}, Fetcher: m.Fetcher}
	staged, err := pipeline.Prepare(ctx, reference)
	if err != nil {
		return Result{}, err
	}
	previousState, err := m.Store.LoadState()
	if err != nil {
		return Result{}, err
	}
	previousOwnership, err := m.Store.LoadOwnership()
	if err != nil {
		return Result{}, err
	}
	activation := supplychain.Activation{
		Apply: func() error {
			_, err := m.activate(ctx, staged.Source, staged.ObjectPath, true)
			return err
		},
		Validate: func() error { return m.validateConsistency(staged.Source, staged.ObjectPath) },
		Rollback: func() error { return m.restore(previousState, previousOwnership) },
	}
	if err := manager.PromoteWithActivation(staged, activation); err != nil {
		return Result{}, err
	}
	return m.result(staged.Source, staged.ObjectPath, true), nil
}

func (m Manager) Rollback(ctx context.Context, id string) (bool, error) {
	if err := m.validate(); err != nil {
		return false, err
	}
	previousState, err := m.Store.LoadState()
	if err != nil {
		return false, err
	}
	previousOwnership, err := m.Store.LoadOwnership()
	if err != nil {
		return false, err
	}
	manager := m.pipelineManager()
	activation := supplychain.Activation{
		Apply: func() error {
			source, root, err := manager.Active(id)
			if err != nil {
				return err
			}
			_, err = m.activate(ctx, source, root, true)
			return err
		},
		Validate: func() error {
			source, root, err := manager.Active(id)
			if err != nil {
				return err
			}
			return m.validateConsistency(source, root)
		},
		Rollback: func() error { return m.restore(previousState, previousOwnership) },
	}
	return manager.RollbackWithActivation(id, activation)
}

func (m Manager) Recover(ctx context.Context) (int, error) {
	if err := m.validate(); err != nil {
		return 0, err
	}
	manager := m.pipelineManager()
	return manager.RecoverWithActivation(func(id string) error {
		source, root, err := manager.Active(id)
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		_, err = m.activate(ctx, source, root, true)
		return err
	})
}

func (m Manager) activate(ctx context.Context, source supplychain.ResolvedSource, root string, persist bool) (Result, error) {
	if err := m.validateConsistency(source, root); err != nil {
		return Result{}, err
	}
	result := m.result(source, root, persist)
	if !persist {
		return result, nil
	}
	state, err := m.Store.LoadState()
	if err != nil {
		return Result{}, err
	}
	ownership, err := m.Store.LoadOwnership()
	if err != nil {
		return Result{}, err
	}
	state.Components[source.ID] = result.State
	ownership.Components[source.ID] = config.OwnedItem{Managed: true, Path: result.State.Path}
	if err := m.Store.SaveState(state); err != nil {
		return Result{}, err
	}
	if err := m.Store.SaveOwnership(ownership); err != nil {
		return Result{}, err
	}
	return result, nil
}

func (m Manager) result(source supplychain.ResolvedSource, root string, changed bool) Result {
	return Result{State: config.ComponentState{Installed: true, Managed: true, Version: source.LogicalVersion, Path: filepath.Join(root, filepath.FromSlash(source.PayloadPath))}, Revision: source.Revision, License: source.License, Changed: changed}
}

func (m Manager) validateConsistency(source supplychain.ResolvedSource, root string) error {
	path := filepath.Join(root, filepath.FromSlash(source.PayloadPath))
	info, err := os.Lstat(path)
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || info.Mode().Perm() != 0o700 {
		return errors.New("managed component executable is missing or unsafe")
	}
	return nil
}

func (m Manager) pipelineManager() supplychain.Manager {
	manager := m.Supply
	manager.Structural = supplychain.ValidatorFunc(func(_ context.Context, source supplychain.ResolvedSource, root string) error {
		return m.validateConsistency(source, root)
	})
	manager.Policy = supplychain.ValidatorFunc(func(_ context.Context, source supplychain.ResolvedSource, _ string) error {
		if source.Integrity.TrustLevel != "checksum_only" && source.Integrity.TrustLevel != "upstream_checksum" && source.Integrity.TrustLevel != "independently_attested" {
			return errors.New("managed component trust level is not allowed")
		}
		return nil
	})
	manager.Health = supplychain.ValidatorFunc(func(ctx context.Context, source supplychain.ResolvedSource, root string) error {
		if err := m.validateConsistency(source, root); err != nil {
			return err
		}
		path := filepath.Join(root, filepath.FromSlash(source.PayloadPath))
		if m.NoVersionProbe {
			return nil
		}
		result, err := m.Runner.Run(ctx, path, m.VersionArg, platform.RunOptions{Timeout: m.timeout()})
		if err != nil {
			return err
		}
		if source.LogicalVersion != "" && !strings.Contains(strings.ToLower(result.Stdout+result.Stderr), strings.ToLower(source.LogicalVersion)) {
			return fmt.Errorf("version probe did not report %s", source.LogicalVersion)
		}
		return nil
	})
	return manager
}

func (m Manager) validate() error {
	if m.Store == nil || m.Runner == nil || m.Discoverer == nil || m.Fetcher == nil || m.Executable == "" || !m.NoVersionProbe && len(m.VersionArg) == 0 {
		return errors.New("managed component updater is incomplete")
	}
	return nil
}

func (m Manager) restore(state config.State, ownership config.Ownership) error {
	return errors.Join(m.Store.SaveState(state), m.Store.SaveOwnership(ownership))
}

func (m Manager) timeout() time.Duration {
	if m.Timeout > 0 {
		return m.Timeout
	}
	return 10 * time.Second
}

func (m Manager) probeVersion(ctx context.Context, path string) string {
	if m.NoVersionProbe {
		return "unknown"
	}
	result, err := m.Runner.Run(ctx, path, m.VersionArg, platform.RunOptions{Timeout: m.timeout()})
	if err != nil {
		return "unknown"
	}
	return strings.TrimSpace(result.Stdout)
}

type fixedDiscoverer struct{ source supplychain.ResolvedSource }

func (f fixedDiscoverer) Resolve(context.Context, supplychain.Reference) (supplychain.ResolvedSource, error) {
	return f.source, nil
}

type StaticDiscoverer struct{ Source supplychain.ResolvedSource }

func (s StaticDiscoverer) Resolve(context.Context, supplychain.Reference) (supplychain.ResolvedSource, error) {
	return s.Source, nil
}

type HTTPFetcher struct {
	Client *http.Client
}

func (f HTTPFetcher) Fetch(ctx context.Context, source supplychain.ResolvedSource) (io.ReadCloser, error) {
	client := f.Client
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Minute}
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, source.Source, nil)
	if err != nil {
		return nil, err
	}
	response, err := client.Do(request)
	if err != nil {
		return nil, err
	}
	if response.StatusCode != http.StatusOK {
		response.Body.Close()
		return nil, fmt.Errorf("component download returned HTTP %d", response.StatusCode)
	}
	return response.Body, nil
}
