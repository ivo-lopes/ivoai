package codexfrontend

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/ivo-lopes/ivoai/internal/platform"
	"github.com/pelletier/go-toml/v2"
)

// NativeState identifies provider-owned history, never an IVOAI transcript DB.
// Configuration and authentication remain outside the managed process home.
type NativeState struct {
	Home       string `json:"-"`
	SQLiteHome string `json:"-"`
}

func (s NativeState) ScopeID() string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte(s.Home+"\x00"+s.SQLiteHome)))
}

// ResolveNativeState reads only the supported state-directory setting. No
// authentication file is read, no provider configuration is changed, and parse
// errors are not returned verbatim (TOML diagnostics can contain secret values).
func ResolveNativeState(environment []string, cwd string) (NativeState, error) {
	values := map[string]string{}
	for _, entry := range environment {
		key, value, ok := strings.Cut(entry, "=")
		if ok && (key == "HOME" || key == "CODEX_HOME" || key == "CODEX_SQLITE_HOME") {
			values[key] = value
		}
	}
	home := values["CODEX_HOME"]
	if home == "" && filepath.IsAbs(values["HOME"]) {
		home = filepath.Join(values["HOME"], ".codex")
	}
	if !filepath.IsAbs(home) || !filepath.IsAbs(cwd) {
		return NativeState{}, errors.New("NATIVE_STATE_DIRECTORY_INVALID")
	}
	var config struct {
		SQLiteHome string `toml:"sqlite_home"`
	}
	body, err := platform.ReadRegularFile(filepath.Join(home, "config.toml"), 1<<20)
	if err != nil && !os.IsNotExist(err) {
		return NativeState{}, errors.New("NATIVE_STATE_CONFIG_UNAVAILABLE")
	}
	if err == nil && toml.Unmarshal(body, &config) != nil {
		return NativeState{}, errors.New("NATIVE_STATE_CONFIG_INVALID")
	}
	state := config.SQLiteHome
	if state != "" && !filepath.IsAbs(state) {
		state = filepath.Join(home, state)
	}
	if state == "" {
		state = values["CODEX_SQLITE_HOME"]
	}
	if state == "" {
		state = home
	}
	if !filepath.IsAbs(state) {
		state = filepath.Join(cwd, state)
	}
	return NativeState{Home: filepath.Clean(home), SQLiteHome: filepath.Clean(state)}, nil
}

// PrepareView creates only exact links to provider conversation directories.
// Sharing SQLite without the native writer locks would permit two writers on
// the same thread. Never link config, auth, hooks, plugins, or the whole home.
// The view must be newly allocated: pre-existing content is never replaced.
func (s NativeState) PrepareView(view string) error {
	if !filepath.IsAbs(view) || !filepath.IsAbs(s.Home) || !filepath.IsAbs(s.SQLiteHome) || filepath.Clean(view) == filepath.Clean(s.Home) {
		return errors.New("NATIVE_STATE_VIEW_INVALID")
	}
	if err := ownedStateDirectory(view, false); err != nil {
		return err
	}
	viewInfo, err := os.Lstat(view)
	if err != nil || viewInfo.Mode().Perm()&0077 != 0 {
		return errors.New("NATIVE_STATE_VIEW_UNSAFE")
	}
	entries, err := os.ReadDir(view)
	if err != nil {
		return errors.New("NATIVE_STATE_VIEW_NOT_EMPTY")
	}
	if len(entries) != 0 {
		marker, err := platform.ReadRegularFile(filepath.Join(view, ".ivoai-native-state"), 128)
		if err != nil || string(marker) != s.ScopeID() {
			return errors.New("NATIVE_STATE_VIEW_NOT_EMPTY")
		}
		for _, name := range []string{"config.toml", "auth.json", "hooks.json"} {
			if _, err := os.Lstat(filepath.Join(view, name)); !os.IsNotExist(err) {
				return errors.New("NATIVE_STATE_VIEW_CONTAMINATED")
			}
		}
		for _, name := range []string{"sessions", "archived_sessions", "thread-writer-locks"} {
			target, err := os.Readlink(filepath.Join(view, name))
			if err != nil || target != filepath.Join(s.Home, name) {
				return errors.New("NATIVE_STATE_VIEW_INVALID")
			}
		}
	}
	for _, root := range []string{s.Home, s.SQLiteHome} {
		if err := ownedStateDirectory(root, false); err != nil {
			return err
		}
	}
	homeInfo, err := os.Stat(s.Home)
	if err != nil {
		return errors.New("NATIVE_STATE_DIRECTORY_UNAVAILABLE")
	}
	privateHome := homeInfo.Mode().Perm()&0077 == 0
	for _, name := range []string{"sessions", "archived_sessions", "thread-writer-locks"} {
		target := filepath.Join(s.Home, name)
		if err := ownedStateDirectory(target, privateHome); err != nil {
			return err
		}
	}
	for _, name := range []string{"sessions", "archived_sessions", "thread-writer-locks"} {
		target := filepath.Join(s.Home, name)
		if len(entries) == 0 {
			if err := os.Symlink(target, filepath.Join(view, name)); err != nil {
				return errors.New("NATIVE_STATE_VIEW_FAILED")
			}
		}
	}
	if len(entries) != 0 {
		return nil
	}
	return platform.AtomicWritePrivate([]byte(s.ScopeID()), filepath.Join(view, ".ivoai-native-state"))
}

func ownedStateDirectory(path string, privateAncestor bool) error {
	if err := os.MkdirAll(path, 0700); err != nil {
		return errors.New("NATIVE_STATE_DIRECTORY_UNAVAILABLE")
	}
	info, err := os.Lstat(path)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || (!privateAncestor && info.Mode().Perm()&0022 != 0) {
		return errors.New("NATIVE_STATE_DIRECTORY_UNSAFE")
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || int(stat.Uid) != os.Getuid() {
		return errors.New("NATIVE_STATE_OWNER_MISMATCH")
	}
	return nil
}
