package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/ivo-lopes/ivoai/internal/config"
	"github.com/ivo-lopes/ivoai/internal/migration"
	"github.com/ivo-lopes/ivoai/internal/platform"
	"github.com/ivo-lopes/ivoai/internal/server"
	"github.com/ivo-lopes/ivoai/internal/skills"
	"github.com/ivo-lopes/ivoai/internal/supplychain"
	"github.com/ivo-lopes/ivoai/internal/update"
	"github.com/pelletier/go-toml/v2"
)

const updateCompatibilityProtocol = 1

type UpdateCompatibility struct {
	Protocol               int                          `json:"protocol_version"`
	Version                string                       `json:"version"`
	TargetSchemas          migration.Schemas            `json:"target_schemas"`
	SupportedSourceSchemas map[migration.Artifact][]int `json:"supported_source_schemas"`
	RollbackSafe           bool                         `json:"rollback_safe"`
}

type updateContext struct {
	root           string
	rollbackBinary string
	mode           string
	files          []migration.FileSpec
	allowedRoots   []string
	schemas        migration.Schemas
}

func (a *App) UpdateCompatibility() UpdateCompatibility {
	schemas := currentSchemas()
	supported, err := updateMigrationRegistry().SupportedSources(schemas)
	if err != nil {
		return UpdateCompatibility{Protocol: updateCompatibilityProtocol, Version: a.Version, TargetSchemas: schemas, SupportedSourceSchemas: map[migration.Artifact][]int{}, RollbackSafe: false}
	}
	return UpdateCompatibility{Protocol: updateCompatibilityProtocol, Version: a.Version, TargetSchemas: schemas, SupportedSourceSchemas: supported, RollbackSafe: true}
}

func (a *App) ApplyPreparedUpdateMigration(ctx context.Context) error {
	id := os.Getenv("IVOAI_UPDATE_TRANSACTION")
	root := filepath.Clean(os.Getenv("IVOAI_UPDATE_ROOT"))
	parent, err := strconv.Atoi(os.Getenv("IVOAI_UPDATE_PARENT_PID"))
	if err != nil || parent <= 1 || parent != os.Getppid() {
		return errors.New("invalid prepared update parent")
	}
	clientRoot := filepath.Join(a.Store.Paths.StateDir, "updates")
	layout := server.DefaultLayout(testServerRoot())
	serverRoot := filepath.Join(layout.DataDir, "updates")
	if root == "." || root != filepath.Clean(clientRoot) && root != filepath.Clean(serverRoot) {
		return errors.New("prepared update root does not match the managed installation")
	}
	executable, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve candidate executable: %w", err)
	}
	executable, err = filepath.EvalSymlinks(executable)
	if err != nil {
		return fmt.Errorf("resolve candidate executable path: %w", err)
	}
	executableRoot := filepath.Dir(executable)
	allowed := []string{executableRoot, a.Store.Paths.ConfigDir, a.Store.Paths.StateDir, a.Store.Paths.DataDir}
	if os.Getenv("IVOAI_TEST_MODE") == "1" {
		allowed = append(allowed, a.Store.Paths.CacheDir)
	}
	if root == filepath.Clean(serverRoot) {
		allowed = []string{executableRoot, layout.ConfigDir, layout.SystemdDir}
	}
	manager := migration.Manager{Root: root, AllowedRoots: allowed, Registry: updateMigrationRegistry()}
	return manager.ApplyPrepared(ctx, id)
}

func (a *App) transactionalUpdate(ctx context.Context, checker update.Checker) error {
	return a.transactionalUpdateMode(ctx, checker, false)
}

func (a *App) transactionalUpdateDryRun(ctx context.Context, checker update.Checker) error {
	return a.transactionalUpdateMode(ctx, checker, true)
}

func (a *App) transactionalUpdateMode(ctx context.Context, checker update.Checker, dryRun bool) error {
	executable, err := a.managedExecutable()
	if err != nil {
		return err
	}
	if dryRun {
		manager, _, found, journalErr := a.transactionManagerFromJournal(executable)
		if journalErr != nil {
			return fmt.Errorf("inspect update journal: %w", journalErr)
		}
		if found {
			pending, pendingErr := manager.NeedsRecovery()
			if pendingErr != nil {
				return fmt.Errorf("inspect update journal: %w", pendingErr)
			}
			if pending {
				return migration.ErrRecoveryRequired
			}
		}
	} else {
		if recovered, recoverErr := a.recoverInterruptedUpdate(ctx, executable); recoverErr != nil {
			return fmt.Errorf("recover interrupted update: %w", recoverErr)
		} else if recovered {
			fmt.Fprintln(a.Out, "recovered an interrupted ivoai update; rerun the command using the restored binary")
			return nil
		}
	}
	release, available, err := checker.Check(ctx, a.Version)
	if err != nil {
		return err
	}
	if !available {
		fmt.Fprintf(a.Out, "ivoai %s is current\n", a.Version)
		return nil
	}
	updateCtx, err := a.resolveUpdateContext(executable)
	if err != nil {
		return err
	}
	manager := migration.Manager{Root: updateCtx.root, Files: updateCtx.files, AllowedRoots: updateCtx.allowedRoots, Registry: updateMigrationRegistry(), Retention: 1}
	fmt.Fprintf(a.Out, "preparing ivoai %s (%s)\n", release.Version, release.URL)
	prepared, err := checker.Prepare(ctx, release, executable)
	if err != nil {
		return err
	}
	defer prepared.Close()
	metadata, err := a.probeUpdateCompatibility(ctx, prepared.Path)
	if err != nil {
		return fmt.Errorf("candidate compatibility preflight: %w", err)
	}
	if normalizeVersion(metadata.Version) != normalizeVersion(release.Version) {
		return fmt.Errorf("candidate compatibility version %q does not match release %q", metadata.Version, release.Version)
	}
	if err := validateCompatibility(updateCtx.schemas, metadata); err != nil {
		return err
	}
	if dryRun {
		snapshotBytes, preflightErr := manager.PreflightSnapshot()
		if preflightErr != nil {
			return fmt.Errorf("update snapshot preflight: %w", preflightErr)
		}
		fmt.Fprintf(a.Out, "update dry-run: %s -> %s\n", a.Version, release.Version)
		for _, artifact := range sortedSchemaArtifacts(metadata.TargetSchemas) {
			fmt.Fprintf(a.Out, "  %s schema %d -> %d\n", artifact, updateCtx.schemas[artifact], metadata.TargetSchemas[artifact])
		}
		fmt.Fprintf(a.Out, "  managed files eligible for snapshot: %d\n", len(updateCtx.files))
		fmt.Fprintf(a.Out, "  snapshot bytes: %d\n", snapshotBytes)
		fmt.Fprintln(a.Out, "compatible: no managed changes were committed (the checksum-verified candidate was staged and executed for bounded preflight)")
		return nil
	}
	tx, err := manager.Begin(ctx, a.Version, release.Version, updateCtx.schemas, metadata.TargetSchemas)
	if err != nil {
		return fmt.Errorf("prepare update transaction: %w", err)
	}
	fail := func(stage string, stageErr error) error {
		rollbackErr := tx.Rollback(context.Background())
		if rollbackErr != nil {
			return errors.Join(fmt.Errorf("%s: %w", stage, stageErr), fmt.Errorf("automatic rollback failed: %w", rollbackErr))
		}
		if reconcileErr := a.reconcileRollbackRuntime(executable, updateCtx.mode); reconcileErr != nil {
			return errors.Join(fmt.Errorf("%s: %w", stage, stageErr), fmt.Errorf("managed files were restored but runtime reconciliation failed: %w", reconcileErr))
		}
		return fmt.Errorf("%s: %w (the previous binary and managed state were restored)", stage, stageErr)
	}
	if err := checker.Promote(prepared, executable, updateCtx.rollbackBinary); err != nil {
		return fail("promote candidate binary", err)
	}
	if err := tx.MarkPromoted(); err != nil {
		return fail("record binary promotion", err)
	}
	if err := tx.MarkMigrating(); err != nil {
		return fail("start migrations", err)
	}
	env := []string{
		"IVOAI_UPDATE_TRANSACTION=" + tx.ID(),
		"IVOAI_UPDATE_ROOT=" + updateCtx.root,
		"IVOAI_UPDATE_PARENT_PID=" + strconv.Itoa(os.Getpid()),
	}
	if _, err := a.Runner.Run(ctx, executable, []string{"_update-migrate"}, platform.RunOptions{Env: env, Stderr: a.Err, Timeout: 5 * time.Minute, ParentDeathSignal: true}); err != nil {
		return fail("candidate migration", err)
	}
	if err := tx.MarkMigrated(); err != nil {
		return fail("record migrations", err)
	}
	setupArgs, doctorArgs := updateModeCommands(updateCtx.mode)
	if _, err := a.Runner.Run(ctx, executable, setupArgs, platform.RunOptions{Stderr: a.Err, Timeout: 30 * time.Minute, ParentDeathSignal: true}); err != nil {
		return fail("candidate setup", err)
	}
	if err := tx.MarkVerifying(); err != nil {
		return fail("record verification", err)
	}
	if _, err := a.Runner.Run(ctx, executable, doctorArgs, platform.RunOptions{Stderr: a.Err, Timeout: time.Minute, ParentDeathSignal: true}); err != nil {
		return fail("post-update doctor", err)
	}
	if err := tx.Commit(); err != nil {
		return fail("commit update", err)
	}
	fmt.Fprintf(a.Out, "update complete; transaction %s committed with rollback state in %s\n", tx.ID(), updateCtx.root)
	return nil
}

func (a *App) transactionalRollback(ctx context.Context) error {
	return a.transactionalRollbackMode(ctx, false)
}

func (a *App) transactionalRollbackMode(ctx context.Context, force bool) error {
	executable, err := a.managedExecutable()
	if err != nil {
		return err
	}
	manager, mode, found, err := a.transactionManagerFromJournal(executable)
	if err != nil {
		return err
	}
	var updateCtx updateContext
	if !found {
		updateCtx, err = a.resolveUpdateContext(executable)
		if err != nil {
			return err
		}
		manager = migration.Manager{Root: updateCtx.root, Files: updateCtx.files, AllowedRoots: updateCtx.allowedRoots, Registry: updateMigrationRegistry(), Retention: 1}
		mode = updateCtx.mode
	}
	var rolled bool
	if force {
		rolled, err = manager.RollbackLastForce(ctx)
	} else {
		rolled, err = manager.RollbackLast(ctx)
	}
	if err != nil {
		return fmt.Errorf("transaction rollback: %w", err)
	}
	if !rolled {
		legacy := updateCtx.rollbackBinary
		if _, statErr := os.Stat(legacy); errors.Is(statErr, os.ErrNotExist) && mode == "server" {
			legacy = filepath.Join(a.Store.Paths.StateDir, "updates", "ivoai.previous")
		}
		if err := (update.Checker{}).Rollback(ctx, executable, legacy); err != nil {
			return err
		}
	}
	if err := a.reconcileRollbackRuntime(executable, mode); err != nil {
		return fmt.Errorf("rollback restored managed state but runtime reconciliation failed: %w", err)
	}
	fmt.Fprintf(a.Out, "rollback complete; managed binary and state are compatible\n")
	return nil
}

func (a *App) reconcileRollbackRuntime(executable, mode string) error {
	setupArgs, doctorArgs := updateModeCommands(mode)
	ctx, cancel := context.WithTimeout(context.Background(), 31*time.Minute)
	defer cancel()
	if _, err := a.Runner.Run(ctx, executable, setupArgs, platform.RunOptions{Stderr: a.Err, Timeout: 30 * time.Minute, ParentDeathSignal: true}); err != nil {
		return fmt.Errorf("restore %s runtime: %w", mode, err)
	}
	if _, err := a.Runner.Run(ctx, executable, doctorArgs, platform.RunOptions{Stderr: a.Err, Timeout: time.Minute, ParentDeathSignal: true}); err != nil {
		return fmt.Errorf("post-rollback doctor: %w", err)
	}
	return nil
}

func (a *App) probeUpdateCompatibility(ctx context.Context, candidate string) (UpdateCompatibility, error) {
	result, err := a.Runner.Run(ctx, candidate, []string{"_update-metadata"}, platform.RunOptions{Timeout: 15 * time.Second})
	if err != nil {
		return UpdateCompatibility{}, err
	}
	var metadata UpdateCompatibility
	decoder := json.NewDecoder(strings.NewReader(result.Stdout))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&metadata); err != nil {
		return UpdateCompatibility{}, err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return UpdateCompatibility{}, errors.New("candidate compatibility metadata has trailing data")
	}
	return metadata, nil
}

func validateCompatibility(source migration.Schemas, metadata UpdateCompatibility) error {
	if metadata.Protocol != updateCompatibilityProtocol || !metadata.RollbackSafe {
		return errors.New("candidate does not provide the required rollback-safe compatibility protocol")
	}
	for artifact, sourceSchema := range source {
		targetSchema, ok := metadata.TargetSchemas[artifact]
		if !ok || targetSchema < sourceSchema {
			return fmt.Errorf("candidate does not support %s schema %d", artifact, sourceSchema)
		}
		supported := false
		for _, value := range metadata.SupportedSourceSchemas[artifact] {
			if value == sourceSchema {
				supported = true
				break
			}
		}
		if !supported {
			return fmt.Errorf("candidate has no migration path from %s schema %d", artifact, sourceSchema)
		}
	}
	return nil
}

func (a *App) resolveUpdateContext(executable string) (updateContext, error) {
	if executable == "" {
		var err error
		executable, err = a.managedExecutable()
		if err != nil {
			return updateContext{}, err
		}
	}
	root := ""
	if os.Getenv("IVOAI_TEST_MODE") == "1" {
		root = os.Getenv("IVOAI_SERVER_ROOT")
	}
	layout := server.DefaultLayout(root)
	serverConfig := filepath.Join(layout.ConfigDir, "server.toml")
	files := []migration.FileSpec{{Name: "executable", Artifact: migration.ArtifactExecutable, Path: executable, Root: filepath.Dir(executable), Executable: true}}
	serverInfo, serverErr := os.Lstat(serverConfig)
	if serverErr != nil && !errors.Is(serverErr, os.ErrNotExist) {
		return updateContext{}, fmt.Errorf("inspect server installation: %w", serverErr)
	}
	if serverErr == nil && (serverInfo.Mode()&os.ModeSymlink != 0 || !serverInfo.Mode().IsRegular()) {
		return updateContext{}, errors.New("server config must be a regular non-symlink file")
	}
	if serverErr == nil {
		updateRoot := filepath.Join(layout.DataDir, "updates")
		serverFiles := []struct{ name, path, root string }{
			{"server-config", serverConfig, layout.ConfigDir},
			{"gateway-config", filepath.Join(layout.ConfigDir, "gateway.json"), layout.ConfigDir},
			{"connectors", filepath.Join(layout.ConfigDir, "connectors.json"), layout.ConfigDir},
			{"compose", filepath.Join(layout.ConfigDir, "compose.yaml"), layout.ConfigDir},
			{"compose-arm64", filepath.Join(layout.ConfigDir, "compose.arm64.yaml"), layout.ConfigDir},
			{"gateway-unit", filepath.Join(layout.SystemdDir, "ivoai-gateway.service"), layout.SystemdDir},
			{"context-unit", filepath.Join(layout.SystemdDir, "ivoai-context.service"), layout.SystemdDir},
			{"dependencies-unit", filepath.Join(layout.SystemdDir, "ivoai-dependencies.service"), layout.SystemdDir},
		}
		for _, value := range serverFiles {
			files = append(files, migration.FileSpec{Name: value.name, Artifact: migration.ArtifactServer, Path: value.path, Root: value.root, Optional: value.name != "server-config"})
		}
		schemas := currentSchemas()
		data, err := platform.ReadRegularFile(serverConfig, 4<<20)
		if err != nil {
			return updateContext{}, err
		}
		var envelope struct {
			Protocol int `toml:"protocol_version"`
		}
		if err := toml.Unmarshal(data, &envelope); err != nil || envelope.Protocol <= 0 {
			return updateContext{}, errors.New("server config has an invalid protocol schema")
		}
		schemas[migration.ArtifactServer] = envelope.Protocol
		return updateContext{root: updateRoot, rollbackBinary: filepath.Join(updateRoot, "ivoai.previous"), mode: "server", files: files, allowedRoots: []string{filepath.Dir(executable), layout.ConfigDir, layout.SystemdDir}, schemas: schemas}, nil
	}
	mode := "client"
	updateRoot := filepath.Join(a.Store.Paths.StateDir, "updates")
	files = append(files,
		migration.FileSpec{Name: "config", Artifact: migration.ArtifactConfig, Path: a.Store.Paths.Config, Root: a.Store.Paths.ConfigDir, Optional: true},
		migration.FileSpec{Name: "state", Artifact: migration.ArtifactState, Path: a.Store.Paths.State, Root: a.Store.Paths.StateDir, Optional: true},
		migration.FileSpec{Name: "ownership", Artifact: migration.ArtifactOwnership, Path: a.Store.Paths.Ownership, Root: a.Store.Paths.StateDir, Optional: true},
		migration.FileSpec{Name: "skill-registry", Artifact: migration.ArtifactSkillRegistry, Path: skills.RegistryPath(a.Store.Paths.StateDir), Root: a.Store.Paths.StateDir, Optional: true},
	)
	supplyRoot := filepath.Join(a.Store.Paths.DataDir, "supply-chain")
	supplyPointers, supplyErr := supplychain.TransactionalPointerFiles(supplyRoot)
	if supplyErr != nil {
		return updateContext{}, fmt.Errorf("inspect supply-chain transaction participants: %w", supplyErr)
	}
	for _, pointer := range supplyPointers {
		id := strings.TrimSuffix(filepath.Base(pointer), ".json")
		files = append(files, migration.FileSpec{Name: "supply-chain-" + safeUpdateName(id), Artifact: migration.ArtifactSupplyChain, Path: pointer, Root: a.Store.Paths.DataDir})
	}
	state, stateErr := a.Store.LoadStateForUpdate()
	ownership, ownershipErr := a.Store.LoadOwnershipForUpdate()
	if stateErr != nil && !errors.Is(stateErr, os.ErrNotExist) {
		return updateContext{}, stateErr
	}
	if ownershipErr != nil && !errors.Is(ownershipErr, os.ErrNotExist) {
		return updateContext{}, ownershipErr
	}
	var names []string
	for name := range state.Components {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		component, owned := state.Components[name], ownership.Components[name]
		if name == "ivoai" || !component.Installed {
			continue
		}
		if component.Managed != owned.Managed || component.Managed && (component.Path == "" || component.Path != owned.Path) {
			return updateContext{}, fmt.Errorf("managed component %s has inconsistent state and ownership metadata", name)
		}
		if !component.Managed {
			continue
		}
		componentRoot := a.Store.Paths.DataDir
		if os.Getenv("IVOAI_TEST_MODE") == "1" && pathWithin(a.Store.Paths.CacheDir, component.Path) {
			componentRoot = a.Store.Paths.CacheDir
		}
		if !pathWithin(componentRoot, component.Path) {
			return updateContext{}, fmt.Errorf("managed component %s escapes the ivoai data root", name)
		}
		files = append(files, migration.FileSpec{Name: "component-" + safeUpdateName(name), Artifact: migration.ArtifactComponents, Path: component.Path, Root: componentRoot})
	}
	schemas := currentSchemas()
	inspected, err := a.Store.InspectSchemas()
	if err != nil {
		return updateContext{}, err
	}
	if inspected.Config > 0 {
		schemas[migration.ArtifactConfig] = inspected.Config
	}
	if inspected.State > 0 {
		schemas[migration.ArtifactState] = inspected.State
	}
	if inspected.Ownership > 0 {
		schemas[migration.ArtifactOwnership] = inspected.Ownership
	}
	allowedRoots := []string{filepath.Dir(executable), a.Store.Paths.ConfigDir, a.Store.Paths.StateDir, a.Store.Paths.DataDir}
	if os.Getenv("IVOAI_TEST_MODE") == "1" {
		allowedRoots = append(allowedRoots, a.Store.Paths.CacheDir)
	}
	return updateContext{root: updateRoot, rollbackBinary: filepath.Join(updateRoot, "ivoai.previous"), mode: mode, files: files, allowedRoots: allowedRoots, schemas: schemas}, nil
}

func (a *App) recoverInterruptedUpdate(ctx context.Context, executable string) (bool, error) {
	manager, mode, found, err := a.transactionManagerFromJournal(executable)
	if err != nil || !found {
		return false, err
	}
	recovered, err := manager.Recover(ctx)
	if err != nil || !recovered {
		return recovered, err
	}
	if err := a.reconcileRollbackRuntime(executable, mode); err != nil {
		return true, err
	}
	return true, nil
}

func (a *App) transactionManagerFromJournal(executable string) (migration.Manager, string, bool, error) {
	serverRoot := testServerRoot()
	layout := server.DefaultLayout(serverRoot)
	clientAllowed := []string{filepath.Dir(executable), a.Store.Paths.ConfigDir, a.Store.Paths.StateDir, a.Store.Paths.DataDir}
	if os.Getenv("IVOAI_TEST_MODE") == "1" {
		clientAllowed = append(clientAllowed, a.Store.Paths.CacheDir)
	}
	candidates := []struct {
		root    string
		mode    string
		allowed []string
	}{
		{filepath.Join(a.Store.Paths.StateDir, "updates"), "client", clientAllowed},
	}
	if os.Geteuid() == 0 || serverRoot != "" {
		candidates = append(candidates, struct {
			root    string
			mode    string
			allowed []string
		}{filepath.Join(layout.DataDir, "updates"), "server", []string{filepath.Dir(executable), layout.ConfigDir, layout.SystemdDir}})
	}
	var selected *struct {
		root    string
		mode    string
		allowed []string
	}
	for index := range candidates {
		journal := filepath.Join(candidates[index].root, "current.json")
		info, err := os.Lstat(journal)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return migration.Manager{}, "", false, fmt.Errorf("unsafe update journal %s", journal)
		}
		if selected != nil {
			return migration.Manager{}, "", false, errors.New("ambiguous client and server update journals")
		}
		selected = &candidates[index]
	}
	if selected == nil {
		return migration.Manager{}, "", false, nil
	}
	return migration.Manager{Root: selected.root, AllowedRoots: selected.allowed, Registry: updateMigrationRegistry(), Retention: 1}, selected.mode, true, nil
}

func testServerRoot() string {
	if os.Getenv("IVOAI_TEST_MODE") == "1" {
		return os.Getenv("IVOAI_SERVER_ROOT")
	}
	return ""
}

// updateMigrationRegistry is the single ordered registry shipped by a target
// release. Schema 1 is intentionally a no-op for the v0.5.0 compatibility
// foundation; future schema changes add explicit reversible steps here.
func updateMigrationRegistry() migration.Registry {
	return migration.Registry{}
}

func currentSchemas() migration.Schemas {
	return migration.Schemas{
		migration.ArtifactConfig:     config.ConfigSchemaVersion,
		migration.ArtifactState:      config.StateSchemaVersion,
		migration.ArtifactOwnership:  config.OwnershipSchemaVersion,
		migration.ArtifactComponents: 1,
		migration.ArtifactServer:     1,
	}
}

func (a *App) managedExecutable() (string, error) {
	if a.ExecutablePath != "" {
		if os.Getenv("IVOAI_TEST_MODE") != "1" || !filepath.IsAbs(a.ExecutablePath) {
			return "", errors.New("test executable override is not allowed")
		}
		info, err := os.Lstat(a.ExecutablePath)
		if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return "", errors.New("test executable override must be a regular file")
		}
		return a.ExecutablePath, nil
	}
	executable, err := os.Executable()
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(executable)
}

func updateModeCommands(mode string) ([]string, []string) {
	if mode == "server" {
		return []string{"setup", "--mode", "server"}, []string{"server", "doctor"}
	}
	return []string{"setup"}, []string{"doctor", "--json"}
}

func normalizeVersion(value string) string { return strings.TrimPrefix(strings.TrimSpace(value), "v") }

func pathWithin(root, path string) bool {
	rel, err := filepath.Rel(filepath.Clean(root), filepath.Clean(path))
	return err == nil && rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func safeUpdateName(value string) string {
	var result strings.Builder
	for _, r := range value {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' {
			result.WriteRune(r)
		}
	}
	if result.Len() == 0 {
		return "managed"
	}
	return result.String()
}

func sortedSchemaArtifacts(values migration.Schemas) []migration.Artifact {
	result := make([]migration.Artifact, 0, len(values))
	for artifact := range values {
		result = append(result, artifact)
	}
	sort.Slice(result, func(i, j int) bool { return result[i] < result[j] })
	return result
}
