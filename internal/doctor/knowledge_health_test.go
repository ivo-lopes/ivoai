package doctor

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ivo-lopes/ivoai/internal/config"
	"github.com/ivo-lopes/ivoai/internal/secrets"
)

func TestKnowledgeHealthSeparatesReadsHooksAndEmptyContext(t *testing.T) {
	for _, scenario := range []string{"zero", "single", "two_healthy", "auth_error", "protocol_error", "upstream_error", "transport_error"} {
		t.Run(scenario, func(t *testing.T) {
			cfg := config.Default()
			root := t.TempDir()
			store := config.NewStore(config.Paths{Secrets: filepath.Join(root, "secrets.json")})
			count := 2
			if scenario == "zero" {
				count = 0
			}
			if scenario == "single" {
				count = 1
			}
			for i := 0; i < count; i++ {
				bad := i == 1 && scenario != "two_healthy"
				alias := []string{"alpha", "beta"}[i]
				token := "fixture-private-" + alias
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					switch r.URL.Path {
					case "/.well-known/ivoai":
						json.NewEncoder(w).Encode(map[string]any{"protocol_version": 1, "health_endpoint": "/health", "ready_endpoint": "/ready", "context_mcp_endpoint": "/context", "memory_mcp_endpoint": "/memory", "features": map[string]bool{"memory": true, "context": true}})
					case "/health":
						json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
					case "/ready":
						json.NewEncoder(w).Encode(map[string]string{"status": "ready"})
					case "/hooks":
						t.Error("read health probed a write hook")
						w.WriteHeader(409)
					case "/context", "/memory":
						if r.Header.Get("Authorization") != "Bearer "+token {
							t.Error("credential crossover")
						}
						if bad {
							switch scenario {
							case "auth_error":
								w.WriteHeader(401)
								return
							case "upstream_error":
								w.WriteHeader(503)
								return
							case "protocol_error":
								w.Write([]byte("invalid"))
								return
							}
						}
						// Empty tools/data is valid; no document count is inferred.
						json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": 1, "result": map[string]any{"tools": []any{}}})
					}
				}))
				t.Cleanup(server.Close)
				cfg.Connections.Servers[alias] = config.ServerProfile{ID: "srv_fixture_" + alias, Alias: alias, Purpose: alias, URL: server.URL, Enabled: true, Status: "connected", Protocol: 1, ContextMCPURL: server.URL + "/context", MemoryMCPURL: server.URL + "/memory", MemoryHooksURL: server.URL + "/hooks", Features: map[string]bool{"memory": true, "context": true}}
				if err := (secrets.Store{Path: store.Paths.Secrets}).Set("srv_fixture_"+alias, secrets.ClientCredential{Token: token}); err != nil {
					t.Fatal(err)
				}
				if bad && scenario == "transport_error" {
					server.Close()
				}
			}
			profiles, health := (Doctor{Store: store}).ProbeProfiles(context.Background(), cfg)
			if len(profiles) != count {
				t.Fatal("profile count changed")
			}
			if count == 0 {
				if health.Configured {
					t.Fatal("invented configuration")
				}
				return
			}
			if profiles[0].ContextState != "healthy" || profiles[0].MemoryState != "healthy" {
				t.Fatalf("empty valid read failed: %+v", profiles[0])
			}
			if count == 1 && health.HookDestination != "single_destination" {
				t.Fatal(health)
			}
			if count == 2 && health.HookDestination != "ambiguous_no_write" {
				t.Fatal(health)
			}
			if scenario == "two_healthy" || scenario == "single" {
				if health.ContextState != "healthy" || health.MemoryState != "healthy" {
					t.Fatal(health)
				}
			} else {
				if health.ContextState != "degraded" || profiles[1].ContextState != scenario {
					t.Fatalf("wrong failure class: %+v %+v", profiles, health)
				}
			}
			raw, _ := json.Marshal(profiles)
			if strings.Contains(string(raw), "fixture-private") {
				t.Fatal("secret in health output")
			}
		})
	}
}
