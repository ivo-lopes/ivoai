package session

import (
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestNativeAdoptionConfirmationAndConcurrentIdentity(t *testing.T) {
	store := Store{Root: filepath.Join(t.TempDir(), "sessions")}
	thread, _ := NewNativeUUID()
	cwd := t.TempDir()
	scope := strings.Repeat("a", 64)
	if _, err := store.AdoptCodex(thread, cwd, scope, false); err == nil {
		t.Fatal("adoption without confirmation")
	}
	var wg sync.WaitGroup
	ids := make(chan string, 8)
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			v, err := store.AdoptCodex(thread, cwd, scope, true)
			if err != nil {
				t.Error(err)
				return
			}
			ids <- v.SessionID
		}()
	}
	wg.Wait()
	close(ids)
	var identity string
	for id := range ids {
		if identity != "" && id != identity {
			t.Fatal("duplicate logical mappings")
		}
		identity = id
	}
	values, err := store.List()
	if err != nil || len(values) != 1 || values[0].FrontendSessionID != thread || values[0].NativeAdoption.ThreadID != thread {
		t.Fatal("native identity lost")
	}
	if _, err := store.AdoptCodex(thread, t.TempDir(), scope, true); err == nil {
		t.Fatal("different project silently adopted")
	}
}

func TestNativeAdoptionPreservesExistingDirectMode(t *testing.T) {
	store, v := fixtureSession(t, filepath.Join(t.TempDir(), "sessions"))
	thread, _ := NewNativeUUID()
	v.ExecutorSessionID = thread
	if err := store.Create(v); err != nil {
		t.Fatal(err)
	}
	got, err := store.AdoptCodex(thread, v.WorkingDirectory, strings.Repeat("a", 64), true)
	if err != nil || got.SessionID != v.SessionID || got.Mode != ModeDirect {
		t.Fatalf("direct mode or identity changed: %v", err)
	}
}

func TestPortabilityClasses(t *testing.T) {
	for _, c := range []struct {
		a, b, f, g string
		managed    bool
		want       PortabilityClass
	}{
		{"codex", "codex", "codex", "codex", true, SameNative},
		{"codex", "codex", "codex", "opencode", true, SameIVOAI},
		{"codex", "codex", "opencode", "codex", true, SameIVOAI},
		{"opencode", "codex", "opencode", "opencode", true, ExplicitHandoff},
		{"codex", "opencode", "opencode", "opencode", true, ExplicitHandoff},
		{"codex", "codex", "codex", "codex", false, ExplicitAdoption},
	} {
		if got := ClassifyPortability(c.a, c.b, c.f, c.g, c.managed); got != c.want {
			t.Fatalf("got %s want %s", got, c.want)
		}
	}
}

func TestNativeAdoptionModeSwitchPreservesConversation(t *testing.T) {
	store := Store{Root: filepath.Join(t.TempDir(), "sessions")}
	thread, _ := NewNativeUUID()
	scope := strings.Repeat("a", 64)
	v, err := store.AdoptCodex(thread, t.TempDir(), scope, true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.SwitchCodexMode(v.SessionID, thread, scope, ModeDirect, false); err == nil {
		t.Fatal("missing confirmation accepted")
	}
	foreign, _ := NewNativeUUID()
	if _, err := store.SwitchCodexMode(v.SessionID, foreign, scope, ModeDirect, true); err == nil {
		t.Fatal("foreign thread attached")
	}
	if _, err := store.SwitchCodexMode(v.SessionID, thread, strings.Repeat("b", 64), ModeDirect, true); err == nil {
		t.Fatal("foreign provider store attached")
	}
	for _, mode := range []Mode{ModeDirect, ModeAuto, ModeDirect} {
		got, err := store.SwitchCodexMode(v.SessionID, thread, scope, mode, true)
		if err != nil {
			t.Fatal(err)
		}
		if got.SessionID != v.SessionID || got.FrontendSessionID != thread || got.NativeAdoption.ThreadID != thread || got.Mode != mode || got.Auto != (mode == ModeAuto) {
			t.Fatal("identity or admission changed unexpectedly")
		}
	}
	lease, err := store.Acquire(v.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	defer lease.Close()
	if _, err := store.SwitchCodexMode(v.SessionID, thread, scope, ModeAuto, true); err == nil {
		t.Fatal("active session switched")
	}
}
