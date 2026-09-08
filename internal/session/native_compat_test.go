package session

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ivo-lopes/ivoai/internal/quota"
)

func TestNativeSessionMigrationPreservesHistoryAndRejectsSymlinks(t *testing.T) {
	store, value := fixtureSession(t, filepath.Join(t.TempDir(), "sessions"))
	if err := store.Create(value); err != nil {
		t.Fatal(err)
	}
	value.RequestedExecutor, value.EffectiveExecutor = "opencode", "opencode"
	body, _ := json.Marshal(value)
	legacy := filepath.Join(store.Root, value.SessionID+".json")
	if err := os.WriteFile(legacy, body, 0600); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := store.ReconcileNativeMetadata(); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := os.Stat(legacy); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("unsupported record still visible to legacy binary")
	}
	current, err := store.Get(value.SessionID)
	if err != nil || !current.UpdatedAt.Equal(value.UpdatedAt) || current.RequestedExecutor != "opencode" {
		t.Fatal("migration lost native metadata", err)
	}
	if err := store.Create(value); err == nil {
		t.Fatal("duplicate native session accepted")
	}
	if _, err := store.Update(value.SessionID, func(s *Session) error { s.State = StateRunning; return nil }); err != nil {
		t.Fatal(err)
	}
	if err := store.Delete(value.SessionID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Get(value.SessionID); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("native delete failed", err)
	}
	unsafe := Store{Root: t.TempDir()}
	if err := os.Symlink(store.nativeDir(), unsafe.nativeDir()); err != nil {
		t.Fatal(err)
	}
	if _, err := unsafe.List(); err == nil {
		t.Fatal("native namespace symlink accepted")
	}
}

func TestNativeRuntimeStateReadableAfterV092RollbackAndReapply(t *testing.T) {
	if err := exec.Command("git", "cat-file", "-e", "v0.9.2^{commit}").Run(); err != nil {
		if os.Getenv("CI") != "" {
			t.Fatal("rollback baseline tag required", err)
		}
		t.Skip("requires canonical v0.9.2 tag")
	}
	root := t.TempDir()
	store, legacy := fixtureSession(t, filepath.Join(root, "sessions"))
	legacy.Mode, legacy.Auto, legacy.InitialPlanner, legacy.CurrentPrimary = ModeAuto, true, "codex", "codex"
	legacy.Quota = map[quota.Provider]quota.ProviderQuota{quota.ProviderOpenCode: {Provider: quota.ProviderOpenCode, Authenticated: true, TelemetryUnknown: true}}
	if err := store.Create(legacy); err != nil {
		t.Fatal(err)
	}
	native := legacy
	native.SessionID, _ = NewID()
	native.PrimaryExecutor, native.InitialPlanner, native.CurrentPrimary = "opencode", "opencode", "opencode"
	native.RequestedExecutor, native.EffectiveExecutor = "opencode", "opencode"
	if err := store.Create(native); err != nil {
		t.Fatal(err)
	}
	quotas := quota.Store{Root: filepath.Join(root, "quota")}
	if err := quotas.Put(quota.ProviderQuota{Provider: quota.ProviderOpenCode, Authenticated: true, TelemetryUnknown: true}); err != nil {
		t.Fatal(err)
	}
	if err := quotas.Put(quota.ProviderQuota{Provider: quota.ProviderCodex, Authenticated: true}); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(root, "v092")
	if err := os.MkdirAll(filepath.Join(source, "cmd", "nativecompat"), 0700); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	archive := exec.CommandContext(ctx, "git", "archive", "v0.9.2")
	repo, err := exec.Command("git", "rev-parse", "--show-toplevel").Output()
	if err != nil {
		t.Fatal(err)
	}
	archive.Dir = strings.TrimSpace(string(repo))
	pipe, err := archive.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	unpack := exec.CommandContext(ctx, "tar", "-x", "-C", source)
	unpack.Stdin = pipe
	if err := archive.Start(); err != nil {
		t.Fatal(err)
	}
	if err := unpack.Run(); err != nil {
		t.Fatal(err)
	}
	if err := archive.Wait(); err != nil {
		t.Fatal(err)
	}
	helper := `package main
import("fmt";"os";"path/filepath";"github.com/ivo-lopes/ivoai/internal/quota";"github.com/ivo-lopes/ivoai/internal/session")
func main(){
 root:=os.Args[1]
 q:=quota.Store{Root:filepath.Join(root,"quota")}
 if _,err:=q.Load();err!=nil {panic(err)}
 if err:=q.Put(quota.ProviderQuota{Provider:quota.ProviderCodex,Authenticated:true});err!=nil {panic(err)}
 s,err:=(session.Store{Root:filepath.Join(root,"sessions")}).List()
 if err!=nil {panic(err)}
 if len(s)!=1||s[0].PrimaryExecutor!="codex" {panic("unsupported native records leaked into legacy history")}
 fmt.Println("V092_RUNTIME_READ_WRITE=PASS")
}
`
	if err := os.WriteFile(filepath.Join(source, "cmd", "nativecompat", "main.go"), []byte(helper), 0600); err != nil {
		t.Fatal(err)
	}
	command := exec.CommandContext(ctx, "go", "run", "./cmd/nativecompat", root)
	command.Dir = source
	output, err := command.CombinedOutput()
	if err != nil || !strings.Contains(string(output), "V092_RUNTIME_READ_WRITE=PASS") {
		t.Fatalf("v0.9.2 rejected candidate runtime state: %s; %v", output, err)
	}
	values, err := store.List()
	if err != nil || len(values) != 2 {
		t.Fatal("reapply lost native session history", err)
	}
	value, err := store.Get(native.SessionID)
	if err != nil || value.CurrentPrimary != "opencode" || value.Quota[quota.ProviderOpenCode].Provider != quota.ProviderOpenCode {
		t.Fatal("native metadata lost on reapply", err)
	}
	t.Log("V092_ROLLBACK_RUNTIME=PASS; REAPPLY_NATIVE_HISTORY=PASS")
}
