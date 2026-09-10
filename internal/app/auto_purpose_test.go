package app

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/ivo-lopes/ivoai/internal/config"
	"github.com/ivo-lopes/ivoai/internal/secrets"
)

func TestAutoPurposeRoutingDoesNotQueryIrrelevantInstitution(t *testing.T) {
	root := t.TempDir()
	for _, key := range []string{"XDG_CONFIG_HOME", "XDG_DATA_HOME", "XDG_STATE_HOME", "XDG_CACHE_HOME"} {
		t.Setenv(key, filepath.Join(root, key))
	}
	a, err := New("fixture", strings.NewReader(""), io.Discard, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	counts := map[string]*atomic.Int32{}
	for _, purpose := range []string{"company-a", "company-b"} {
		counter := &atomic.Int32{}
		counts[purpose] = counter
		upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			counter.Add(1)
			if r.Header.Get("Authorization") != "Bearer fixture-"+purpose {
				t.Error("institutional credential crossover")
			}
			w.Header().Set("Content-Type", "application/json")
			io.WriteString(w, `{"jsonrpc":"2.0","id":1,"result":{"tools":[]}}`)
		}))
		t.Cleanup(upstream.Close)
		p := config.ServerProfile{ID: "srv_" + strings.ReplaceAll(purpose, "-", "_"), Alias: purpose, Purpose: purpose, URL: upstream.URL, ContextMCPURL: upstream.URL + "/context", Status: "connected", Enabled: true}
		cfg.Connections.Servers[purpose] = p
		if err := (secrets.Store{Path: a.Store.Paths.Secrets}).Set(p.ID, secrets.ClientCredential{Token: "fixture-" + purpose}); err != nil {
			t.Fatal(err)
		}
	}
	for _, tc := range []struct {
		prompt   string
		explicit []string
		expected string
	}{
		{"Analyze company-a configuration. Acceptance: return a findings report without edits.", nil, "company-a"},
		{"Analyze company-a configuration. Acceptance: return a findings report without edits.", []string{"company-b"}, "company-b"},
		{"Read VERSION. Acceptance: return only the version value.", nil, ""},
	} {
		for _, count := range counts {
			count.Store(0)
		}
		k, err := a.prepareAutoPromptKnowledge(context.Background(), cfg, tc.explicit, tc.prompt, "codex", t.TempDir(), nil)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Join(k.aliases(), ",") != tc.expected {
			k.close()
			t.Fatal("incorrect purpose selection")
		}
		if k.router != nil {
			request, _ := http.NewRequest("POST", k.router.BaseURL()+"/mcp/context", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}`))
			request.Header.Set("Authorization", "Bearer "+k.router.Token())
			request.Header.Set("Content-Type", "application/json")
			request.Header.Set("Accept", "application/json, text/event-stream")
			response, err := http.DefaultClient.Do(request)
			if err != nil {
				k.close()
				t.Fatal(err)
			}
			io.Copy(io.Discard, response.Body)
			response.Body.Close()
			if response.StatusCode != http.StatusOK {
				k.close()
				t.Fatalf("router response=%d", response.StatusCode)
			}
		}
		k.close()
		for purpose, count := range counts {
			if purpose == tc.expected && count.Load() == 0 {
				t.Fatal("selected source not queried")
			}
			if purpose != tc.expected && count.Load() != 0 {
				t.Fatal("irrelevant source queried")
			}
		}
	}
}
