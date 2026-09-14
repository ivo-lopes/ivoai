package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/ivo-lopes/ivoai/internal/config"
	contextsvc "github.com/ivo-lopes/ivoai/internal/context"
	"github.com/ivo-lopes/ivoai/internal/enrollment"
	"github.com/ivo-lopes/ivoai/internal/project"
	"github.com/ivo-lopes/ivoai/internal/secrets"
	"github.com/ivo-lopes/ivoai/internal/session"
)

// Certifies the current local-client operating model, NOT distributed leases,
// human path ownership, shared provider accounts, or automatic project identity.
func TestTeamCurrentTwoClientIsolation(t *testing.T) {
	ctx, err := contextsvc.NewService(contextsvc.DeterministicEmbedder{DimensionsN: 8}, contextsvc.NewMemoryStore(), contextsvc.NewMemoryCatalog())
	if err != nil {
		t.Fatal(err)
	}
	if err := ctx.Initialize(context.Background()); err != nil {
		t.Fatal(err)
	}
	store := enrollment.NewStore(filepath.Join(t.TempDir(), "enrollment.json"))
	g, err := New(Config{ServerVersion: "team-fixture", Context: ctx, Enrollments: store, Memory: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p, ok := r.Context().Value(principalKey{}).(enrollment.Principal)
		if !ok || p.ClientID == "" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"purpose": "voicecorp", "knowledge": "shared fixture decision", "client_id": p.ClientID})
	})})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewTLSServer(g.Handler())
	defer server.Close()
	var paths [2]config.Paths
	var creds [2]enrollment.ClientCredential
	var providerPaths [2]string
	for i, name := range []string{"developer-a", "developer-b"} {
		home := filepath.Join(t.TempDir(), name)
		t.Setenv("HOME", home)
		for key, sub := range map[string]string{"XDG_CONFIG_HOME": "config", "XDG_DATA_HOME": "data", "XDG_STATE_HOME": "state", "XDG_CACHE_HOME": "cache"} {
			t.Setenv(key, filepath.Join(home, sub))
		}
		paths[i], err = config.ResolvePaths()
		if err != nil {
			t.Fatal(err)
		}
		code, err := store.Create(time.Minute, []enrollment.Scope{enrollment.ScopeMemoryRead, enrollment.ScopeContextRead})
		if err != nil {
			t.Fatal(err)
		}
		body, _ := json.Marshal(map[string]any{"code": code.Code, "client_name": name, "requested_scopes": []string{"memory:read", "context:read"}})
		response, err := server.Client().Post(server.URL+"/v1/enroll", "application/json", bytes.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		if response.StatusCode != http.StatusCreated {
			t.Fatalf("enrollment status=%d", response.StatusCode)
		}
		decode(t, response, &creds[i])
		if err := (secrets.Store{Path: paths[i].Secrets}).Save(secrets.Data{Servers: map[string]secrets.ClientCredential{"voicecorp": {ClientID: creds[i].ClientID, Token: creds[i].Token}}}); err != nil {
			t.Fatal(err)
		}
		// Synthetic provider state belongs to each developer; never copied from an operator.
		providerPaths[i] = filepath.Join(home, ".codex", "auth.json")
		if err := os.MkdirAll(filepath.Dir(providerPaths[i]), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(providerPaths[i], []byte("fixture-provider-"+name), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if creds[0].ClientID == creds[1].ClientID || creds[0].Token == creds[1].Token {
		t.Fatal("client credential crossover")
	}
	if paths[0].SessionsDir == paths[1].SessionsDir || paths[0].QuotaDir == paths[1].QuotaDir {
		t.Fatal("shared local state")
	}
	for i := range creds {
		data, err := (secrets.Store{Path: paths[i].Secrets}).Load()
		if err != nil {
			t.Fatal(err)
		}
		if len(data.Servers) != 1 || data.Servers["voicecorp"].Token != creds[i].Token {
			t.Fatal("credential ownership lost")
		}
		b, err := os.ReadFile(providerPaths[i])
		if err != nil {
			t.Fatal(err)
		}
		other, err := os.ReadFile(providerPaths[1-i])
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Equal(b, other) || bytes.Contains(b, []byte(creds[1-i].Token)) {
			t.Fatal("provider credential crossover")
		}
	}
	read := func(i, status int) {
		t.Helper()
		req, _ := http.NewRequest(http.MethodGet, server.URL+"/v1/memory/handoff", nil)
		req.Header.Set("Authorization", "Bearer "+creds[i].Token)
		resp, err := server.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != status {
			t.Fatalf("memory status=%d want=%d", resp.StatusCode, status)
		}
		if status == http.StatusOK {
			var result map[string]string
			if err := json.NewDecoder(io.LimitReader(resp.Body, 4096)).Decode(&result); err != nil {
				t.Fatal(err)
			}
			if result["purpose"] != "voicecorp" || result["knowledge"] != "shared fixture decision" || result["client_id"] != creds[i].ClientID {
				t.Fatal("shared knowledge or principal isolation failed")
			}
		}
	}
	read(0, http.StatusOK)
	read(1, http.StatusOK)
	if _, err := store.Authenticate(creds[0].Token, enrollment.ScopeMemoryWrite); err == nil {
		t.Fatal("read enrollment acquired write scope")
	}
	if err := store.RevokeClient(creds[0].ClientID); err != nil {
		t.Fatal(err)
	}
	read(0, http.StatusUnauthorized)
	read(1, http.StatusOK)
	id, err := session.NewID()
	if err != nil {
		t.Fatal(err)
	}
	a := session.Store{Root: paths[0].SessionsDir}
	b := session.Store{Root: paths[1].SessionsDir}
	lease, err := a.Acquire(id)
	if err != nil {
		t.Fatal(err)
	}
	defer lease.Close()
	if duplicate, err := a.Acquire(id); err == nil {
		duplicate.Close()
		t.Fatal("local duplicate lease allowed")
	}
	independent, err := b.Acquire(id)
	if err != nil {
		t.Fatal("separate client lease must remain local")
	}
	defer independent.Close()
}

func TestTeamCurrentProjectIdentityCrossCloneGap(t *testing.T) {
	var roots [2]string
	var markers [2]project.Marker
	for i := range roots {
		roots[i] = t.TempDir()
		for _, args := range [][]string{{"init", "-q"}, {"remote", "add", "origin", "https://example.invalid/team/repo.git"}} {
			cmd := exec.Command("git", append([]string{"-C", roots[i]}, args...)...)
			if err := cmd.Run(); err != nil {
				t.Fatal(err)
			}
		}
		var err error
		markers[i], err = project.Init(roots[i])
		if err != nil {
			t.Fatal(err)
		}
	}
	if markers[0].ID == markers[1].ID {
		t.Fatal("identity behavior changed: review documented cross-clone GAP")
	}
	// Existing reviewed project markers are portable through Git. No new registry.
	marker, err := os.ReadFile(filepath.Join(roots[0], project.MarkerName))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(roots[1], project.MarkerName), marker, 0600); err != nil {
		t.Fatal(err)
	}
	preserved, err := project.Init(roots[1])
	if err != nil {
		t.Fatal(err)
	}
	if preserved.ID != markers[0].ID || project.Identity(roots[1]) != markers[0].ID {
		t.Fatal("reviewed project marker not preserved")
	}
}
