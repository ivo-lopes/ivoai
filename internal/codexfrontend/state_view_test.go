package codexfrontend

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNativeStateViewDoesNotExposePersonalConfiguration(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "provider")
	if err := os.Mkdir(home, 0700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"auth.json", "config.toml", "hooks.json"} {
		if err := os.WriteFile(filepath.Join(home, name), []byte("personal-canary"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	view, err := os.MkdirTemp(t.TempDir(), "private-view-")
	if err != nil {
		t.Fatal(err)
	}
	state := NativeState{Home: home, SQLiteHome: home}
	if err := state.PrepareView(view); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(view)
	if err != nil || len(entries) != 4 {
		t.Fatal("unexpected native view entries")
	}
	for _, name := range []string{"sessions", "archived_sessions", "thread-writer-locks"} {
		target, err := os.Readlink(filepath.Join(view, name))
		if err != nil || target != filepath.Join(home, name) {
			t.Fatal("conversation state or native writer locks not shared")
		}
	}
	for _, name := range []string{"auth.json", "config.toml", "hooks.json"} {
		if _, err := os.Lstat(filepath.Join(view, name)); !os.IsNotExist(err) {
			t.Fatal("personal config exposed")
		}
		body, err := os.ReadFile(filepath.Join(home, name))
		if err != nil || string(body) != "personal-canary" {
			t.Fatal("personal config mutated")
		}
	}
	if err := state.PrepareView(view); err != nil {
		t.Fatal("verified state view cannot reopen")
	}
}

func TestResolveNativeStateConfigPrecedence(t *testing.T) {
	root := t.TempDir()
	config := filepath.Join(root, "config.toml")
	if err := os.WriteFile(config, []byte("sqlite_home = 'configured-state'\n"), 0600); err != nil {
		t.Fatal(err)
	}
	state, err := ResolveNativeState([]string{"HOME=" + root, "CODEX_HOME=" + root, "CODEX_SQLITE_HOME=/unused", "OPENAI_API_KEY=not-read"}, root)
	if err != nil || state.SQLiteHome != filepath.Join(root, "configured-state") {
		t.Fatal("official config precedence not respected")
	}
	if err := os.WriteFile(config, []byte("[ invalid secret-config-canary"), 0600); err != nil {
		t.Fatal(err)
	}
	_, err = ResolveNativeState([]string{"CODEX_HOME=" + root}, root)
	if err == nil || err.Error() != "NATIVE_STATE_CONFIG_INVALID" {
		t.Fatal("unsafe config diagnostic")
	}
}

func TestNativeStateRejectsSymlinkDirectory(t *testing.T) {
	home := t.TempDir()
	if err := os.Symlink(t.TempDir(), filepath.Join(home, "thread-writer-locks")); err != nil {
		t.Fatal(err)
	}
	if err := (NativeState{Home: home, SQLiteHome: home}).PrepareView(t.TempDir()); err == nil {
		t.Fatal("foreign writer-lock target accepted")
	}
}
