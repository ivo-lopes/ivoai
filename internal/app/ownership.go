package app

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/ivo-lopes/ivoai/internal/config"
)

// RegisterInstall records the executable installed by install.sh so uninstall
// can distinguish it from an ivoai binary supplied by another package manager.
// It is intentionally an internal, argument-free command: callers cannot use it
// to claim arbitrary paths.
func (a *App) RegisterInstall() error {
	executable, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve installed executable: %w", err)
	}
	executable, err = filepath.EvalSymlinks(executable)
	if err != nil {
		return fmt.Errorf("resolve installed executable symlinks: %w", err)
	}
	executable, err = filepath.Abs(executable)
	if err != nil {
		return err
	}
	if filepath.Base(executable) != "ivoai" {
		return fmt.Errorf("refusing to register unexpected executable %s", executable)
	}
	info, err := os.Lstat(executable)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("refusing to register non-regular executable %s", executable)
	}
	ownership, err := a.Store.LoadOwnership()
	if err != nil {
		return err
	}
	previous := ownership.Components["ivoai"]
	item := config.OwnedItem{Managed: true, Path: executable, Launchers: append([]string(nil), previous.Launchers...)}
	if launcher := os.Getenv("IVOAI_MANAGED_LAUNCHER"); launcher != "" {
		launcher = filepath.Clean(launcher)
		if !filepath.IsAbs(launcher) {
			return fmt.Errorf("managed launcher must be an absolute path")
		}
		info, err := os.Lstat(launcher)
		if err != nil || info.Mode()&os.ModeSymlink == 0 {
			return fmt.Errorf("managed launcher must be an existing symlink")
		}
		resolved, err := filepath.EvalSymlinks(launcher)
		if err != nil {
			return fmt.Errorf("resolve managed launcher: %w", err)
		}
		resolved, err = filepath.Abs(resolved)
		if err != nil || resolved != executable {
			return fmt.Errorf("managed launcher does not resolve to the installed executable")
		}
		item.Launchers = []string{launcher}
	}
	ownership.Components["ivoai"] = item
	return a.Store.SaveOwnership(ownership)
}
