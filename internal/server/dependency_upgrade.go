package server

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ivo-lopes/ivoai/internal/platform"
	"golang.org/x/sys/unix"
)

var ErrDependencyUpgradeRequired = errors.New("existing dependency data requires an explicit server upgrade with stopped services, backup and copy validation; ordinary setup cannot change runtime/model pins")

// CheckDependencyUpgrade never creates/repairs the existing data or config tree.
func (m Manager) CheckDependencyUpgrade() error {
	populated := false
	for _, path := range []string{m.Layout.MemoryDir, m.Layout.QdrantDir} {
		info, err := os.Lstat(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return errors.New("unsafe existing dependency data root")
		}
		entries, err := os.ReadDir(path)
		if err != nil {
			return err
		}
		populated = populated || len(entries) > 0
	}
	if !populated {
		return nil
	}
	previous, err := readUpgradeFile(filepath.Join(m.Layout.ConfigDir, "compose.yaml"), 1<<20)
	if err != nil {
		return ErrDependencyUpgradeRequired
	}
	if pinnedDependencyIdentity(string(previous)) != pinnedDependencyIdentity(ComposeYAML) {
		return ErrDependencyUpgradeRequired
	}
	return nil
}

func pinnedDependencyIdentity(compose string) string {
	parts := []string{}
	for _, line := range strings.Split(compose, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "image:") || strings.Contains(trimmed, `"--revision"`) {
			parts = append(parts, trimmed)
		}
	}
	return strings.Join(parts, "\n")
}

func MemoryImageReference() string { return memoryImage(ComposeYAML) }
func QdrantImageReference() string {
	for _, line := range strings.Split(ComposeYAML, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "image: qdrant/qdrant@") {
			return strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "image:"))
		}
	}
	return ""
}

func memoryImage(compose string) string {
	inMemory := false
	for _, line := range strings.Split(compose, "\n") {
		if line == "  ai-memory:" {
			inMemory = true
			continue
		}
		if inMemory && strings.HasPrefix(line, "  ") && !strings.HasPrefix(line, "    ") {
			break
		}
		if inMemory && strings.HasPrefix(strings.TrimSpace(line), "image:") {
			return strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "image:"))
		}
	}
	return ""
}

type DependencyAsset struct {
	Present bool   `json:"present"`
	Content []byte `json:"content,omitempty"`
	Mode    uint32 `json:"mode"`
}
type DependencyUpgradeReceipt struct {
	SchemaVersion int                        `json:"schema_version"`
	CreatedAt     time.Time                  `json:"created_at"`
	Backup        string                     `json:"backup"`
	BackupSHA256  string                     `json:"backup_sha256"`
	IndexSnapshot string                     `json:"index_snapshot"`
	IndexSHA256   string                     `json:"index_sha256"`
	State         string                     `json:"state"`
	Assets        map[string]DependencyAsset `json:"assets"`
}

type DependencyValidator func(context.Context, Layout) error

func dependencyAssetPaths(l Layout) map[string]string {
	return map[string]string{
		"compose.yaml": filepath.Join(l.ConfigDir, "compose.yaml"), "compose.arm64.yaml": filepath.Join(l.ConfigDir, "compose.arm64.yaml"), "server.toml": filepath.Join(l.ConfigDir, "server.toml"),
		"dependencies.unit": filepath.Join(l.SystemdDir, "ivoai-dependencies.service"), "context.unit": filepath.Join(l.SystemdDir, "ivoai-context.service"), "gateway.unit": filepath.Join(l.SystemdDir, "ivoai-gateway.service"), "docs.unit": filepath.Join(l.SystemdDir, "ivoai-docs.service"),
	}
}

func requireQuiescedDependencies(ctx context.Context, m Manager) error {
	if m.Controller == nil {
		return errors.New("upgrade requires a service controller and observed stopped services")
	}
	states, err := m.Status(ctx)
	if err != nil {
		return err
	}
	if len(states) != len(ManagedServices) {
		return errors.New("upgrade requires complete service state")
	}
	expected := map[string]bool{}
	for _, name := range ManagedServices {
		expected[name] = true
	}
	for _, state := range states {
		if !expected[state.Name] || state.Active {
			return errors.New("upgrade requires all managed services stopped")
		}
		delete(expected, state.Name)
	}
	if len(expected) != 0 {
		return errors.New("upgrade requires complete stopped service state")
	}
	return nil
}

// UpgradeDependencies is only called inside the existing quiesced lifecycle.
// A copy restored through Core Backup/Restore must pass the fixed host validator
// before any new runtime asset is installed. Original data is never probed by
// the new ai-memory image here. A failed start is recovered explicitly via the
// returned receipt, which binds old runtime assets to the exact data backup.
func (m Manager) UpgradeDependencies(ctx context.Context, backup string, validate DependencyValidator) (string, error) {
	var receipt string
	err := WithDependencyMutationLock(ctx, m.Layout, func(locked context.Context) error {
		var err error
		receipt, err = m.upgradeDependencies(locked, backup, validate)
		return err
	})
	return receipt, err
}

func (m Manager) upgradeDependencies(ctx context.Context, backup string, validate DependencyValidator) (string, error) {
	if err := requireQuiescedDependencies(ctx, m); err != nil {
		return "", err
	}
	if validate == nil {
		return "", errors.New("dependency copy validator is required")
	}
	if !filepath.IsAbs(backup) || filepath.Dir(filepath.Clean(backup)) != filepath.Clean(m.Layout.BackupDir) {
		return "", errors.New("upgrade backup must be a new file directly inside the managed backup directory")
	}
	for _, path := range []string{backup, backup + ".upgrade.json"} {
		if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
			return "", errors.New("upgrade never overwrites an existing backup or receipt")
		}
	}
	receipt := DependencyUpgradeReceipt{SchemaVersion: 1, CreatedAt: time.Now().UTC(), Backup: backup, State: "prepared", Assets: map[string]DependencyAsset{}}
	for name, path := range dependencyAssetPaths(m.Layout) {
		body, err := readUpgradeFile(path, 1<<20)
		if errors.Is(err, os.ErrNotExist) {
			receipt.Assets[name] = DependencyAsset{}
			continue
		}
		if err != nil {
			return "", err
		}
		// Managed defaults reference private env files. Inline credential material
		// must be migrated by the operator instead of copied into an output receipt.
		inspected := strings.ReplaceAll(string(body), "Authorization: Bearer '$$API_KEY", "environment-referenced-auth-header")
		if platform.Redact(inspected) != inspected || strings.Contains(string(body), "PRIVATE KEY-----") {
			return "", errors.New("managed dependency asset contains inline private material; operator migration required")
		}
		mode := uint32(0600)
		if strings.HasSuffix(name, ".unit") {
			mode = 0644
		}
		receipt.Assets[name] = DependencyAsset{Present: true, Content: body, Mode: mode}
	}
	if !receipt.Assets["compose.yaml"].Present {
		return "", errors.New("existing managed dependency assets are required before upgrade")
	}
	if err := Backup(m.Layout, backup, receipt.CreatedAt); err != nil {
		return "", err
	}
	digest, err := hashUpgradeFile(backup)
	if err != nil {
		return "", err
	}
	receipt.BackupSHA256 = digest
	receipt.IndexSnapshot = backup + ".qdrant"
	receipt.IndexSHA256, err = copyDependencyIndex(ctx, m.Layout.QdrantDir, receipt.IndexSnapshot)
	if err != nil {
		return "", err
	}
	stage, err := os.MkdirTemp(m.Layout.BackupDir, ".dependency-validation-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(stage)
	copyLayout := DefaultLayout(stage)
	if err := Restore(copyLayout, backup); err != nil {
		return "", fmt.Errorf("restore upgrade validation copy: %w", err)
	}
	if err := platform.AtomicWritePrivate(receipt.Assets["compose.yaml"].Content, filepath.Join(copyLayout.ConfigDir, ".dependency-baseline-compose")); err != nil {
		return "", err
	}
	if err := os.Remove(copyLayout.QdrantDir); err != nil {
		return "", err
	}
	if _, err := copyDependencyIndex(ctx, receipt.IndexSnapshot, copyLayout.QdrantDir); err != nil {
		return "", err
	}
	if err := validate(ctx, copyLayout); err != nil {
		return "", fmt.Errorf("new runtime failed isolated-copy validation; original runtime/data preserved: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	path := backup + ".upgrade.json"
	if err := writeDependencyReceipt(path, receipt); err != nil {
		return "", err
	}
	if err := m.setup(ctx); err != nil {
		return path, err
	}
	receipt.State = "assets_installed"
	if err := writeDependencyReceipt(path, receipt); err != nil {
		return path, err
	}
	return path, nil
}

func (m Manager) RollbackDependencies(ctx context.Context, path string) error {
	return WithDependencyMutationLock(ctx, m.Layout, func(locked context.Context) error { return m.rollbackDependencies(locked, path) })
}

func (m Manager) rollbackDependencies(ctx context.Context, path string) error {
	if err := requireQuiescedDependencies(ctx, m); err != nil {
		return err
	}
	if filepath.Dir(filepath.Clean(path)) != filepath.Clean(m.Layout.BackupDir) || !strings.HasSuffix(path, ".upgrade.json") {
		return errors.New("rollback receipt must belong to the managed backup directory")
	}
	body, err := readUpgradeFile(path, 8<<20, true)
	if err != nil {
		return err
	}
	var receipt DependencyUpgradeReceipt
	decoder := json.NewDecoder(strings.NewReader(string(body)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&receipt); err != nil {
		return errors.New("invalid upgrade receipt")
	}
	if receipt.SchemaVersion != 1 || (receipt.State != "prepared" && receipt.State != "assets_installed") || receipt.Backup+".upgrade.json" != path || len(receipt.Assets) != len(dependencyAssetPaths(m.Layout)) {
		return errors.New("upgrade receipt identity invalid")
	}
	digest, err := hashUpgradeFile(receipt.Backup)
	if err != nil || digest != receipt.BackupSHA256 {
		return errors.New("upgrade backup integrity failed")
	}
	if receipt.IndexSnapshot != receipt.Backup+".qdrant" || receipt.IndexSHA256 == "" {
		return errors.New("upgrade index snapshot identity invalid")
	}
	prepared, err := os.MkdirTemp(m.Layout.DataDir, ".qdrant-rollback-")
	if err != nil {
		return err
	}
	if err := os.Remove(prepared); err != nil {
		return err
	}
	defer os.RemoveAll(prepared)
	indexDigest, err := copyDependencyIndex(ctx, receipt.IndexSnapshot, prepared)
	if err != nil || indexDigest != receipt.IndexSHA256 {
		return errors.New("upgrade index snapshot integrity failed")
	}
	paths := dependencyAssetPaths(m.Layout)
	for name, asset := range receipt.Assets {
		if paths[name] == "" || len(asset.Content) > 1<<20 || (asset.Present && asset.Mode != 0600 && asset.Mode != 0644) {
			return errors.New("invalid operational asset in upgrade receipt")
		}
	}
	if err := Restore(m.Layout, receipt.Backup); err != nil {
		return err
	}
	quarantine, err := os.MkdirTemp(m.Layout.BackupDir, "post-upgrade-index-")
	if err != nil {
		return err
	}
	if err := os.Remove(quarantine); err != nil {
		return err
	}
	if err := os.Rename(m.Layout.QdrantDir, quarantine); err != nil {
		return fmt.Errorf("index rollback could not preserve current index; services must remain stopped: %w", err)
	}
	if err := os.Rename(prepared, m.Layout.QdrantDir); err != nil {
		return err
	}
	for name, asset := range receipt.Assets {
		if asset.Present {
			if err := writeManagedFile(paths[name], asset.Content, os.FileMode(asset.Mode)); err != nil {
				return err
			}
		} else {
			if _, err := readUpgradeFile(paths[name], 1<<20); err == nil {
				if err := os.Remove(paths[name]); err != nil {
					return err
				}
			} else if !errors.Is(err, os.ErrNotExist) {
				return err
			}
		}
	}
	return m.Controller.DaemonReload(ctx)
}

func writeDependencyReceipt(path string, value DependencyUpgradeReceipt) error {
	body, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return platform.AtomicWritePrivate(body, path)
}

func readUpgradeFile(path string, max int64, private ...bool) ([]byte, error) {
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	f := os.NewFile(uintptr(fd), path)
	defer f.Close()
	var stat unix.Stat_t
	if unix.Fstat(fd, &stat) != nil || stat.Mode&unix.S_IFMT != unix.S_IFREG || stat.Nlink != 1 || int(stat.Uid) != os.Geteuid() || stat.Mode&0022 != 0 || stat.Size > max || (len(private) > 0 && private[0] && stat.Mode&0077 != 0) {
		return nil, errors.New("unsafe dependency upgrade file")
	}
	return io.ReadAll(io.LimitReader(f, max+1))
}
func hashUpgradeFile(path string) (string, error) {
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	if err != nil {
		return "", err
	}
	f := os.NewFile(uintptr(fd), path)
	defer f.Close()
	var stat unix.Stat_t
	if unix.Fstat(fd, &stat) != nil || stat.Mode&unix.S_IFMT != unix.S_IFREG || stat.Nlink != 1 || int(stat.Uid) != os.Geteuid() || stat.Mode&0077 != 0 {
		return "", errors.New("unsafe dependency backup")
	}
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
