package enrollment

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func newTestStore(t *testing.T, now *time.Time) *Store {
	t.Helper()
	random := bytes.Repeat([]byte("0123456789abcdef"), 32)
	store := NewStore(filepath.Join(t.TempDir(), "secrets", "enrollment.json"))
	store.Clock = func() time.Time { return *now }
	store.Random = bytes.NewReader(random)
	return store
}

func TestEnrollmentIsOneTimeAndStoresHashesOnly(t *testing.T) {
	now := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	store := newTestStore(t, &now)
	created, err := store.Create(10*time.Minute, nil)
	if err != nil {
		t.Fatal(err)
	}
	credential, err := store.Consume(created.Code, "test-client")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Consume(created.Code, "second-client"); err == nil {
		t.Fatal("one-time code was reused")
	}
	principal, err := store.Authenticate(credential.Token, ScopeContextRead)
	if err != nil || principal.ClientID != credential.ClientID {
		t.Fatalf("authenticate = %#v, %v", principal, err)
	}
	contents, err := os.ReadFile(store.Path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(contents), created.Code) || strings.Contains(string(contents), credential.Token) {
		t.Fatal("plaintext credential persisted")
	}
	info, _ := os.Stat(store.Path)
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("state mode = %o", info.Mode().Perm())
	}
}

func TestEnrollmentExpiryRevocationAndScopes(t *testing.T) {
	now := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	store := newTestStore(t, &now)
	expired, _ := store.Create(time.Minute, []Scope{ScopeStatusRead})
	now = now.Add(2 * time.Minute)
	if _, err := store.Consume(expired.Code, "late"); err == nil {
		t.Fatal("expired code accepted")
	}
	active, _ := store.Create(time.Minute, []Scope{ScopeStatusRead})
	if err := store.Revoke(active.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Consume(active.Code, "revoked"); err == nil {
		t.Fatal("revoked code accepted")
	}
	allowed, _ := store.Create(time.Minute, []Scope{ScopeStatusRead})
	credential, _ := store.Consume(allowed.Code, "scoped")
	if _, err := store.Authenticate(credential.Token, ScopeContextRead); err == nil {
		t.Fatal("missing scope accepted")
	}
}

func TestEnrollmentRequestedScopesAreLeastPrivilege(t *testing.T) {
	now := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	store := newTestStore(t, &now)
	created, err := store.Create(time.Minute, []Scope{ScopeContextRead, ScopeStatusRead})
	if err != nil {
		t.Fatal(err)
	}
	credential, err := store.ConsumeScoped(created.Code, "least-privilege", []Scope{ScopeStatusRead})
	if err != nil {
		t.Fatal(err)
	}
	if len(credential.Scopes) != 1 || credential.Scopes[0] != ScopeStatusRead {
		t.Fatalf("unexpected granted scopes: %#v", credential.Scopes)
	}
	if _, err := store.Authenticate(credential.Token, ScopeContextRead); err == nil {
		t.Fatal("unrequested context scope was granted")
	}
}

func TestEnrollmentStateTransactionsAreInterprocessSafe(t *testing.T) {
	path := filepath.Join(t.TempDir(), "secrets", "enrollment.json")
	const workers = 32
	var group sync.WaitGroup
	errorsSeen := make(chan error, workers)
	for range workers {
		group.Add(1)
		go func() {
			defer group.Done()
			_, err := NewStore(path).Create(time.Minute, nil)
			errorsSeen <- err
		}()
	}
	group.Wait()
	close(errorsSeen)
	for err := range errorsSeen {
		if err != nil {
			t.Fatal(err)
		}
	}
	items, err := NewStore(path).List()
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != workers {
		t.Fatalf("concurrent transactions lost state: got %d, want %d", len(items), workers)
	}
	if info, err := os.Stat(path + ".lock"); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("lock permissions: info=%v err=%v", info, err)
	}
}

func TestStoreRejectsSymlinkStateAndLock(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	if err := os.WriteFile(target, []byte(`{"enrollments":{},"clients":{}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	statePath := filepath.Join(dir, "state.json")
	if err := os.Symlink(target, statePath); err != nil {
		t.Fatal(err)
	}
	if _, err := NewStore(statePath).List(); err == nil {
		t.Fatal("symlink enrollment state was accepted")
	}
	if err := os.Remove(statePath); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(statePath + ".lock"); err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	if err := os.Symlink(target, statePath+".lock"); err != nil {
		t.Fatal(err)
	}
	if _, err := NewStore(statePath).Create(time.Minute, nil); err == nil {
		t.Fatal("symlink enrollment lock was accepted")
	}
}

func TestStoreRejectsOversizedState(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate((16 << 20) + 1); err != nil {
		file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := NewStore(path).List(); err == nil {
		t.Fatal("oversized enrollment state was accepted")
	}
}

func TestRedactCredentialsAndHeaders(t *testing.T) {
	input := "Authorization: Bearer abc.def Cookie=session api_key=foo token=bar ivoai-client_deadbeef_secret"
	output := Redact(input)
	for _, secret := range []string{"abc.def", "session", "foo", "bar", "deadbeef"} {
		if strings.Contains(output, secret) {
			t.Fatalf("redaction leaked %q: %s", secret, output)
		}
	}
}
