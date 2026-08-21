package webauth

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestOAuthPKCERotationAndGrantRevocation(t *testing.T) {
	const resource = "https://ivoai.example/mcp"
	store := NewStore(filepath.Join(t.TempDir(), "oauth", "state.json"))
	activation, err := store.CreateActivation(10*time.Minute, nil)
	if err != nil {
		t.Fatal(err)
	}
	client, err := store.RegisterClient("ChatGPT", []string{"https://example.test/callback"})
	if err != nil {
		t.Fatal(err)
	}
	verifier := "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-._~"
	nonce, err := store.BeginAuthorization(client.ID, client.RedirectURIs[0], PKCEChallenge(verifier), "opaque-state", resource, DefaultScopes)
	if err != nil {
		t.Fatal(err)
	}
	code, redirect, state, err := store.AuthorizeRequest(activation.Code, nonce)
	if err != nil {
		t.Fatal(err)
	}
	if redirect != client.RedirectURIs[0] || state != "opaque-state" {
		t.Fatalf("redirect/state mismatch: %q %q", redirect, state)
	}
	if _, _, _, err := store.AuthorizeRequest(activation.Code, nonce); err == nil {
		t.Fatal("CSRF request was reusable")
	}
	if _, err := store.ExchangeCode(code, client.ID, redirect, "wrong-verifier", resource); err == nil {
		t.Fatal("invalid verifier accepted")
	}
	// A failed exchange consumes the authorization code by design. Authorize a fresh grant.
	activation2, _ := store.CreateActivation(10*time.Minute, nil)
	nonce, _ = store.BeginAuthorization(client.ID, redirect, PKCEChallenge(verifier), "state-2", resource, DefaultScopes)
	code, _, _, _ = store.AuthorizeRequest(activation2.Code, nonce)
	tokens, err := store.ExchangeCode(code, client.ID, redirect, verifier, resource)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Authenticate(tokens.AccessToken, resource, ScopeContextRead); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Authenticate(tokens.AccessToken, "https://other.example/mcp"); err == nil {
		t.Fatal("access token accepted for wrong resource")
	}
	rotated, err := store.Refresh(tokens.RefreshToken, client.ID, resource)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Refresh(tokens.RefreshToken, client.ID, resource); err == nil {
		t.Fatal("rotated refresh token was reusable")
	}
	if err := store.RevokeActivation(activation2.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Authenticate(rotated.AccessToken, resource); err == nil {
		t.Fatal("grant access token survived revocation")
	}
	if _, err := store.Refresh(rotated.RefreshToken, client.ID, resource); err == nil {
		t.Fatal("grant refresh token survived revocation")
	}
	info, err := os.Stat(store.Path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("state mode=%o", info.Mode().Perm())
	}
}

func TestConcurrentActivationConsumptionIsOneTime(t *testing.T) {
	const resource = "https://ivoai.example/mcp"
	store := NewStore(filepath.Join(t.TempDir(), "state.json"))
	a, _ := store.CreateActivation(time.Minute, nil)
	c, _ := store.RegisterClient("Claude", []string{"https://example.test/cb"})
	verifier := "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-._~"
	const n = 12
	nonces := make([]string, n)
	for i := range nonces {
		nonces[i], _ = store.BeginAuthorization(c.ID, c.RedirectURIs[0], PKCEChallenge(verifier), "state", resource, DefaultScopes)
	}
	var wg sync.WaitGroup
	var mu sync.Mutex
	success := 0
	for _, nonce := range nonces {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, _, _, err := store.AuthorizeRequest(a.Code, nonce); err == nil {
				mu.Lock()
				success++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if success != 1 {
		t.Fatalf("successful consumptions=%d", success)
	}
}

func TestStateAndLockSymlinksRejected(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	if err := os.WriteFile(target, []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	statePath := filepath.Join(dir, "state.json")
	if err := os.Symlink(target, statePath); err != nil {
		t.Skip(err)
	}
	store := NewStore(statePath)
	if _, err := store.ListActivations(); err == nil {
		t.Fatal("state symlink accepted")
	}
	os.Remove(statePath)
	os.Remove(statePath + ".lock")
	if err := os.Symlink(target, statePath+".lock"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ListActivations(); err == nil {
		t.Fatal("lock symlink accepted")
	}
}

func TestExclusiveWritesPruneAbandonedActivationsAndClients(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	now := base
	store := NewStore(filepath.Join(t.TempDir(), "state.json"))
	store.Clock = func() time.Time { return now }
	oldActivation, err := store.CreateActivation(time.Minute, nil)
	if err != nil {
		t.Fatal(err)
	}
	oldClient, err := store.RegisterClient("abandoned", []string{"https://example.test/callback"})
	if err != nil {
		t.Fatal(err)
	}
	now = base.Add(91 * 24 * time.Hour)
	if _, err := store.CreateActivation(time.Minute, nil); err != nil {
		t.Fatal(err)
	}
	activations, err := store.ListActivations()
	if err != nil {
		t.Fatal(err)
	}
	for _, activation := range activations {
		if activation.ID == oldActivation.ID {
			t.Fatal("expired abandoned activation was not pruned")
		}
	}
	if _, err := store.Client(oldClient.ID, oldClient.RedirectURIs[0]); err == nil {
		t.Fatal("abandoned DCR client was not pruned")
	}
}

func TestStateRejectsTrailingJSONAndPrunesExpiredRequests(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	if err := os.WriteFile(path, []byte("{}\n{}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	store := NewStore(path)
	if _, err := store.ListActivations(); err == nil {
		t.Fatal("trailing JSON accepted")
	}
	os.Remove(path)
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	store.Clock = func() time.Time { return now }
	client, err := store.RegisterClient("client", []string{"https://example.test/cb"})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.with(true, func(st *state) error {
		for i := 0; i < 1024; i++ {
			key := fmt.Sprintf("%04d", i)
			st.Requests[key] = authorizationRequest{Hash: key, ExpiresAt: now.Add(-time.Second)}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	verifier := "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-._~"
	if _, err := store.BeginAuthorization(client.ID, client.RedirectURIs[0], PKCEChallenge(verifier), "state", "https://ivoai.example/mcp", DefaultScopes); err != nil {
		t.Fatalf("expired requests were not pruned: %v", err)
	}
}
