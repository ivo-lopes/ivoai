// Package server manages server persistence, services, backup, and restore.
package server

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Layout separates configuration, secrets, authoritative data, cache/index,
// backups, runtime state, and installation assets.
type Layout struct {
	ConfigDir          string
	SecretsDir         string
	DataDir            string
	ContextDir         string
	CorpusDir          string
	MemoryDir          string
	QdrantDir          string
	QdrantSnapshotsDir string
	QdrantInitDir      string
	ModelsDir          string
	BackupDir          string
	RuntimeDir         string
	InstallDir         string
	SystemdDir         string
}

func DefaultLayout(root string) Layout {
	join := func(path string) string {
		if root == "" || root == "/" {
			return path
		}
		return filepath.Join(root, strings.TrimPrefix(path, "/"))
	}
	data := join("/var/lib/ivoai")
	return Layout{
		ConfigDir: join("/etc/ivoai"), SecretsDir: join("/etc/ivoai/secrets"), DataDir: data,
		ContextDir: filepath.Join(data, "context"), CorpusDir: filepath.Join(data, "corpus"), MemoryDir: filepath.Join(data, "memory"),
		QdrantDir: filepath.Join(data, "qdrant"), QdrantSnapshotsDir: filepath.Join(data, "qdrant-snapshots"), QdrantInitDir: filepath.Join(data, "qdrant-init"),
		ModelsDir: filepath.Join(data, "models"), BackupDir: filepath.Join(data, "backups"),
		RuntimeDir: join("/run/ivoai"), InstallDir: join("/opt/ivoai"), SystemdDir: join("/etc/systemd/system"),
	}
}

func (l Layout) Validate() error {
	dirs := []string{l.ConfigDir, l.SecretsDir, l.DataDir, l.ContextDir, l.CorpusDir, l.MemoryDir, l.QdrantDir, l.QdrantSnapshotsDir, l.QdrantInitDir, l.ModelsDir, l.BackupDir, l.RuntimeDir, l.InstallDir, l.SystemdDir}
	for _, dir := range dirs {
		if !filepath.IsAbs(dir) || filepath.Clean(dir) == "/" {
			return fmt.Errorf("unsafe server directory %q", dir)
		}
	}
	return nil
}

func (l Layout) Ensure() error {
	if err := l.Validate(); err != nil {
		return err
	}
	private := []string{l.ConfigDir, l.SecretsDir, filepath.Join(l.SecretsDir, "tls"), l.DataDir, l.ContextDir, l.CorpusDir, l.MemoryDir, l.QdrantDir, l.QdrantSnapshotsDir, l.QdrantInitDir, l.ModelsDir, l.BackupDir, l.RuntimeDir, l.InstallDir}
	for _, dir := range private {
		if info, err := os.Lstat(dir); err == nil && info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("refusing symlink server directory %s", dir)
		}
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return err
		}
		if err := os.Chmod(dir, 0o700); err != nil {
			return err
		}
	}
	if err := os.MkdirAll(l.SystemdDir, 0o755); err != nil {
		return err
	}
	return nil
}
